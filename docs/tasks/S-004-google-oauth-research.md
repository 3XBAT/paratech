# S-004: Google OAuth and Calendar scope research

- **Agent:** architect
- **Label:** docs
- **Depends on:** -
- **PRD sections / AC:** §11.3, §12, AC-17
- **Branch:** task/S-004-google-oauth

## Goal
Resolve the Google-side unknowns before building the calendar adapter.

## Allowed to change
- `docs/adr/`, `docs/refs/google-calendar.md`

## Acceptance criteria
- [ ] Verified from official Google docs: refresh-token lifetime and user limits for apps in Testing vs production; verification requirements for `calendar.app.created`
- [ ] Verified: can a secondary calendar created by the app be seen by the user in Apple Calendar via their Google account; is the secret iCal URL available via API
- [ ] Recommendation for the closed pilot (test-user mode vs verification) with steps the user must do in Google Cloud Console
