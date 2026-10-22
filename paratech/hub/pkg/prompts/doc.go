// Package prompts holds the embedded server prompts and tool catalogs of the Hub (ARCH-04, ADR-0023).
//
// Layout, the version in the path:
//
//	agent/<profile>/v<N>/system.md   system prompt of an agent profile
//	agent/<profile>/v<N>/tools.json  tool catalog version N (agent/panel/v1/tools.json is shared/testdata/tool_catalog_v1.json)
//	ops/<operation>/v<N>/system.md   server prompt of an operation served by Complete (added by the reader tasks)
//
// The package only finds and returns the bytes; app parses and checks them at start and hands them to the use cases
// as data. A prompt change is a new version directory, never an edit of a released one. The package imports only the
// standard library and may be imported only by adapters and app.
package prompts
