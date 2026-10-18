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
    explorer.exe opens a folder for any URL with a query, so the authorization URL goes through a temporary `.url`
    file in %TEMP% that explorer.exe opens (removed after a minute); the browser stays out of our process tree (ADR-0002).
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

## Calendar (TASK-051)
- **Binding.** The id of the app calendar «Learning Assistant» and a `fresh` flag live in Credential Manager
  (`paratech/google/calendar-id`, JSON `{id, fresh}`); no SQLite migration. `calendarList.list` is not allowed with
  `calendar.app.created`, so the stored id is the only binding.
- **Gone** only when `calendars.get` AND `calendarList.get` both answer 404/410/403 (not a rate limit). Then a new
  calendar is created with `fresh: true`; the sync marks every lesson unwritten, rewrites all, then clears `fresh`.
- **Event ids** = base32hex(SHA-256(`lesson:<id>` or `deadline:<assignment id>`)), nothing stored. Upsert = PUT, on 404/410
  insert with the id, on 409 PUT again; delete: 2xx/404/410 = done. A retry after a lost answer updates the same event.
- **Job** `calendar_sync`: requeue on recovery (idempotent), not Heavy, 30 min. At most one job waits to start; a request
  while one runs makes the next one. Requested after an import that changed lessons, after sign-in, at start (if
  signed in) and by «Синхронизировать». Not signed in / sign-in expired = job completes without writing, the page says so.
  A job that starts while a run is in progress ends at once; the running one requests the next job.
- **Stuck lessons**: a lesson Google refuses (400, bad request) or keeps taken/deleted is skipped and stays pending;
  the rest is written and the job fails (retried if a skip may pass). 20 skips in a row, or any other error, end the run.
- **Sign-out keeps the binding**: the same account gets its calendar back; another account cannot see it, so a fresh
  calendar is made and everything is rewritten.
- **Known limitation:** one binding per installation, not per account. Accounts A → B → A: back in A the binding is B's
  calendar, so A gets a second calendar while its first one keeps the old events (duplicates the user sees; delete the
  old calendar by hand). The same after a change of the OAuth client: calendars of the old client are invisible to it.
- **Deadlines** only through a confirmed proposal (hard rule 3); the sync job writes lessons only. A recreated calendar
  does not get old deadlines back.
- **No read-back** from Google (AC-20): edits in Google Calendar are overwritten by the next write of that lesson.
- **Accepted crash window:** the calendar is created at Google before its id is stored; a crash in between leaves an
  extra empty calendar the user can delete. After an id that could not be stored, the next insert waits until the
  credential store takes a write again: one extra calendar per failure, not one per attempt.

## Open question (owner, 2026-10-18)
- Sign-out keeps the calendar binding for now. Revisit: switching to another account vs. signing out for good have opposite needs (keep vs. forget the binding and the app calendar).
