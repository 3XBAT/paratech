package wails

import (
	"context"
	"errors"

	"paratech/desktop/internal/cases/agentchats"
	"paratech/desktop/internal/domain/agent/thread"
	"paratech/desktop/internal/domain/agent/values"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the log.
const (
	opAgentChats   = "ui: agent chats"
	opAgentNewChat = "ui: agent new chat"
	opAgentRename  = "ui: agent rename chat"
	opAgentArchive = "ui: agent archive chat"
	opAgentRestore = "ui: agent restore chat"
	opAgentEarlier = "ui: agent earlier requests"
)

// AgentProblemNotFound is the problem of a chat call naming a chat that does not exist.
const AgentProblemNotFound = "not_found"

// AgentChats is the chat list (cases/agentchats, ADR-0028) as the panel uses it; satisfied by agentchats.Service.
type AgentChats interface {
	List(ctx context.Context) ([]agentchats.Chat, error)
	New(ctx context.Context) (agentchats.Chat, error)
	Rename(ctx context.Context, id shared.ThreadID, title string) (agentchats.Chat, error)
	Archive(ctx context.Context, id shared.ThreadID) (agentchats.Chat, error)
	Restore(ctx context.Context, id shared.ThreadID) (agentchats.Chat, error)
	Earlier(ctx context.Context, id shared.ThreadID) ([]agentchats.Request, error)
}

// AgentMention is an object the student mentioned with «@»: kind is subject, lesson, material or assignment; label is
// the name shown at mention time, for display only (plain text; it never goes to the Hub).
type AgentMention struct {
	Kind  string `json:"kind"`
	ID    string `json:"id"`
	Label string `json:"label"`
}

// AgentChatView is a line of the chat list. LastActive is RFC 3339 in UTC; Running is true while the chat's request
// runs (its updates keep coming with its threadId). Title is the student's text or «Новый чат», plain text only.
type AgentChatView struct {
	ThreadID   string `json:"threadId"`
	Title      string `json:"title"`
	Archived   bool   `json:"archived"`
	LastActive string `json:"lastActive"`
	Running    bool   `json:"running"`
}

// AgentChatsView is the chat list: the unarchived chats by last activity, newest first, then the archive. Problem is ""
// or failed.
type AgentChatsView struct {
	Problem string          `json:"problem"`
	Chats   []AgentChatView `json:"chats"`
}

// AgentChatResult is the outcome of a chat call: Problem is "", invalid (a malformed id or title), not_found, busy (the
// chat's request runs or waits, or the chat changed at the same moment; try again) or failed; Chat is the chat after
// the call when Problem is "".
type AgentChatResult struct {
	Problem string        `json:"problem"`
	Chat    AgentChatView `json:"chat"`
}

// AgentEarlierRequestView is a request of a chat, read-only: the student's text, the mentions with the names shown at
// mention time and the reply line (what the student was shown, plain text; "" when no card was shown). Current marks
// the chat's current request, the only one with buttons.
type AgentEarlierRequestView struct {
	Turn     int            `json:"turn"`
	Text     string         `json:"text"`
	Mentions []AgentMention `json:"mentions"`
	Reply    string         `json:"reply"`
	Current  bool           `json:"current"`
}

// AgentEarlierView is the requests of a chat, oldest first, the current one last. Problem is "", invalid, not_found or
// failed.
type AgentEarlierView struct {
	Problem  string                    `json:"problem"`
	Requests []AgentEarlierRequestView `json:"requests"`
}

// Chats returns the chat list; it reads only the local database.
func (s *AgentService) Chats(ctx context.Context) AgentChatsView {
	list, err := s.chats.List(ctx)
	if err != nil {
		s.log.Warn(opAgentChats, "err", err)
		return AgentChatsView{Problem: AgentProblemFailed, Chats: []AgentChatView{}}
	}
	out := make([]AgentChatView, 0, len(list))
	for _, c := range list {
		out = append(out, chatView(c))
	}
	return AgentChatsView{Chats: out}
}

// NewChat is «Новый чат»: the current chat when it is still empty, else a new one, which becomes current.
func (s *AgentService) NewChat(ctx context.Context) AgentChatResult {
	c, err := s.chats.New(ctx)
	return s.chatResult(opAgentNewChat, c, err)
}

// RenameChat sets the title the student typed (1..80 characters, one line); busy while the chat's request runs.
func (s *AgentService) RenameChat(ctx context.Context, threadID, title string) AgentChatResult {
	return s.onChat(opAgentRename, threadID, func(id shared.ThreadID) (agentchats.Chat, error) {
		return s.chats.Rename(ctx, id, title)
	})
}

// ArchiveChat is «В архив»; busy while the chat's request is not final.
func (s *AgentService) ArchiveChat(ctx context.Context, threadID string) AgentChatResult {
	return s.onChat(opAgentArchive, threadID, func(id shared.ThreadID) (agentchats.Chat, error) {
		return s.chats.Archive(ctx, id)
	})
}

// RestoreChat is «Вернуть»: the chat leaves the archive.
func (s *AgentService) RestoreChat(ctx context.Context, threadID string) AgentChatResult {
	return s.onChat(opAgentRestore, threadID, func(id shared.ThreadID) (agentchats.Chat, error) {
		return s.chats.Restore(ctx, id)
	})
}

// ChatEarlier returns the requests of a chat as text only (ADR-0028); the context ring is not here (TASK-251).
func (s *AgentService) ChatEarlier(ctx context.Context, threadID string) AgentEarlierView {
	id := shared.ThreadID(threadID)
	if id.Validate() != nil {
		return AgentEarlierView{Problem: AgentProblemInvalid, Requests: []AgentEarlierRequestView{}}
	}
	reqs, err := s.chats.Earlier(ctx, id)
	if err != nil {
		return AgentEarlierView{Problem: s.chatProblem(opAgentEarlier, err), Requests: []AgentEarlierRequestView{}}
	}
	out := make([]AgentEarlierRequestView, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, AgentEarlierRequestView{
			Turn: r.Turn, Text: r.Text, Mentions: mentionViews(r.Mentions), Reply: r.Reply, Current: r.Current,
		})
	}
	return AgentEarlierView{Requests: out}
}

