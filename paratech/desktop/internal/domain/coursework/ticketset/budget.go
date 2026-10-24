package ticketset

// Cost of a run. A ticket is one answer with citations on the strong model (Sonnet): at most answer.MaxFragments
// passages in and answer.MaxOutputTokens out. The figures are a conservative worst case, not a price list: the Hub
// charges the real use against the quota.
const (
	// TicketCostMicroUSD is the estimate of one ticket: ~10k input tokens at $3/M and 4096 output tokens at $15/M.
	TicketCostMicroUSD int64 = 90_000
	// RunCapMicroUSD is the estimate above which the student must accept the cost of the run explicitly.
	RunCapMicroUSD int64 = 3_000_000
)

// Budget is the estimate of a run as the confirm card shows it.
type Budget struct {
	Tickets          int   // the tickets the run asks about
	EstimateMicroUSD int64 // the worst case of the run
	CapMicroUSD      int64
	OverCap          bool // the student must accept the cost to confirm
}

// Estimate returns the budget of a run of tickets tickets (at most MaxTickets, so a run is bounded).
func Estimate(tickets int) Budget {
	tickets = max(0, min(tickets, MaxTickets))
	cost := int64(tickets) * TicketCostMicroUSD
	return Budget{Tickets: tickets, EstimateMicroUSD: cost, CapMicroUSD: RunCapMicroUSD, OverCap: cost > RunCapMicroUSD}
}
