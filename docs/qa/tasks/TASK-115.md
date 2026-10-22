# TASK-115: owner check

Domain code only, nothing visible in the app. Nothing to check by hand.
CI covers it: `go test -race ./desktop/...` (including the architecture test and the golden tool-catalog test).
