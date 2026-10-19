package values

import "paratech/desktop/internal/domain/shared"

// PostProcessJobKeyPrefix is the start of the idempotency key of every post-processing job of type typ
// (SummarizeJobType or ExtractHomeworkJobType) for transcript t, "<type>:<transcript id>:"; the prompts version
// follows it. Readers find the jobs of a transcript by it, whatever the prompts version.
func PostProcessJobKeyPrefix(typ string, t shared.TranscriptID) string {
	return typ + ":" + string(t) + ":"
}