func (s *AgentService) onChat(op, threadID string, run func(shared.ThreadID) (agentchats.Chat, error)) AgentChatResult {
	id := shared.ThreadID(threadID)
	if id.Validate() != nil {
		return AgentChatResult{Problem: AgentProblemInvalid}
	}
	c, err := run(id)
	return s.chatResult(op, c, err)
}

func (s *AgentService) chatResult(op string, c agentchats.Chat, err error) AgentChatResult {
	if err != nil {
		return AgentChatResult{Problem: s.chatProblem(op, err)}
	}
	return AgentChatResult{Chat: chatView(c)}
}

// chatProblem sorts an error of a chat call into the problems the panel knows; an unknown one is logged as failed.
func (s *AgentService) chatProblem(op string, err error) string {
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return AgentProblemNotFound
	case errors.Is(err, agentchats.ErrInvalidInput), errors.Is(err, thread.ErrInvalidTitle):
		return AgentProblemInvalid
	case errors.Is(err, agentchats.ErrBusy), errors.Is(err, thread.ErrInvalidTransition),
		errors.Is(err, shared.ErrVersionConflict):
		return AgentProblemBusy
	}
	s.log.Warn(op, "err", err)
	return AgentProblemFailed
}

func chatView(c agentchats.Chat) AgentChatView {
	return AgentChatView{
		ThreadID: string(c.ID), Title: c.Title, Archived: c.Archived, LastActive: timeText(c.LastActive), Running: c.Running,
	}
}

func mentionViews(ms []values.Mention) []AgentMention {
	out := make([]AgentMention, 0, len(ms))
	for _, m := range ms {
		out = append(out, AgentMention{Kind: string(m.Ref().Kind()), ID: m.Ref().ID(), Label: m.Label()})
	}
	return out
}

// mentionsOf checks the mentions of an Ask: at most values.MaxMentions, each a known kind with a valid id and label.
func mentionsOf(in []AgentMention) ([]values.Mention, bool) {
	if len(in) > values.MaxMentions {
		return nil, false
	}
	var out []values.Mention
	for _, m := range in {
		ref, err := values.NewRef(values.IDKind(m.Kind), m.ID)
		if err != nil {
			return nil, false
		}
		mention, err := values.NewMention(ref, m.Label)
		if err != nil {
			return nil, false
		}
		out = append(out, mention)
	}
	return out, true
}
