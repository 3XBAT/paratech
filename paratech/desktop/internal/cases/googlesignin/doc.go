// Package googlesignin is the use case of the Google section of the «Подключения» page (TASK-050; PRD sections 11.3
// and 12, AC-17, AC-19): the user's own OAuth client (client id and secret), the sign-in in the system browser, its
// cancellation and the sign-out with revocation.
//
// Secrets only go in: the use case stores, replaces and removes the client values and asks whether they and the
// refresh token exist, never reads them (hard rule 4). The browser flow, the tokens and the revocation belong to the
// Authorizer port (adapters/googleauth through a bridge in the app). At most one sign-in or sign-out runs at a time,
// and the client cannot change while signed in: a refresh token belongs to the client that obtained it.
//
// Rules that apply: ARCH-20..27.
package googlesignin
