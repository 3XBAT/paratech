// Package gateway is the LLM gateway use case (PRD sections 4.2, 9.2): validate a completion request, reserve quota,
// call the provider, settle or release. For now it declares only the Provider port; the scenario is a later task.
package gateway
