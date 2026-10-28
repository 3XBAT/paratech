package thread

// NeedsReplyLine reports whether the current request is final and its reply line is not frozen yet: the case then
// calls FreezeReply with the entries of the request before it saves the thread.
func (th *Thread) NeedsReplyLine() bool {
	return th.current != nil && th.current.final() && th.current.replyLine == ""
}
