// Package values holds the value objects shared by the agent aggregates (ADR-0021, ADR-0022): the closed tool
// catalog and strict parsing of tool arguments, typed ID references for the provenance check, card types, the scope
// and base sent to the planner, and the step types that cross the AgentModel port.
//
// Everything the planner returns is untrusted (hard rule 3): arguments are only IDs, enums, dates, short labels and
// card texts, each checked here before anything else looks at them.
package values
