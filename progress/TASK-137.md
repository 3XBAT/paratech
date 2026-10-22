# TASK-137: agent panel opens and resizes by dragging its edge

Done (frontend only, no Go, no new dependency):
- `src/agent/panelWidth.ts` (pure rules), `PanelResizeHandle.tsx` (hook + separator), small edits in App.tsx, AgentPanel.tsx, agent.css.
- Handle on the panel's left edge (6px strip at window edge when closed), col-resize, pointer events + capture. Drag left opens and follows, release keeps width, drag right below 180px closes, double click toggles, Escape on handle closes.
- Width 260..min(640, viewport-484), stored in localStorage `ui.agentWidth`/`ui.agentOpen` (like the accent, ADR-0012). Stored preference is kept unclamped; only the drawn width follows the window.
- Keyboard: role=separator, aria-valuenow (0 = closed), arrows/Home/End/Escape.
- Ctrl+K and hint chips (paratech:agent-draft) reveal a closed panel and focus the input.

Decisions to confirm: the panel has no close button, so none was added (closing = drag, double click, Escape on handle). Default stays open at 340px.
Review fixes: chips into closed panel, CSS specificity, handle vs scrollbar, width survives narrow window, ArrowLeft opens at remembered width, pointer-capture guard.
Tests: test/panelwidth.test.mjs (node:test on pure rules; no DOM in this repo, so pointer wiring is untested - owner check in qa file). No existing tests changed (fitPref assertions replaced by effectiveWidth in own file).
Follow-ups (low): aria-controls on the separator; handle z-index 5 sits above the source picker backdrop (z-index 1) at the right window edge.
Security: no findings (localStorage value validated /^\d{1,4}$/ and clamped; no secrets).
