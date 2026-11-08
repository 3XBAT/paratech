# frontend/src/agent

The agent panel on the right of every main-window screen: the chat header, the request field with `@` mentions, the thread
of requests and the cards the agent answers with (plan, questions, reply, action line with "Отменить", confirm, work, offer
button...). It only shows what the Go core publishes and sends the student's clicks back; the planner, the safety classes
and every rule live in Go (ADR-0021, ADR-0033). Paths below are relative to `paratech/desktop/frontend/src` unless they start with `paratech/` or `docs/`;
`test/...` and `tsconfig.test.json` live in `paratech/desktop/frontend` (not in `src/`).

## Map
- `agent/AgentPanel.tsx` - the stateful container: calls `bindings/agent.ts`, listens to `updateEvent` ("agent:update"),
  keeps `PanelState` (`reducePanel`), builds `CardActions` in `actionsFor` (`press`, `undo`, `offer`, `copy`...).
- `agent/AgentPanelView.tsx` - the pure-props layout of the panel (header, field, thread); rendered directly by tests.
- `agent/AgentCards.tsx` - `AgentRequest` (one request) and `Card()`, the `switch` over `CardModel.type`; `CardActions` is
  what a card may ask of the panel. Big cards live in their own files:
  `ConfirmCard.tsx` (`ConfirmCardView` + stateful `ConfirmCard`), `ScheduleCard.tsx`, `WorkCard.tsx`, `WorkSend.tsx`,
  `TurnkeyCards.tsx` (triage, questions, understanding, helper), `AnswerCard.tsx` (the `answer` card body: `useAnswer` reads the
  answer once per answerId and attempt, claims then the missing line then the sources, in an `aria-live="polite"` container; a failed
  read shows a quiet line with a «Повторить» icon that reads again; pure texts and the long-claim clamp in `answerView.ts`)
  with `AnswerSources.tsx` (the sources list only). E2E relies on: first `<p>` of the card = first claim, `ul.ag-src` buttons.
- Smaller parts: `MessageActions.tsx` (copy / rerun icons under messages), `ChatBar.tsx`, `ContextRing.tsx`, `RestartDialog.tsx`,
  `MentionParts.tsx` with `useMentionComposer.ts`, `Waiting.tsx`, `PanelResizeHandle.tsx` with `panelWidth.ts` (pure rules).
- Compaction (TASK-270): `AgentPanel` keeps a per-chat `CompactingMap` (`compactingFromUpdate` / `compactingFromList` / `compactingOf` in `agentView.ts`)
  and passes `compacting` to `AgentPanelView`, which draws `CompactionNote` (`ContextRing.tsx`), an always-rendered empty `role="status"`
  text line under the field (no box, no dots: the thread's Waiting already animates). While it shows, `ContextHint` is not drawn (one line
  in that slot) and the thread re-pins to the bottom. The ring itself follows `fillsFromUpdate`; the hint sentence is `RingModel.hintText`
  (`ringTexts.overflowHint` after an overflow).
