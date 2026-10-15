# go-mail (SMTP client)

Version: `github.com/wneessen/go-mail v0.8.1`. Source: https://github.com/wneessen/go-mail (read from the module cache: `client.go`, `senderror.go`, `smtp/auth_plain.go`, `smtp/auth_login.go`).
Used only by `desktop/internal/adapters/smtp`.

## Behaviour we rely on

- **`Msg.IsDelivered()`** is set in `sendSingleMsg` right after the server answered the terminating dot with success, before the RSET that follows.
  So `IsDelivered() == true` means accepted, even if `SendWithSMTPClient` returns an error (an `ErrSMTPReset` from the RSET).
- **`SendError.Reason`** names the step that failed; `SendError.ErrorCode()` is the SMTP reply code, 0 when the step failed without a reply (I/O error, timeout).
  Our classification (`classify.go`):

  | Reason | Meaning | Class |
  |---|---|---|
  | `ErrConnCheck` | NOOP before the message failed | transient |
  | `ErrGetSender`, `ErrGetRcpts`, `ErrNoUnencoded` | message cannot be built | rejected |
  | `ErrSMTPMailFrom`, `ErrSMTPRcptTo`, `ErrSMTPData` | before the content | code 0 or 4xx: transient; 5xx: rejected |
  | `ErrWriteContent` | connection lost while writing the content | **unknown** |
  | `ErrSMTPDataClose` | reply to the terminating dot | code 0: **unknown**; 4xx: transient; 5xx: rejected |
  | anything else, or not a `SendError` | | **unknown** |

  Session setup errors (dial, STARTTLS, AUTH) come from `DialToSMTPClientWithContext`, not as `SendError`: network/4xx transient, otherwise rejected.
- **PLAIN and LOGIN** auth refuse an unencrypted connection (`smtp.ErrUnencrypted`), except to localhost. We never rely on that exemption:
  the adapter always uses `TLSMandatory` (STARTTLS) or implicit TLS.
- **Deadlines**: `WithTimeout` is not a session timeout. The connection deadline is set at dial (`SetDeadline`, then `UpdateDeadline` after the greeting)
  and reset once in `checkConn` before the message. So setup and MAIL..final reply each get the full timeout:
  the job timeout must be at least 2x the SMTP timeout. Cancellation is our own: a `context.AfterFunc` closes the raw connection.
