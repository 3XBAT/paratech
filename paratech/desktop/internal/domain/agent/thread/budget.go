package thread

// Budget limits of one request (ADR-0021). Over a limit the request stops with «Остановлено: лимит просьбы».
const (
	MaxSteps         = 8
	MaxCallsPerStep  = 4
	MaxWrites        = 6
	MaxPaidUnplanned = 1 // paid jobs without an approved plan
	MaxInputTokens   = 60_000
	MaxRetries       = 3 // «Повторить» of one failed step; each retry is a new paid attempt on the Hub
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
