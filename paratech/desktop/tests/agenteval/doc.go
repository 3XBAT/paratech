// Package agenteval is the live agent check (level 2, TASK-135, design §3.3): the case set of student requests,
// lecture questions and «cannot do» cases, and the pure scoring of a run (routing, citations, impersonal voice) with
// its markdown report. The scoring is unit-tested in CI; the run itself (live_test.go) needs the build tag agentlive
// and a Hub, see README.md.
package agenteval
