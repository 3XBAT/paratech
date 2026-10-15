package smtp

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"strings"

	"github.com/wneessen/go-mail"

	"paratech/desktop/internal/domain/delivery/delivery"
)

// goMailAuthFailed starts the error go-mail v0.8.1 returns when the AUTH exchange fails (Client.auth in client.go):
// DialToSMTPClientWithContext returns it unwrapped, so the prefix tells a reply to AUTH from a reply to EHLO or
// STARTTLS. TestCheckClassifiesTheSetup pins it.
const goMailAuthFailed = "SMTP AUTH failed: "

// classifySetup classifies a failure of the session setup (connect, EHLO, STARTTLS, AUTH). Nothing of the message was
// sent yet, so it is never ErrOutcomeUnknown: the network and 4xx replies are transient, everything else (5xx such as
// a failed login, an untrusted certificate, a server without STARTTLS or a usable AUTH mechanism) is a configuration
// problem that retrying does not fix. The session class (ErrUnreachable ... ErrServerRefused) is added for Check.
func classifySetup(err error) error {
	var (
		certErr     *tls.CertificateVerificationError
		unknownAuth x509.UnknownAuthorityError
		hostErr     x509.HostnameError
		invalidErr  x509.CertificateInvalidError
		reply       *textproto.Error
		netErr      net.Error
	)
	switch {
	case errors.As(err, &certErr), errors.As(err, &unknownAuth), errors.As(err, &hostErr), errors.As(err, &invalidErr):
		return withKind(fail(msgCertificate, delivery.ErrRejected), ErrCertificate)
	case errors.As(err, &reply):
		return setupReply(reply.Code, strings.HasPrefix(err.Error(), goMailAuthFailed))
	case errors.As(err, &netErr), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return withKind(fail(msgConnect, delivery.ErrTransient), ErrUnreachable)
	}
	return withKind(fail(msgSetup, delivery.ErrRejected), ErrSetupUnsupported)
}

// setupReply classifies a reply code of the session setup; auth is whether it answered AUTH.
func setupReply(code int, auth bool) error {
	step := "session setup"
	if auth {
		step = "AUTH"
	}
	err := replyClass(step, code, delivery.ErrRejected)
	switch {
	case code >= 400 && code < 500:
		return withKind(err, ErrServerTemporary)
	case auth && code >= 500 && code < 600:
		return withKind(err, ErrLoginRejected)
	}
	return withKind(err, ErrServerRefused)
}

// classifySend classifies a failure of SendWithSMTPClient by the step that failed (mail.SendError.Reason). The
// message counts as handed over from the moment its content is written: from then on only a reply of the server
// proves that it was not accepted.
func classifySend(err error) error {
	var se *mail.SendError
	if !errors.As(err, &se) {
		return fail(msgAmbiguous, delivery.ErrOutcomeUnknown)
	}
	code := se.ErrorCode() // the SMTP reply code, 0 when the step failed without a reply (I/O error, timeout)

	switch se.Reason {
	case mail.ErrConnCheck:
		return fail(msgCheck, delivery.ErrTransient)
	case mail.ErrGetSender, mail.ErrGetRcpts, mail.ErrNoUnencoded:
		return fail(msgMessage, delivery.ErrRejected)
	case mail.ErrSMTPMailFrom, mail.ErrSMTPRcptTo, mail.ErrSMTPData:
		// Before the content: without a reply the connection was lost, and nothing can have been accepted.
		if code == 0 {
			return fail(msgCheck, delivery.ErrTransient)
		}
		return replyClass(stepName(se.Reason), code, delivery.ErrRejected)
	case mail.ErrWriteContent:
		return fail(msgWrite, delivery.ErrOutcomeUnknown)
	case mail.ErrSMTPDataClose:
		if code == 0 {
			return fail(msgNoFinalReply, delivery.ErrOutcomeUnknown)
		}
		return replyClass("the message", code, delivery.ErrOutcomeUnknown)
	}
	return fail(msgAmbiguous, delivery.ErrOutcomeUnknown)
}

// replyClass maps an SMTP reply code: 4xx is transient, 5xx is rejected, anything else is other.
func replyClass(step string, code int, other error) error {
	switch {
	case code >= 400 && code < 500:
		return fmt.Errorf("smtp: server replied %d to %s: %w", code, step, delivery.ErrTransient)
	case code >= 500 && code < 600:
		return fmt.Errorf("smtp: server replied %d to %s: %w", code, step, delivery.ErrRejected)
	}
	return fmt.Errorf("smtp: unexpected reply %d to %s: %w", code, step, other)
}

func stepName(r mail.SendErrReason) string {
	switch r {
	case mail.ErrSMTPMailFrom:
		return "MAIL FROM"
	case mail.ErrSMTPRcptTo:
		return "RCPT TO"
	}
	return "DATA"
}
