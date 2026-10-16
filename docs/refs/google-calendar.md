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
