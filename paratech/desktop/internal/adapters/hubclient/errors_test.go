package hubclient

import (
	"errors"
	"net"
	"os"
	"syscall"
	"testing"

	"connectrpc.com/connect"

	agent "paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
	hubv1 "paratech/shared/gen/paratech/hub/v1"
)

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// A dial the host refused means the Hub is down, not the network (TASK-083): the job parks as for a network failure
// but records "service unavailable". Other transport failures stay a plain network wait.
func TestDialFailuresMapToNetworkWait(t *testing.T) {
	dial := func(errno error) error {
		return connect.NewError(connect.CodeUnavailable, &net.OpError{Op: "dial", Net: "tcp", Err: errno})
	}
	tests := []struct {
		name string
		err  error
		want job.Cause
	}{
		{"refused", dial(os.NewSyscallError("connect", syscall.ECONNREFUSED)), job.CauseServiceUnavailable},
		{"timeout", dial(timeoutErr{}), job.CauseNone},
		{"reset", dial(os.NewSyscallError("read", syscall.ECONNRESET)), job.CauseNone},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := mapCallError(opComplete, tc.err)
			if status, ok := backoff.WaitStatus(err); !ok || status != job.StatusWaitingForNetwork {
				t.Fatalf("wait status %q %v, want WAITING_FOR_NETWORK", status, ok)
			}
			if got := backoff.CauseOf(err); got != tc.want {
				t.Fatalf("cause %q, want %q", got, tc.want)
			}
			if !errors.Is(err, shared.ErrNetworkUnavailable) || err.Error() != opComplete+": "+shared.ErrNetworkUnavailable.Error() {
				t.Fatalf("error %q, want the fixed network text", err)
			}
		})
	}
}

// Only an error the Hub answered with an ErrorDetail carries agent.ErrHubFailure (ADR-0026 addendum 2026-11-08): the
// agent stops such a request failed, a transport failure, a timeout or a bare unavailable offline.
func TestHubFailureMarksOnlyErrorsWithDetail(t *testing.T) {
	provider := func(retryable bool, msg string) error {
		return detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
			Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: retryable, Message: msg,
		})
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"retryable provider error", provider(true, "upstream cut"), true},
		{"permanent provider error", provider(false, "upstream refused"), true},
		{"unusable model answer", provider(false, hubMsgModelAnswer), true},
		{"rate limited", detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_RATE_LIMITED}), true},
		{"invalid api key", detailErr(connect.CodeUnauthenticated, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_INVALID_API_KEY}), true},
		{"unknown detail code", detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{Code: hubv1.ErrorCode(999)}), true},
		{"transport", connect.NewError(connect.CodeUnavailable, &net.OpError{Op: "dial", Net: "tcp", Err: timeoutErr{}}), false},
		{"timeout", connect.NewError(connect.CodeDeadlineExceeded, errors.New("deadline")), false},
		{"bare unavailable", connect.NewError(connect.CodeUnavailable, errors.New("bad gateway")), false},
		{"not a connect error", errors.New("broken pipe"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := errors.Is(mapCallError(opAgentStep, tc.err), agent.ErrHubFailure); got != tc.want {
				t.Fatalf("hub failure mark = %v, want %v", got, tc.want)
			}
		})
	}
}

// The mark does not change the class or the domain sentinel of an error with a detail.
func TestHubFailureKeepsClass(t *testing.T) {
	err := mapCallError(opAgentStep, detailErr(connect.CodeUnavailable, &hubv1.ErrorDetail{
		Code: hubv1.ErrorCode_ERROR_CODE_PROVIDER_ERROR, Retryable: true,
	}))
	if !errors.Is(err, ErrProvider) || !errors.Is(err, backoff.ErrTransient) || !errors.Is(err, shared.ErrServiceUnavailable) {
		t.Fatalf("retryable provider error = %v, want provider, transient, unavailable", err)
	}
	if err.Error() != opAgentStep+": "+ErrProvider.Error() {
		t.Fatalf("text = %q, the mark must not change it", err)
	}
	quota := mapCallError(opAgentStep, detailErr(connect.CodeResourceExhausted, &hubv1.ErrorDetail{Code: hubv1.ErrorCode_ERROR_CODE_QUOTA_EXCEEDED}))
	if !errors.Is(quota, shared.ErrQuotaExceeded) || errors.Is(quota, backoff.ErrTransient) || errors.Is(quota, backoff.ErrPermanent) {
		t.Fatalf("quota = %v, want quota without a class", quota)
	}
}
