package anthropic

import "sync"

// ResetNoSSELog lets a test see the once-per-process "provider answered without SSE" line again.
func ResetNoSSELog() { noSSELogged = new(sync.Once) }
