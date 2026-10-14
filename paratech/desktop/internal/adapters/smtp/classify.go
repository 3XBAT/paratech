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

	"github.com/wneessen/go-mail"

	"paratech/desktop/internal/domain/delivery/delivery"
)

// classifySetup classifies a failure of the session setup (connect, EHLO, STARTTLS, AUTH). Nothing of the message was
// sent yet, so it is never ErrOutcomeUnknown: the network and 4xx replies are transient, everything else (5xx such as
// a failed login, an untrusted certificate, a server without STARTTLS or a usable AUTH mechanism) is a configuration
// problem that retrying does not fix.
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
		return fail(msgCertificate, delivery.ErrRejected)
	case errors.As(err, &reply):
		return replyClass("session setup", reply.Code, delivery.ErrRejected)
	case errors.As(err, &netErr), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF),
		errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return fail(msgConnect, delivery.ErrTransient)
	}
	return fail(msgSetup, delivery.ErrRejected)
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
