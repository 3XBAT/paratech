// Package localhub is the use case of the Hub the owner's beta starts on this computer (ADR-0036). Service.Ensure
// reads «Адрес сервера»: a remote address means nothing to do; for a loopback Target it probes the Hub and, when
// nothing answers and every precondition holds, generates the seal keys if missing, starts hub.exe and waits for it
// to answer. Concurrent Ensure calls share one run, so the Hub is never started twice. The app never starts Docker
// Desktop: Docker off is a reason the owner acts on. When the engine already runs and the database does not answer,
// Ensure starts the database container through DatabaseStarter first (ADR-0036 section 8, status starting_database);
// a failed start (database_start_failed) is not repeated automatically until Recheck, a stored database URL or the
// engine seen off and on again.
//
// Only the wiring behind the build tag beta uses this package (ADR-0036 section 1, enforced by tests/arch_test.go).
//
// No secret value is kept, logged, returned or put in an error here: SetSecret hands the value straight to the
// SecretSlots port; the launcher and the probe read the secrets themselves.
//
// Rules that apply: ARCH-20..27.
package localhub
