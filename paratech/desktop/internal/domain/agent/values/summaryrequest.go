package values

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"paratech/desktop/internal/domain/shared"
)

// Limits of a chat summary request: the Hub contract of OPERATION_TYPE_CHAT_SUMMARY (gateway.proto, TASK-267; the
// Hub's chatsummary reader refuses anything beyond them). Sizes are bytes of UTF-8; the previous summary is bounded by
// MaxSummaryBytes as a HistorySummary is.
const (
	// MaxSummaryDataBytes bounds the whole untrusted data as encoded (EncodedSize).
	MaxSummaryDataBytes = 64 << 10
	// MaxSummaryPairs bounds the requests summarized in one call (two window blocks).
	MaxSummaryPairs = 10
	// MaxSummaryStudentBytes bounds the student's text of one pair.
	MaxSummaryStudentBytes = 4 << 10
	// MaxSummaryReplyBytes bounds the reply of one pair.
	MaxSummaryReplyBytes = 2 << 10
)

var (
	// ErrInvalidSummaryRequest is returned for a summary request that breaks the Hub limits.
	ErrInvalidSummaryRequest = errors.New("invalid summary request")

	// ErrSummaryRefused means the Hub refused the model's summary for good: it is charged, and the same key replays the
	// refusal (the Summarizer port's errors wrap it).
	ErrSummaryRefused = errors.New("the summary answer was refused")
)

// SummaryPair is one earlier request to summarize: the student's texts and the agent reply it got. Both are data.
type SummaryPair struct {
	Student string
	Reply   string
}

// SummaryRequest asks the Hub for the summary of requests 1…Through of a chat: the previous summary (of the requests
// before the pairs) and the pairs after it. Build it with NewSummaryRequest; the zero value is invalid.
type SummaryRequest struct {
	thread   shared.ThreadID
	previous string
	through  int
	pairs    []SummaryPair
	hash     string // of the data sent, for Key
}

// keyHashLen is the hex digits of the data hash in Key: enough to tell the data of one chat and Through apart.
const keyHashLen = 8

// NewSummaryRequest checks the request against the Hub limits: a valid thread, Through above the previous summary's,
// 1..MaxSummaryPairs pairs of valid UTF-8 without NUL, a student text that is not blank and at most
// MaxSummaryStudentBytes, a reply of at most MaxSummaryReplyBytes, and at most MaxSummaryDataBytes encoded.
func NewSummaryRequest(thread shared.ThreadID, previous HistorySummary, through int, pairs []SummaryPair) (SummaryRequest, error) {
	if err := thread.Validate(); err != nil {
		return SummaryRequest{}, fmt.Errorf("%w: %w", ErrInvalidSummaryRequest, err)
	}
	if through < 1 || through <= previous.Through() {
		return SummaryRequest{}, fmt.Errorf("%w: through must be past the previous summary", ErrInvalidSummaryRequest)
	}
	if len(pairs) == 0 || len(pairs) > MaxSummaryPairs {
		return SummaryRequest{}, fmt.Errorf("%w: 1..%d pairs", ErrInvalidSummaryRequest, MaxSummaryPairs)
	}
	for _, p := range pairs {
		if !summaryPart(p.Student, MaxSummaryStudentBytes) || strings.TrimSpace(p.Student) == "" ||
			!summaryPart(p.Reply, MaxSummaryReplyBytes) {
			return SummaryRequest{}, fmt.Errorf("%w: a pair is {student: 1-%d bytes, not blank, reply: 0-%d bytes} of UTF-8 without NUL",
				ErrInvalidSummaryRequest, MaxSummaryStudentBytes, MaxSummaryReplyBytes)
		}
	}
	data := summaryData(previous.Text(), pairs)
	if len(data) > MaxSummaryDataBytes {
		return SummaryRequest{}, fmt.Errorf("%w: the data is over %d bytes", ErrInvalidSummaryRequest, MaxSummaryDataBytes)
	}
	sum := sha256.Sum256(data)
	return SummaryRequest{
		thread: thread, previous: previous.Text(), through: through, pairs: append([]SummaryPair(nil), pairs...),
		hash: hex.EncodeToString(sum[:])[:keyHashLen],
	}, nil
}

func summaryPart(s string, limit int) bool {
	return len(s) <= limit && utf8.ValidString(s) && !strings.ContainsRune(s, 0)
}

// SummaryEncodedSize is the size of the untrusted data the hubclient adapter sends for previous and pairs: the JSON
// object {"summary":…,"pairs":[{"student":…,"reply":…}]} written by encoding/json without HTML escaping and without
// a trailing line break. The adapter test pins that its encoding has exactly this size.
func SummaryEncodedSize(previous HistorySummary, pairs []SummaryPair) int {
	return len(summaryData(previous.Text(), pairs))
}

// summaryData is the data the adapter sends, as SummaryEncodedSize describes it.
func summaryData(previous string, pairs []SummaryPair) []byte {
	type pair struct {
		Student string `json:"student"`
		Reply   string `json:"reply"`
	}
	data := struct {
		Summary string `json:"summary"`
		Pairs   []pair `json:"pairs"`
	}{Summary: previous, Pairs: make([]pair, len(pairs))}
	for i, p := range pairs {
		data.Pairs[i] = pair(p)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	// Only strings: encoding cannot fail.
	_ = enc.Encode(data)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n")) // Encode ends with a line break
}

// Key returns the idempotency key cs:<thread>:<through>:<hash8>, hash8 the first 8 lowercase hex digits of the SHA-256
// of the data sent. The Hub keeps a key's answer (and a refusal) for its retention: the same requests summarized again
// after a «Скрыть» or a restart changed their texts are other data, so they get another key, not the old answer or an
// idempotency conflict.
func (r SummaryRequest) Key() string {
	return "cs:" + string(r.thread) + ":" + strconv.Itoa(r.through) + ":" + r.hash
}

// Thread returns the chat.
func (r SummaryRequest) Thread() shared.ThreadID { return r.thread }

// Through returns the last request the new summary covers.
func (r SummaryRequest) Through() int { return r.through }

// Previous returns the text of the previous summary; empty when there is none.
func (r SummaryRequest) Previous() string { return r.previous }

// Pairs returns a copy of the pairs, oldest first.
func (r SummaryRequest) Pairs() []SummaryPair { return append([]SummaryPair(nil), r.pairs...) }

// EncodedSize is SummaryEncodedSize of the request.
func (r SummaryRequest) EncodedSize() int {
	return SummaryEncodedSize(HistorySummary{text: r.previous}, r.pairs)
}

// Equal reports whether o asks for the same summary: the same chat, Through, previous summary and pairs.
func (r SummaryRequest) Equal(o SummaryRequest) bool {
	return r.thread == o.thread && r.through == o.through && r.previous == o.previous && slices.Equal(r.pairs, o.pairs)
}

// IsZero reports whether the request is the zero value.
func (r SummaryRequest) IsZero() bool { return r.through == 0 }
