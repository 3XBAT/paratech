package thread

// Budget limits of one request (ADR-0021). Over a limit the request stops with «Остановлено: лимит просьбы».
const (
	MaxSteps         = 8
	MaxCallsPerStep  = 4
	MaxWrites        = 6
	MaxPaidUnplanned = 1 // paid jobs without an approved plan
	MaxRetries       = 3 // «Повторить» of one failed step; each retry is a new paid attempt on the Hub

	// MaxStepInputTokens is the planner input allowance of one step. Every step resends the whole prefix (catalog,
	// history window, ~13k tokens since catalog 8) and the Hub reports cache reads/writes as input, so the request
	// limit is the allowance of all MaxSteps: a normal request fits, a growing context still stops early.
	MaxStepInputTokens = 20_000
	MaxInputTokens     = MaxSteps * MaxStepInputTokens
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
