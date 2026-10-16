# Google authorization and Calendar scope

Library: `golang.org/x/oauth2` v0.37.0 (latest on pkg.go.dev, published 2026-08-25; the used version is in
`paratech/desktop/go.mod`). `golang.org/x/oauth2/google` is NOT used (ADR-0011).
All facts below fetched 2026-10-16 from the official pages; "updated" is the date shown on the page ("no date" if none).

## Sources
| Page | Updated |
|---|---|
| [OAuth 2.0 for iOS & Desktop Apps](https://developers.google.com/identity/protocols/oauth2/native-app) | 2026-10-15 |
| [OpenID configuration](https://accounts.google.com/.well-known/openid-configuration) | no date |
| [Using OAuth 2.0 (overview, refresh token expiration)](https://developers.google.com/identity/protocols/oauth2) | 2026-05-26 |
| [OAuth 2.0 scopes for Google APIs](https://developers.google.com/identity/protocols/oauth2/scopes) | 2026-09-14 |
| [Calendar API: choose scopes](https://developers.google.com/workspace/calendar/api/auth) | 2026-09-03 |
| [Sensitive scope verification](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification) | 2026-08-19 |
| [Verification requirements](https://support.google.com/cloud/answer/13464321) | no date |
| [Restricted scopes](https://support.google.com/cloud/answer/13464325) | no date |
| [Manage App Audience](https://support.google.com/cloud/answer/15549945) | no date |
| [Unverified apps](https://support.google.com/cloud/answer/7454865) | no date |
| [Manage OAuth Clients](https://support.google.com/cloud/answer/15549257) | no date |
| [Calendar API: troubleshoot auth](https://developers.google.com/workspace/calendar/api/troubleshoot-authentication-authorization) | 2026-09-03 |
| [Create a project](https://developers.google.com/workspace/guides/create-project) | 2026-09-03 |
| [Enable APIs](https://developers.google.com/workspace/guides/enable-apis) | 2026-09-03 |
| [Configure the OAuth consent screen](https://developers.google.com/workspace/guides/configure-oauth-consent) | 2026-09-03 |
| [Create credentials](https://developers.google.com/workspace/guides/create-credentials) | 2026-10-12 |
| [Manage links with apps from other developers](https://support.google.com/accounts/answer/13533235) | no date |
| [pkg.go.dev golang.org/x/oauth2](https://pkg.go.dev/golang.org/x/oauth2) | v0.37.0 |

## 1. Scope `https://www.googleapis.com/auth/calendar.app.created`
- Meaning: "Make secondary Google calendars, and see, create, change, and delete events on them" (Calendar auth page, scopes page).
  So: the app may create its own calendar and manage events only there; no access to the primary or other calendars.
- Class: NOT restricted (the Restricted scopes page lists Gmail, Drive, Fit, Chat, Data Portability, Photos Ambient, Health; no Calendar).
- Sensitive or non-sensitive: **not verified** — no fetched page labels this exact scope. The sensitive-scope page uses
  "reading events stored in Google Calendar" as an example of a sensitive scope. Expect "sensitive"; the Console shows the
  indicator under Data Access when the scope is added. Treat it as sensitive until the Console says otherwise.
- Sensitive scope in production needs verification: verified domain (Search Console), homepage describing the app, privacy policy
  on that domain, demo video of the OAuth flow, justification why a narrower scope is not enough; review "typically takes
  3-5 business days" (Sensitive scope verification; Verification requirements). Restricted scopes also need a yearly security
  assessment (not our case).
- No verification needed while the app stays in Testing with listed test users, or for personal use (Sensitive scope verification, exceptions).

## 2. Desktop flow (native-app page unless noted)
- Endpoints (same in the OpenID configuration):
  auth `https://accounts.google.com/o/oauth2/v2/auth`, token `https://oauth2.googleapis.com/token`,
  revoke `https://oauth2.googleapis.com/revoke`. `code_challenge_methods_supported`: `plain`, `S256`.
- Client type: "Desktop app". Loopback redirect is the recommended method for Windows desktop (not UWP); it is deprecated
  only for Android, Chrome app and iOS. Custom URI schemes and the OOB copy/paste flow are not supported.
- Redirect URI: `http://127.0.0.1:port` or `http://[::1]:port`; "start an HTTP listener on a random available port"; the port is
  not registered in the Console. `localhost` works but "may cause issues with client firewalls".
- Path in the loopback URI: **not stated** (all examples have no path). Use no path: `http://127.0.0.1:<port>`; the listener
  serves `/` only.
- PKCE: verifier 43-128 chars of `[A-Za-z0-9-._~]`; `code_challenge_method` `S256` (recommended) or `plain` (default if omitted,
  so always send `S256`).
- `state`: "Recommended"; the client "must prevent CSRF". Returned unchanged.
- Refresh token: "refresh tokens are always returned for installed applications"; the page never mentions `access_type`.
  `prompt=consent` is mentioned only for forcing a new (DPoP-bound) refresh token. Incremental authorization is not supported
  for installed apps.
- Errors: `error=access_denied` = user declined; `invalid_grant` on refresh = "the token may have expired or has been invalidated";
  on exchange = bad/missing code verifier.
- `client_secret`: marked **"Optional"** in the token exchange and refresh tables. The overview page says for installed apps
  "the client secret is obviously not treated as a secret". Not verified here whether the token endpoint accepts a Desktop
  client without it; the app sends it (user-configured), which works either way.
- Token response: `access_token`, `expires_in`, `refresh_token`, `scope`, `token_type`; `refresh_token_expires_in` only with
  time-based access.
- Revoke: `POST https://oauth2.googleapis.com/revoke`, `Content-type: application/x-www-form-urlencoded`, parameter
  `token=<access or refresh token>` (the page's curl puts it in the query string; a form body is the standard form).
  `200` = revoked; `400` + error code otherwise. Revoking removes all scopes granted to the project, for all its clients;
  full effect may take some time.
- User-side revoke: Google Account > linked apps (`https://myaccount.google.com/linkedapps`) > the app > See details >
  Remove access > Confirm (support answer 13533235).

## 3. Testing vs production
- Testing (External user type): up to **100 test users** listed under Audience; a test user uses a slot once added
  (Manage App Audience).
- Testing: "Authorizations by a test user will expire seven days from the time of consent" — the refresh token stops working
  after **7 days** (Manage App Audience; overview: exempt only if the scopes are just `openid`, `userinfo.email`, `userinfo.profile`).
- Unverified app screen: shown when sensitive/restricted scopes are requested without finished verification; the user passes it via
  "Advanced > Go to {Project Name} (unsafe)" (Unverified apps; Calendar troubleshoot page).
- In production but unverified: cap of "100 new users in total" after the unverified screen, for the project's lifetime, cannot be reset.
- Refresh token stops working when (overview): user revokes; unused 6 months; password change with Gmail scopes; over the live
  token limit; time-based access expired; admin restricts a service; GCP session control (`invalid_grant`, `error_subtype`).
- Limits: 100 refresh tokens per Google Account per client ID — the 101st silently invalidates the oldest; a larger limit per
  account across all clients.
- OAuth clients inactive for 6 months are deleted automatically (email 30 days before) (Manage OAuth Clients).

## 4. golang.org/x/oauth2 (pkg.go.dev, v0.37.0)
- PKCE since v0.13.0: `GenerateVerifier() string` (32 random octets), `S256ChallengeOption(verifier) AuthCodeOption`,
  `VerifierOption(verifier) AuthCodeOption`, `S256ChallengeFromVerifier(verifier) string`.
- `Config.AuthCodeURL(state, opts...)`, `Config.Exchange(ctx, code, opts...)` ("opts should include a VerifierOption"),
  `Config.TokenSource(ctx, t)` (refreshes automatically).
- `Endpoint{AuthURL, DeviceAuthURL, TokenURL, AuthStyle}`; `AuthStyleInParams` sends id/secret in the form body,
  `AuthStyleInHeader` via Basic auth, `AuthStyleAutoDetect` (0) tries both.
- `AccessTypeOffline` is a var = `SetAuthURLParam("access_type", "offline")`. `RetrieveError.ErrorCode` = RFC 6749 `error`.

## App settings mapping
- Client ID and client secret: entered by the user in «Подключения», stored in Windows Credential Manager as
  `paratech/google/client-id` and `paratech/google/client-secret` (see `wincred.md`).
- Refresh token: Credential Manager `paratech/google/refresh-token`. Access token: memory only.
- Nothing in SQLite, plaintext files, the repo or the binary.

## Owner steps in Google Cloud Console
1. Create a project: Menu > IAM & Admin > Create a Project > name > Create.
2. Enable the API: Menu > APIs & Services > Library > Google Workspace > Google Calendar API > Enable.
3. Menu > Google Auth platform > Branding > Get Started: app name, support email; Audience: **External**; contact email;
   agree to the User Data Policy > Create. Publishing status stays **Testing**.
4. Audience > Test users > Add users: your Gmail (and each pilot tester) > Save.
5. Data Access > Add or Remove Scopes: `https://www.googleapis.com/auth/calendar.app.created` > Save.
6. Clients > Create Client > Application type **Desktop app** > name > Create.
7. Copy the client ID and the client secret right away: the full secret is shown/downloadable **only once** at creation
   (Manage OAuth Clients). Paste both in the app's «Подключения».
8. Sign in from the app; on the "Google hasn't verified this app" screen choose Advanced > Go to … (unsafe).
9. To revoke: «Выйти» in the app, or myaccount.google.com/linkedapps > the app > Remove access.
- In Testing the sign-in lasts 7 days; then the app asks to sign in again.

## Calendar API v3 (TASK-051)
Fetched 2026-10-16 (base `https://developers.google.com/workspace/calendar/api/`). "Not stated" = the docs do not say it;
the adapter (`adapters/googlecal`) then picks the behavior that fails safe (no duplicate events, nothing outside the app
calendar, no item marked synced when it is not).

### Sources
| Page | Updated |
|---|---|
| [calendars.insert](https://developers.google.com/workspace/calendar/api/v3/reference/calendars/insert) | 2025-10-28 |
| [calendars.get](https://developers.google.com/workspace/calendar/api/v3/reference/calendars/get) | 2026-05-12 |
| [Calendars resource](https://developers.google.com/workspace/calendar/api/v3/reference/calendars) | 2026-07-01 |
| [calendarList.list](https://developers.google.com/workspace/calendar/api/v3/reference/calendarList/list) | 2026-07-29 |
| [calendarList.get](https://developers.google.com/workspace/calendar/api/v3/reference/calendarList/get) | 2026-05-12 |
| [CalendarList resource](https://developers.google.com/workspace/calendar/api/v3/reference/calendarList) | 2026-07-29 |
| [events.insert](https://developers.google.com/workspace/calendar/api/v3/reference/events/insert) | 2026-07-07 |
| [events.update](https://developers.google.com/workspace/calendar/api/v3/reference/events/update) | 2026-07-07 |
| [events.get](https://developers.google.com/workspace/calendar/api/v3/reference/events/get) | 2026-05-12 |
| [events.delete](https://developers.google.com/workspace/calendar/api/v3/reference/events/delete) | 2026-05-12 |
| [events.patch](https://developers.google.com/workspace/calendar/api/v3/reference/events/patch) | 2026-07-01 |
| [events.list](https://developers.google.com/workspace/calendar/api/v3/reference/events/list) | 2026-07-29 |
| [Events resource](https://developers.google.com/workspace/calendar/api/v3/reference/events) | 2026-07-07 |
| [Handle API errors](https://developers.google.com/workspace/calendar/api/guides/errors) | 2026-09-11 |
| [Extended properties](https://developers.google.com/workspace/calendar/api/guides/extended-properties) | 2026-09-11 |
| [Create events](https://developers.google.com/workspace/calendar/api/guides/create-events) | 2026-09-11 |
| [Choose scopes](https://developers.google.com/workspace/calendar/api/auth) | 2026-09-03 |

### Facts
- Base URL `https://www.googleapis.com/calendar/v3`: verified (all reference pages).
- Scope `calendar.app.created` is allowed for calendars.insert/get, calendarList.get and events
  insert/update/get/delete/patch/list: verified (each method page). **calendarList.list is NOT allowed** (only
  `calendar.readonly`, `calendar`, `calendar.calendarlist`, `calendar.calendarlist.readonly`): verified. So the app
  cannot look its calendar up; the id it stored is the only binding.
- calendars.insert: only `summary` required; `description`, `timeZone` optional: verified. That Google assigns the id:
  not stated (the docs only say "call calendarList.list() to retrieve IDs").
- calendars.get for a missing calendar: not stated per method; the errors guide has a generic 404 `notFound` and advises
  exponential backoff for it. calendarList.get: same.
- Event `id` chosen by the client: lowercase `a-v` and `0-9` (base32hex), length 5..1024, unique per calendar;
  "collisions may not be detected at creation time": verified (events.insert, Events resource). The Create events guide
  recommends client ids to avoid duplicates when a call failed after it succeeded on the backend: verified.
- Insert of an existing id: 409 `duplicate` "The requested identifier already exists": verified (errors guide, generic).
  Insert with the id of a deleted (cancelled) event gives 409: not stated.
- events.update (PUT) "always updates the entire event resource"; `start`/`end` required: verified. PUT on an unknown id
  gives 404: not stated (generic 404). PUT with `status: confirmed` restores a cancelled event: not stated; the Events
  resource only says cancelled events on the organizer's calendar keep their details "so that they can be restored
  (undeleted)" and "will eventually disappear".
- events.get "always returns" cancelled events: verified (Events resource).
- events.delete returns an empty body: verified; the status (204) is not stated. 410 `deleted` = already deleted, no
  action needed: verified (errors guide).
- `start`/`end` = `{dateTime (RFC 3339), timeZone (IANA)}`: "A time zone offset is required unless a time zone is
  explicitly specified in timeZone"; end is exclusive: verified. A `Z` dateTime with `timeZone: UTC` is therefore valid.
  A zero-length timed event: not stated.
- `status`: `confirmed`, `tentative`, `cancelled` ("The event is cancelled (deleted)."): verified.
- `extendedProperties.private`: private to the copy of the event on this calendar (not private to the app); key at most
  44 characters (a longer key is dropped silently), value at most 1024 characters (cut silently), at most 300
  properties and 32 kB per event: verified (Extended properties guide).
- `summary`/`description` length limits: not stated ("Can contain HTML. Optional.").
- Errors (errors guide; body `error.code`, `error.errors[].reason`): 400 do not retry; 401 `authError` refresh the
  token, else sign in again; 403 `rateLimitExceeded`/`userRateLimitExceeded` exponential backoff (`rateLimitExceeded` may
  also come as 429); 403 `quotaExceeded` = usage limits, no retry advice; 403 `forbiddenForNonOrganizer` listed;
  `dailyLimitExceeded`, `forbidden`, `insufficientPermissions` not listed; 409 `duplicate` (409 `conflict` only in
  batches); 412 `conditionNotMet` (ETag; the adapter sends no `If-Match`); 429 backoff, `Retry-After` not stated; 500
  `backendError` backoff; 502/503/504 not listed: all verified as quoted.

### Adapter decisions
- Event id = lowercase base32hex (no padding) of SHA-256 of `lesson:<lesson id>` or `deadline:<assignment id>`: 52
  characters, deterministic, so no event id is stored (ADR-0004(e)).
- Upsert = PUT; 404/410 → insert with the id; 409 → PUT once more. A retry after a lost answer updates the event it
  created and never makes a second one (PRD section 14 case 8). A 2xx PUT whose answer is still `cancelled` is an
  error (`ErrNotRestored`, transient): the item stays pending, never "synced" with an invisible event.
- Calendar binding: the stored id only. It is replaced only when calendars.get AND calendarList.get both answer 404, 410
  or 403 (not a rate limit), so one spurious 404 never makes a second calendar; a 403 alone (another account's
  calendar, or anything) needs the other check to agree. Crash window: created at Google, app dies before the id is
  stored → the next run creates another, empty calendar (accepted, the user can delete it; no wider scope).
- Delete: any 2xx, 404 and 410 are success.
- Private properties: at most 4 per event, keys of at most 12 characters, values of at most 137 bytes (ids are at most
  128); the adapter refuses a write that would exceed the limits instead of letting Google drop or cut it.
- 403 rate-limit and quota reasons (`quotaExceeded`, `dailyLimitExceeded` included) are transient, never "no access" and
  never "calendar gone". The adapter does not retry them itself; the job runner's exponential backoff (5 s doubling to
  5 min, 5 attempts) spaces the retries. `Retry-After` is exposed through `RetryAfter()`; the runner does not read it
  today.
- Deadline event: start = due, end = due + 15 minutes, `timeZone: UTC` (zero length is not stated).

### Проверить владельцу на живом Google
- Does PUT with `status: confirmed` restore an event deleted in Google Calendar (and one the app deleted)? If not,
  the lesson stays pending with `ErrNotRestored`.
- Does insert with the id of a deleted event answer 409 (the adapter then PUTs), or something else?
- What do calendars.get and calendarList.get answer for the stored calendar after it is deleted, and after signing in
  with another account (404 or 403)?
- Zero-length events are not used, nothing to check there.
