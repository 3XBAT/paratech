// Package anthropic is the Hub's LLM provider adapter for the Anthropic Messages API (non-streaming), written on
// net/http and encoding/json (facts used: docs/refs/anthropic.md). It implements the gateway's Provider port
// (Complete) and the agent step's Provider port (Step: tools, tool choice "any", prompt caching; ADR-0023).
//
// Trust rules:
//   - the API key comes only from the server configuration, is sent only in the x-api-key header and never appears
//     in errors or printed values (String, GoString, LogValue redact it);
//   - the URL is the configured base URL plus a fixed path; nothing in a request changes host, path or query, and
//     redirects are refused because net/http would forward the x-api-key header to the new host;
//   - provider failures become *values.ProviderError with fixed texts and known values only (ARCH-32); provider
//     messages, response bodies, prompts and output never go into an error;
//   - the model output is untrusted data: it is returned, never interpreted or logged.
package anthropic
