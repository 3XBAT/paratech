# ADR-0011: Google authorization for the pilot

- **Status:** Accepted
- **Date:** 2026-10-16
- **Source:** TASK-050; PRD §4.3, §11.3, §12, §19 question 15. Facts and links: `docs/refs/google-calendar.md`.
- The task text calls this "ADR 0004"; number 0004 was taken, so 0011.

## Context
- The alpha/pilot is a closed group; the app is not verified by Google.
- `calendar.app.created` is not restricted ([Restricted scopes](https://support.google.com/cloud/answer/13464325)); Calendar
  scopes are treated as sensitive ([Sensitive scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification)),
  so a production app needs verification.
- In Testing: max 100 test users, and test-user authorizations "expire seven days from the time of consent"
  ([Manage App Audience](https://support.google.com/cloud/answer/15549945),
  [OAuth 2.0 overview, "Refresh token expiration"](https://developers.google.com/identity/protocols/oauth2)).

## Decision
- **Pilot mode:** Testing, External user type, testers listed as test users. The owner uses his own Cloud project and a
  "Desktop app" client. No verification for now: Testing with test users is an official exception.
- **7-day expiry accepted.** When Google answers `invalid_grant`, the app shows «войдите снова» and drops the stored refresh token.
- **Verification later**, before a wider pilot (over 100 users or public): owned domain verified in Search Console, homepage,
  privacy policy on that domain, demo video, scope justification; "typically 3-5 business days"
  ([Verification requirements](https://support.google.com/cloud/answer/13464321)).
- **Flow** ([native-app guide](https://developers.google.com/identity/protocols/oauth2/native-app)):
  - System browser via `explorer.exe` (`adapters/weblink`), no embedded web view.
  - Authorization Code + PKCE `S256` + random `state`, checked on return.
  - Loopback redirect `http://127.0.0.1:<ephemeral port>` without a path; single-use listener, closed after one callback or a timeout.
  - Scope: only `https://www.googleapis.com/auth/calendar.app.created`.
- **Secrets:**
  - Client ID and secret come from the user («Подключения») and live in Windows Credential Manager
    (`paratech/google/client-id`, `paratech/google/client-secret`). Never in the repo or the binary: Google says the Desktop
    client secret "is obviously not treated as a secret" ([OAuth 2.0 overview](https://developers.google.com/identity/protocols/oauth2)),
    but we still do not ship one (PRD §12).
  - Refresh token only in Credential Manager (`paratech/google/refresh-token`); access token in memory only.
- **Sign-out:** revoke at Google (`POST https://oauth2.googleapis.com/revoke`, `200` = done), then delete the local refresh
  token. The local delete happens even if the revoke fails.
- **Code layout:** client id/secret and scope rules in `domain/schedule/values` (the Google calendar is the schedule's
  calendar projection; no new context); use case `cases/googlesignin`; adapter `adapters/googleauth`; bindings `GoogleService`.
- **Library:** `golang.org/x/oauth2` v0.37.0 (PRD §4.3) with explicit Google endpoints; not `golang.org/x/oauth2/google` (it pulls
  cloud metadata dependencies).

## Consequences
- Pilot testers sign in again about once a week; each must be added as a test user by the owner (max 100).
- Revoke removes the grant for all clients of the project.
- The Desktop client belongs to the owner's project; Google deletes a client after 6 months without token requests or
  changes ([Manage OAuth Clients](https://support.google.com/cloud/answer/15549257)).
- Open: whether Google's token endpoint accepts a Desktop client without `client_secret` (docs say "Optional"); we always send it.
