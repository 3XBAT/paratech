package answer

import (
	"fmt"
	"slices"
	"time"

	"paratech/desktop/internal/domain/coursework/services/citation"
	"paratech/desktop/internal/domain/shared"
)

// Cite is a citation that passed the check: the quote and the index of its place in Answer.Sources.
type Cite struct {
	Quote  string
	Source int
}

// Claim is one statement of the answer with the citations that passed the check. A claim without one is unsupported:
// the lessons do not show it, so it is never shown as «из ваших лекций».
type Claim struct {
	Text  string
	Cites []Cite
}

// Supported reports whether a citation of the claim passed the check.
func (c Claim) Supported() bool { return len(c.Cites) > 0 }

// State is the stored form of an answer, for the repository. Fragments is how many fragments were sent.
type State struct {
	ID        shared.AnswerID
	Key       string
	Subject   shared.SubjectID // "" for a question about the whole base
	Question  string
	Claims    []Claim
	Missing   []string
	Sources   []Source
	Fragments int
	CreatedAt time.Time
}

// Answer is an answer with checked citations. It never changes once made.
type Answer struct {
	st State
}

// New makes the answer of req from the reader's reply rep: each citation is kept only if its quote occurs in the
// fragment it names (citation.Matches), the places of the kept ones become the sources, in the order first cited.
// subject is the subject the question was about, "" for the whole base. Errors: ErrInvalidState, ErrInvalidAnswer
// for a reply that does not belong to req.
func New(id shared.AnswerID, subject shared.SubjectID, req Request, rep Reply, now time.Time) (*Answer, error) {
	if req.IsZero() {
		return nil, fmt.Errorf("%w: no request", ErrInvalidState)
	}
	st := State{
		ID: id, Key: req.Key(), Subject: subject, Question: req.Question(), Claims: []Claim{},
		Missing: slices.Clone(rep.Missing), Sources: []Source{}, Fragments: len(req.fragments), CreatedAt: now,
	}
	if st.Missing == nil {
		st.Missing = []string{}
	}
	sourceOf := map[string]int{} // chunk ID -> index in st.Sources
	for _, rc := range rep.Claims {
		c := Claim{Text: rc.Text, Cites: []Cite{}}
		for _, cite := range rc.Cites {
			f, ok := req.fragment(cite.ChunkID)
			if !ok {
				return nil, fmt.Errorf("%w: a citation of a fragment that was not sent", ErrInvalidAnswer)
			}
			if !citation.Matches(cite.Quote, f.Text) {
				continue
			}
			i, ok := sourceOf[f.ChunkID]
			if !ok {
				i = len(st.Sources)
				sourceOf[f.ChunkID] = i
				st.Sources = append(st.Sources, f.Source)
			}
			kept := Cite{Quote: cite.Quote, Source: i}
			if !slices.Contains(c.Cites, kept) {
				c.Cites = append(c.Cites, kept)
			}
		}
		st.Claims = append(st.Claims, c)
	}
	if err := check(st); err != nil {
		return nil, err
	}
	return &Answer{st: st}, nil
}

// Restore rebuilds a stored answer, checking its invariants; the quotes are not matched again (the fragments are not
// kept).
func Restore(st State) (*Answer, error) {
	st = cloneState(st)
	if err := check(st); err != nil {
		return nil, err
	}
	return &Answer{st: st}, nil
}

func check(st State) error {
	bad := func(what string) error { return fmt.Errorf("%w: %s", ErrInvalidState, what) }
	switch {
	case st.ID.Validate() != nil:
		return bad("id")
	case !validKey(st.Key):
		return bad("key")
	case st.Subject != "" && st.Subject.Validate() != nil:
		return bad("subject")
	case st.CreatedAt.IsZero():
		return bad("created at")
	case st.Fragments < 1 || st.Fragments > MaxFragments || len(st.Sources) > st.Fragments:
		return bad("fragments")
	case len(st.Claims) > MaxClaims || len(st.Missing) > MaxMissing || len(st.Claims)+len(st.Missing) == 0:
		return bad("claims and missing parts")
	}
	if q, err := ParseQuestion(st.Question); err != nil || q != st.Question {
		return bad("question")
	}
	for _, s := range st.Sources {
		if err := s.validate(); err != nil {
			return err
		}
	}
	for _, c := range st.Claims {
		if !boundedText(c.Text, MaxClaimRunes) || len(c.Cites) > MaxCites {
			return bad("claim")
		}
		for _, cite := range c.Cites {
			if cite.Source < 0 || cite.Source >= len(st.Sources) || !boundedText(cite.Quote, MaxQuoteRunes) {
				return bad("citation")
			}
		}
	}
	for _, m := range st.Missing {
		if !boundedText(m, MaxMissingRunes) {
			return bad("missing part")
		}
	}
	return nil
}

func cloneState(st State) State {
	out := st
	out.Claims = make([]Claim, 0, len(st.Claims))
	for _, c := range st.Claims {
		c.Cites = append([]Cite{}, c.Cites...)
		out.Claims = append(out.Claims, c)
	}
	out.Missing = append([]string{}, st.Missing...)
	out.Sources = append([]Source{}, st.Sources...)
	return out
}

// ID returns the identifier.
func (a *Answer) ID() shared.AnswerID { return a.st.ID }

// Key returns the idempotency key of the request it answers.
func (a *Answer) Key() string { return a.st.Key }

// Subject returns the subject the question was about, "" for the whole base.
func (a *Answer) Subject() shared.SubjectID { return a.st.Subject }

// Question returns the question.
func (a *Answer) Question() string { return a.st.Question }

// Claims returns a copy of the claims, supported or not, in the reader's order.
func (a *Answer) Claims() []Claim { return cloneState(a.st).Claims }

// Missing returns the parts of the question the lessons do not answer.
func (a *Answer) Missing() []string { return slices.Clone(a.st.Missing) }

// Sources returns the places the kept citations open.
func (a *Answer) Sources() []Source { return slices.Clone(a.st.Sources) }

// CreatedAt returns when the answer was made.
func (a *Answer) CreatedAt() time.Time { return a.st.CreatedAt }

// State returns a copy of the stored form.
func (a *Answer) State() State { return cloneState(a.st) }

// Counts are what the planner may know of an answer (ADR-0022): numbers only, never its text.
type Counts struct {
	Fragments   int // fragments sent to the reader
	Claims      int
	Supported   int // claims with a citation that passed the check
	Unsupported int
	Missing     int // parts of the question left unanswered
	Sources     int // places in the lessons the citations open
}

// Counts returns the counts of the answer.
func (a *Answer) Counts() Counts {
	c := Counts{Fragments: a.st.Fragments, Claims: len(a.st.Claims), Missing: len(a.st.Missing), Sources: len(a.st.Sources)}
	for _, cl := range a.st.Claims {
		if cl.Supported() {
			c.Supported++
		} else {
			c.Unsupported++
		}
	}
	return c
}
