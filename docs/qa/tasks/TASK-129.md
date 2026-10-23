# TASK-129: check of turnkey triage (backend only, no UI yet)

Not checked by the agents against the real Hub or app. The card UI comes in TASK-130, so only the agent panel can be tried:

1. In the agent panel ask to analyse an assignment from the library ("разбери задание"). The agent proposes a plan with triage_assignment; approve it (it is paid).
2. A work card appears with the outcome. A second request for the same assignment reuses the work, no second charge.
3. Undo of the step removes the work while nothing was answered or edited.
4. Questions come at most in 2 rounds; after the 2nd round the request goes on with stated assumptions (no 3rd round, no extra charge).
5. A task the reader calls impossible goes to helper mode (steps, pitfalls) without asking.
6. «Спросить по шагу» answers «Вопросы по шагу пока недоступны.» (no backend yet).
