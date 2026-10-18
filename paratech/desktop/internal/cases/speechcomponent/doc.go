// Package speechcomponent is the use case of the speech recognition component (TASK-072): the pinned files of the
// local STT engine (whisper-cli and its libraries, docs/refs/whisper.md). Service.Detect finds a verified set at
// start, first next to the executable, then in the component folder of the data directory, and activates speech
// recognition through the Activator port. When the set is missing or damaged, Service.StartInstall downloads the
// pinned release archive from the hosts of the manifest only, checks its size and SHA-256 before it is opened, has
// only the component files unpacked into the component folder, verifies the installed set and activates it without a
// restart. Nothing from the archive runs before the whole set verifies.
//
// Rules that apply: ARCH-20..27.
package speechcomponent
