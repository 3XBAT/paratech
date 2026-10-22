package thread

// Budget limits of one request (ADR-0021). Over a limit the request stops with «Остановлено: лимит просьбы».
const (
	MaxSteps         = 8
	MaxCallsPerStep  = 4
	MaxWrites        = 6
	MaxPaidUnplanned = 1 // paid jobs without an approved plan
	MaxInputTokens   = 60_000
)

// Budget is what a request has used so far.
type Budget struct {
	Steps          int
	Writes         int
	PaidUnplanned  int
	InputTokens    int
	QuestionRounds int
}

func (b Budget) valid() bool {
	return b.Steps >= 0 && b.Steps <= MaxSteps && b.Writes >= 0 && b.Writes <= MaxWrites &&
		b.PaidUnplanned >= 0 && b.PaidUnplanned <= MaxPaidUnplanned && b.InputTokens >= 0 &&
		b.QuestionRounds >= 0 && b.QuestionRounds <= maxRounds
}

// History window sent to the planner: the last turns of this thread only.
const (
	MaxHistoryTurns = 12
	MaxHistoryBytes = 24 << 10
)

// WindowStart returns the index of the oldest turn sent in full, given the sizes of the turns oldest first: at most
// MaxHistoryTurns turns and MaxHistoryBytes bytes, counted from the newest. Turns before the index shrink to action
// lines; len(sizes) means none is sent in full.
func WindowStart(sizes []int) int {
	total := 0
	start := len(sizes)
	for i := len(sizes) - 1; i >= 0 && len(sizes)-i <= MaxHistoryTurns; i-- {
		if sizes[i] < 0 || total+sizes[i] > MaxHistoryBytes {
			break
		}
		total += sizes[i]
		start = i
	}
	return start
}
