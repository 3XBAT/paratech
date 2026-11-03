package actionpolicy

import "paratech/desktop/internal/domain/agent/values"

// DecideStep is Decide for a step of show_plan: its tool and the action enum the parser checked. A plan is proposed
// before the facts of its steps are read, so the thread decides a step with the zero Facts; Admit decides the call
// again with the real ones when the step runs.
func DecideStep(s values.PlanStep, f Facts) (Decision, error) {
	act, _ := s.Args().Enum(values.ArgAction)
	return decide(s.Tool(), act, f)
}
