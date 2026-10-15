// Separate module on purpose: malgo needs CGO, and the desktop module must not depend on it (CLAUDE.md hard
// rule 1). Not part of go.work; build with GOWORK=off CGO_ENABLED=1.
module paratech/desktop/spikes/audio-capture/malgo

go 1.26.9

require github.com/gen2brain/malgo v0.11.26
