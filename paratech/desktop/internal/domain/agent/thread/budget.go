package thread

// Budget limits of one request (ADR-0021). Over a limit the request stops with «Остановлено: лимит просьбы».
const (
	MaxSteps         = 8
	MaxCallsPerStep  = 4
	MaxWrites        = 6 // writes outside an approved plan; steps of an approved plan are not counted (ADR-0033 §4)
	MaxPaidUnplanned = 1 // paid jobs without an approved plan
	MaxRetries       = 3 // «Повторить» of one failed step; each retry is a new paid attempt on the Hub

	// MaxStepInputTokens is the planner input allowance of one step. Every step resends the whole prefix (catalog,
	// history window, ~13k tokens since catalog 8) and the Hub reports cache reads/writes as input, so the request
	// limit is the allowance of all MaxSteps: a normal request fits, a growing context still stops early.
	MaxStepInputTokens = 20_000
	MaxInputTokens     = MaxSteps * MaxStepInputTokens

	// MaxPlanRunSteps and MaxPlanInputTokens replace MaxSteps and MaxInputTokens while the request has an approved
	// plan: the student's «Да» covers every step, so these are runaway guards, not product limits (ADR-0033 §4). Each
	// step is still a separate quota reservation on the Hub.
	MaxPlanRunSteps    = 60
	MaxPlanInputTokens = 1_000_000
)

// Budget is what a request has used so far.
type Budget struct {
	Steps          int
	Writes         int
	PaidUnplanned  int
	InputTokens    int
	QuestionRounds int
}

// valid accepts both modes: a request may have run up to MaxPlanRunSteps steps under a plan it later revised.
func (b Budget) valid() bool {
	return b.Steps >= 0 && b.Steps <= MaxPlanRunSteps && b.Writes >= 0 && b.Writes <= MaxWrites &&
		b.PaidUnplanned >= 0 && b.PaidUnplanned <= MaxPaidUnplanned && b.InputTokens >= 0 &&
		b.QuestionRounds >= 0 && b.QuestionRounds <= maxRounds
}

// stepLimits returns the step and input token limits of the request: the plan limits while it has an approved plan,
// also after its last step ran (the closing reply is one more step). RevisePlan drops the plan, so a request that ran
// more than MaxSteps steps and then revised stops at its next step.
func (t *Turn) stepLimits() (steps, inputTokens int) {
	if len(t.approved) > 0 {
		return MaxPlanRunSteps, MaxPlanInputTokens
	}
	return MaxSteps, MaxInputTokens
}
