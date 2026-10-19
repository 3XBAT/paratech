package hubclient

import (
	"errors"
	"net"
	"os"
	"syscall"
	"testing"

	"connectrpc.com/connect"

	"paratech/desktop/internal/domain/jobs/job"
	"paratech/desktop/internal/domain/jobs/services/backoff"
	"paratech/desktop/internal/domain/shared"
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
