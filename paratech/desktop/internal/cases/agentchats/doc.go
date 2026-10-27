// Package agentchats is the use case of the agent chat list (ADR-0028): list the chats, start a new one, rename,
// archive and restore one, and read its earlier requests as text. A chat is an agent/thread aggregate; requests in it
// are run by cases/agentturn, which this package does not import (ARCH-20).
//
// Chats are never deleted: journal lines reference them. Earlier requests show only the student's text, its mentions
// and a reply line built from the card texts the planner wrote and the checks let through; no tool result is read.
//
// Rules that apply: ARCH-20..27. Title, archive and «new request restores» rules live in the thread aggregate.
package agentchats
