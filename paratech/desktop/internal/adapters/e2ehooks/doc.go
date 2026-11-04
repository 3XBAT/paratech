// Package e2ehooks answers the native dialogs of the app and records its openers and clipboard writes during an
// end-to-end run (ADR-0035 §3). app wires it only in E2E mode, which exists only in a build with tag e2e; only the
// e2e-tagged app/e2e_hooks_on.go imports it, so every other build does not link it. It starts no process (no os/exec, no syscall: hooks_test.go checks the imports).
//
// Files of the control dir <e2e-dir> (the --e2e-dir of the run):
//
//   - picks.jsonl (written by the runner, read here): one line per open dialog, in the order the dialogs open:
//     {"dialog":"recording","paths":["fixtures/clip.wav"]}. Every line must end with '\n': a last line without it
//     may be half-written, so it is not used ("no pick left") until its newline is there. dialog is one of the Dialog* names; paths are relative,
//     '/' or '\' separated, and must name regular files below <e2e-dir>/fixtures with no link, junction or other
//     reparse point on the way. One path for a single-file dialog; "notes" in its multiple mode takes several.
//   - consumed.jsonl (appended here): one line per used line of picks.jsonl, {"line":1,"dialog":"recording",
//     "result":"picked"} or {"line":2,"dialog":"notes","result":"cancelled","reason":"..."}. Line n of picks.jsonl
//     is the next one when consumed.jsonl has n-1 lines, also across restarts of the app. A dialog that finds a line
//     always uses it, whatever the outcome; a dialog with no line left uses nothing. Every refusal answers
//     «cancelled» (the native Cancel: no path, no error) and logs an error line with the reason.
//   - saved/ (created here): a save dialog answers <e2e-dir>/saved/<proposed name>, the name reduced to a safe base
//     name; an existing regular file there is replaced by the writer.
//   - opened.jsonl (appended here): {"kind":"link|file|call|clipboard","value":"...","at":"<RFC3339>"} for every
//     link, library file, call link and clipboard text the app would have opened or copied; nothing is opened.
//
// Limits the runner must keep: hard links inside fixtures/ and saved/ are not detected (only the trusted runner writes
// the control dir), and the page's own navigator.clipboard writes are not hooked here (TASK-375 stubs them), so the
// runner must not give the page real clipboard access.
package e2ehooks
