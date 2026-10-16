package soundcheck

import "paratech/desktop/internal/domain/recording/values"

// Result is the answer of a check: the verdict for the user's source and what each mode heard.
type Result struct {
	Verdict values.SoundCause
	Source  values.Source // the source the verdict is for
	Check   values.SoundCheck
}
