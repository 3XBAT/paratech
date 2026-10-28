# Architecture decision records

One file per decision: `NNNN-short-title.md` with Status (Proposed/Accepted/Superseded), Context, Decision, Consequences.
Open decisions are tracked in PRD §19.

## Index
- [0001](0001-technology-stack.md) Technology stack
- [0002](0002-initial-bounded-contexts.md) Initial bounded contexts, single lint config, proto path
- [0002 (audio capture)](0002-audio-capture.md) System audio capture on Windows
- [0003](0003-linear-main-and-config-flags.md) Linear `main` and flag parsing in `config`
- [0004](0004-core-contract-decisions.md) Core contract decisions (ports, schema, shared kernel)
- [0005](0005-hub-contract-decisions.md) Hub contract decisions (gateway, quota ledger, errors)
- [0006](0006-pkg-directory-and-migrations.md) The `pkg/` directory and where migrations live
- [0007](0007-wails-popup.md) Wails v3 shell, tray and recording popup
- [0008](0008-hub-quota-ledger.md) Hub quota ledger
- [0009](0009-desktop-core-wiring-and-lifecycle.md) Desktop core wiring and lifecycle
- [0010](0010-hub-llm-gateway.md) Hub LLM gateway
- [0011](0011-google-oauth-pilot.md) Google authorization for the pilot
- [0012](0012-visual-direction-and-fonts.md) Visual direction, font and icons of the desktop UI
- [0013](0013-paratech-url-scheme.md) The paratech: URL scheme
- [0014](0014-secrets-profile-per-data-dir.md) Credential Manager secrets per data directory
- [0015](0015-rename-to-paratech.md) Rename to «paratech»
- [0016](0016-recognition-load-modes.md) Recognition load modes and time estimates
- [0017](0017-lecture-text.md) Lecture text in parts
- [0018](0018-recording-deletion.md) Deleting recordings
- [0019](0019-knowledge-base.md) Knowledge base without an index
- [0020](0020-document-pdf.md) PDF of a student paper
- [0021](0021-agent-core.md) Agent as the core
- [0022](0022-untrusted-input.md) Untrusted input for every call
- [0023](0023-hub-agentstep.md) Hub: AgentStep and prompts on the server
- [0024](0024-subjects-library.md) Subjects and library
- [0025](0025-turnkey-work.md) Turnkey work: triage, questions, helper mode
- [0026](0026-agent-stop-rerun-streaming.md) Agent panel: waiting, «Стоп», repeat and streamed answers (Proposed)
- [0027](0027-own-notes.md) The student's own notes («Мой конспект») for lectures (Proposed)
- [0028](0028-agent-chats-context-ring.md) Agent chats and the context ring
- [0029](0029-student-assignments.md) Assignments the student adds (by hand or through the agent); delete and edit
- [0030](0030-subject-teachers-scheduled-send.md) Teacher of a subject and «Отправить в…»
- [0031](0031-speech-vad.md) Skipping silence in speech recognition (Silero VAD)
- [0032](0032-lesson-call-links.md) Call link of a lesson (Proposed)
- [0033](0033-agent-parity.md) Agent parity: everything the student does in the UI, the agent can do (Proposed)