- «Начать заново отсюда» (TASK-249, TASK-271): the icon under the student's message opens `RestartDialog` with two choices.
  «Скрыть» (`hideRestart`: the text goes into the field, the next send carries `restartFrom`) is off while a request runs.
  «Форк» (`forkFrom` in `AgentPanel`, `AgentApi.forkChat` -> Go `ForkChat`) is always on: the core makes a new chat
  «<название> (fork)» (the suffix is the core's), the panel opens it like «Новый чат», puts the message's text into its field
  (`forkOpened` / `forkField`: typed text is kept, the message follows it; a pending «Скрыть» edit of the old chat ends, but text
  the student edited after «Скрыть» goes into the new field, after what it held before) and does not send it. The chat note then
  says «Открыт новый чат. Исходный «…» не изменился.» with a small icon «Открыть исходный чат» (`forkNote` in `AgentPanel`,
  `ChatBar` `noteAction`); it ends with a chat switch, any other note or the next send. The « (fork)» suffix of a title is its own
  span (`splitForkTitle`) so the ellipsis never takes it. One call at a time (`beginFork`); a refusal is written under «Форк» in
  the dialog (`forkFailure`): the «Форк» button itself is the retry, «Новый чат» (gone or changed chat; `invalid` also reloads
  the earlier requests) stands in the same option. A refusal after the dialog was closed goes to the chat note.
- CSS: `agent.css` (panel and common cards), `confirm.css`, `turnkey.css`; all three are imported in `main.tsx`.
- Pure logic is outside this folder, in `src/`: `agentView.ts` (texts, `CardModel`, `cardModel`, `entryModel`, `offerSpecs`,
  button labels and icons), `agentState.ts` (panel reducer, no React), `agentOffer.ts` (`offerStep`, `runOffer`),
  `agentMentions.ts`, `agentDraft.ts`, `agentConfirmView.ts`, `agentScheduleView.ts`, `planView.ts`, `workView.ts`, `turnkeyView.ts`.
- Bindings used: `bindings/agent.ts` (`AgentService`), `agentConfirm.ts`, `turnkey.ts`, `answers.ts`, `library.ts`, `calendar.ts`, among others (`transcript.ts`, `recordingDelete.ts`, `recipient.ts`).
  Their Go side is in `paratech/desktop/internal/input/wails` (`agent*.go`, `turnkey.go`, `answers.go`).

## Where to add
- A card kind, in order: Go `AgentCardView.Kind` in `paratech/desktop/internal/input/wails/agentpanel.go` -> `bindings/agent.ts`
  (`KnownCardKind`, `cardKinds`, the card parser) -> `agentView.ts` (`CardModel` variant and a `case` in `cardModel`) ->
  `agent/XCard.tsx` (a hook-free `XCardView` for tests plus a stateful wrapper) -> a `case` in `Card()` of `agent/AgentCards.tsx`
  -> CSS -> a `test/agent*.test.mjs` and the new files in `../tsconfig.test.json`. A kind the UI does not know is shown as
  a quiet "unknown" stub, never dropped (`bindings/agent.ts`), so the Go side may ship first. A confirm card also needs
  its target in `cardKinds` of `bindings/agentConfirm.ts` (and the Go `AgentConfirmService.Card`/`Decline` branches).
- A page button the agent offers (`offer_button`, ADR-0033 §3): the action name in `offerActions` (`bindings/agent.ts`),
  its label and target in `offerSpecs` (`agentView.ts`), what a press does in `offerStep` / `runOffer` (`agentOffer.ts`).
  The label comes from the page's own button, never from model text.
- A button of a card (`ButtonId`): `agentView.ts` (`ButtonId`, `buttonLabels`, `cardButtonIcon`), the handler in `press` of `AgentPanel.tsx`.
- A message action (the icon row under a message): `agent/MessageActions.tsx`, its texts `messageActionTexts` in `agentView.ts`,
  placed in `AgentRequest` (`AgentCards.tsx`) and `EarlierRequests` (`AgentPanelView.tsx`); never on service cards.

## Rules that bind
- ARCH-44, ARCH-27, CLAUDE.md rule 3: model text is untrusted and reaches the page only as React text nodes; no element,
  URL, path or label is built from it. Side effects (send, delete, Google) happen through a confirm card whose click calls a
  core binding, not from the card text (ADR-0021, ADR-0022).
- ADR-0033: a feature the UI gets should be doable by the agent; update `docs/agent-parity.md` when a binding is added.
- UI texts in Russian, impersonal voice; main actions keep words, secondary buttons are `IconAction` / `ActionButton` icons
  with a tooltip and `aria-label` (TASK-368, the comment above `cardButtonIcon` in `agentView.ts`).
- Ctrl+K opens the panel with focus in the field, or closes it (`useFocusShortcut` in `AgentPanel.tsx`).

## Tests
Same runner as `README.md` of `src/`: `npm --prefix paratech/desktop/frontend run test` and `run build`. Agent tests are
`test/agent*.test.mjs` (also `answercard`, `answersources296`, `messageactions365`, `panelwidth`, `chatheader365`...) and render
`AgentRequest` / `AgentPanelView` with `react-dom/server`. `test/agent.test.mjs` compares the `methods` of `bindings/agent.ts`
with the exported `AgentService` methods in `agent.go`, `agentjournal.go`, `agentchats.go`, `agentmentions.go`: add a Go method, add it to `methods`
(`ForkChat` is in `methods` as `forkChat`; its parser is `parseForkResult`, tests in `agentfork271.test.mjs`).

## Gotchas
- The core publishes updates from several operations without a global order; `reducePanel` applies each to the request it
  names (threadId + turn) and a finished request stays finished (`agentState.ts`). Do not rely on arrival order.
- Every update of a request carries ALL its cards (the Go Driver sends them in display order: reading cards first; confirm
  cards and action lines «Отменить» after the reply; the stop line last): the later update replaces the earlier list, never
  appends (`agentdriverupdates.test.mjs`); the UI does not reorder. "Late" cards (confirm, action line, schedule, then the stop
  line) come after the reply; `isLateCard` / `firstLateCard` (`agentView.ts`) name them, and the typing reply, the answer drafts
  and a kept cut-off text are drawn right above the first one (`AgentRequest`), so the final reply card replaces the draft in place.
- Keys of cards (`cardKeys`, `agentView.ts`) include kind and caption so a new card does not inherit the typed state of the old
  one; late cards are keyed by identity (confirm target, action id, schedule token), not index: the reply arriving above a
  confirm card must not remount it, with an occurrence counter for repeats (`#2` counts earlier cards with the same identity,
  not the position; the planner may propose one target twice) (`agentLateCards20261108.test.mjs`).
- `AgentActions` and `UserActions` render nothing when there is nothing to copy or rerun.

## E2E
ADR-0035 plans the pack `paratech/e2e/specs/agent/` (link question, "что было на паре", add a subject plus undo, tool failure
honesty, recording titles). It does not exist yet (only `paratech/e2e/src` and `paratech/e2e/test`). Per ADR-0035 §6 a UI task
that changes agent behaviour adds or updates its scenario there; `docs/process/e2e.md` is not written yet.
