package whispercli

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// whisper-cli flags used by the adapter, verified against examples/cli/cli.cpp v1.9.5 (docs/refs/whisper.md).
// The CLI has no "--" end-of-options marker; every value follows its flag, which consumes the next argument
// verbatim, and values never start with '-'.
const (
	flagModel         = "-m"
	flagFile          = "-f"
	flagOutputFile    = "-of"
	flagOutputJSON    = "-oj"
	flagPrintProgress = "-pp"
	flagNoGPU         = "-ng"
	flagThreads       = "-t"
	flagLanguage      = "-l"

	// Voice activity detection (TASK-222, ADR-0031): only the speech goes to the model, silence and pauses are
	// skipped; timestamps stay those of the whole recording. The other -v* options keep the CLI defaults.
	flagVAD      = "--vad"
	flagVADModel = "-vm"

	// jsonExt is appended by the CLI to the -of prefix.
	jsonExt = ".json"
)

// buildArgs returns the argument vector after the program name; vadModel "" runs without voice activity detection.
// All values are validated by the caller.
func buildArgs(model, audio, outPrefix string, threads int, language, vadModel string) []string {
	args := []string{
		flagModel, model,
		flagFile, audio,
		flagOutputFile, outPrefix,
		flagOutputJSON,
		flagPrintProgress,
		flagNoGPU,
		flagThreads, strconv.Itoa(threads),
		flagLanguage, language,
	}
	if vadModel != "" {
		args = append(args, flagVAD, flagVADModel, vadModel)
	}
	return args
}

// checkArg rejects values that could change the meaning of the argument vector: control characters (newlines,
// NUL), a leading '-' (a flag) or '@' (a response file) and, on Windows, characters the CLI's ANSI command line
// would not receive unchanged.
func checkArg(s string) error {
	switch {
	case s == "":
		return fmt.Errorf("%w: empty argument", ErrInvalidInput)
	case !utf8.ValidString(s):
		return fmt.Errorf("%w: argument is not valid UTF-8", ErrInvalidInput)
	case strings.HasPrefix(s, "-"), strings.HasPrefix(s, "@"):
		return fmt.Errorf("%w: argument starts with %q", ErrInvalidInput, s[:1])
	}

	for _, r := range s {
		if unicode.IsControl(r) {
			return fmt.Errorf("%w: argument contains a control character", ErrInvalidInput)
		}
	}

	return checkArgEncoding(s)
}

// checkPath validates an OS path that is passed to the process: absolute, clean, strictly below root, safe as
// an argument. Messages do not contain the path.
func checkPath(root, p string) error {
	if !filepath.IsAbs(p) || filepath.Clean(p) != p {
		return fmt.Errorf("%w: path is not absolute and clean", ErrInvalidInput)
	}

	if !within(root, p) {
		return fmt.Errorf("%w: path is outside the data root", ErrInvalidInput)
	}

	return checkArg(p)
}

// within reports whether p lies strictly below root (lexically).
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != "." && filepath.IsLocal(rel)
}

// wantKind checks that a reference names an artifact of the expected kind.
func wantKind(what string, ref shared.ArtifactRef, kind shared.ArtifactKind) error {
	if ref.IsZero() || ref.Kind() != kind {
		return fmt.Errorf("%w: %w: %s must be a %s artifact", ErrInvalidInput, shared.ErrInvalidArtifactRef, what, kind)
	}
	return nil
}
