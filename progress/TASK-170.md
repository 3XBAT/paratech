# TASK-170: blocked (owner decision needed)

## Result
No hole in the domain rule for unplanned paid work. Real database not opened (supervisor limit); conclusion is from code and tests.

## Path found (from code)
- Only two paths create a homework_draft job: the «Решить» button (input/wails/homework.go Generate) and the agent call generate_draft (input/agent/actions.go). One call = at most one job, same assignment returns the same job. Five jobs = five admitted calls for five assignments.
- Only calls admitted by thread.Admit run. In production input/agent/facts.go never sets a cost estimate (zero = unknown), and actionpolicy.NeedsPlan treats unknown as costly, so a paid write without an approved plan is always ErrPlanRequired, in any request (StartTurn, Rerun, Restart). PaidUnplanned stays 0.
- So the five drafts must have been steps of an approved plan: PlanStep.Matches is strict (one step cannot match two assignments), used flags are persisted, a plan has up to 8 steps and up to MaxWrites (6) writes, no separate limit on paid steps, and the plan card shows no price. Five drafts in ~3 s fits two planner steps (up to 4 calls each) after one «Да». The only way to approve is the «Да» button (AgentPanel.tsx). Unverified guess: the planner's summary text hid that the plan listed five paid jobs.

## Owner questions
a) Limit paid work inside an approved plan: at most N paid steps per plan, and/or show a price on the plan card (needs a cost estimate, none exists today).
b) Count paid work per thread, not per request. Only matters once real cost estimates exist; could be derived in StartTurn from the previous request (no new column).
c) Confirm from a read-only copy of the owner's DB: one agent_turns row with approved_plan of five generate_draft steps, used all true, paid_unplanned = 0. If not, the path is elsewhere.

## Done
Regression tests only (no domain change, no existing test changed):
- domain/agent/thread/paidwork_test.go: five drafts in two steps without a plan (unknown cost: none run; cheap estimate: only the first), across requests (StartTurn/Rerun/Restart), approved-plan cases (plan size cap, not-in-plan, used step, call_id reuse, persistence of used flags).
- domain/agent/services/actionpolicy/policy_test.go: TestPaidWriteWithoutPlan.

## Follow-ups
Product decision on (a)/(b), then a domain change plus tests. TASK-171 owns cases/agentturn and input/agent; untouched here.

Security: no change to production code; no new attack surface.
