package sqlite

import (
	"context"
	"encoding/json"
	"fmt"

	"paratech/desktop/internal/adapters/sqlite/sqlcgen"
	"paratech/desktop/internal/domain/coursework/answer"
	"paratech/desktop/internal/domain/shared"
)

// Operation names of the answer repository, used in its error messages.
const (
	opGetAnswer     = "sqlite: get answer"
	opInsertAnswer  = "sqlite: insert answer"
	opRestoreAnswer = "stored answer"
)

// AnswerRepository stores the answers with citations (the answering.Repository port, TASK-118). An answer is written
// once; claims, missing parts and sources are kept as JSON and read back through answer.Restore. Validation stays in
// the aggregate; the repository only converts rows.
type AnswerRepository struct{ db *DB }

// NewAnswerRepository returns the answer repository of db.
func NewAnswerRepository(db *DB) *AnswerRepository { return &AnswerRepository{db: db} }

// Get loads an answer; none wraps shared.ErrNotFound.
func (r *AnswerRepository) Get(ctx context.Context, id shared.AnswerID) (*answer.Answer, error) {
	row, err := r.db.Queries(ctx).GetAnswer(ctx, string(id))
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w", opGetAnswer, id, MapError(ctx, err))
	}
	return answerFromRow(row)
}

// FindByKey loads the answer of a reader request key; none wraps shared.ErrNotFound.
func (r *AnswerRepository) FindByKey(ctx context.Context, key string) (*answer.Answer, error) {
	row, err := r.db.Queries(ctx).GetAnswerByKey(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("%s by key: %w", opGetAnswer, MapError(ctx, err))
	}
	return answerFromRow(row)
}

// Insert stores a new answer; an answer of the same key (or ID) wraps shared.ErrDuplicate.
func (r *AnswerRepository) Insert(ctx context.Context, a *answer.Answer) error {
	st := a.State()
	claims, missing, sources, err := answerJSON(st)
	if err != nil {
		return fmt.Errorf("%s %s: %w", opInsertAnswer, st.ID, err)
	}
	err = r.db.Queries(ctx).InsertAnswer(ctx, sqlcgen.InsertAnswerParams{
		ID: string(st.ID), RequestKey: st.Key, SubjectID: jobNullString(string(st.Subject)), Question: st.Question,
		Fragments: int64(st.Fragments), Claims: claims, Missing: missing, Sources: sources, CreatedAt: jobMs(st.CreatedAt),
	})
	if err != nil {
		return fmt.Errorf("%s %s: %w", opInsertAnswer, st.ID, MapError(ctx, err))
	}
	return nil
}

// Stored forms of the parts of an answer.
type (
	answerCiteWire struct {
		Quote  string `json:"quote"`
		Source int    `json:"source"`
	}
	answerClaimWire struct {
		Text  string           `json:"text"`
		Cites []answerCiteWire `json:"cites"`
	}
	// answerSourceWire is one source. The note fields (TASK-153) are optional, so answers stored before them read
	// unchanged and a source of a recording is stored as it was.
	answerSourceWire struct {
		Recording   string `json:"recording"`
		Section     string `json:"section"`
		Index       int    `json:"index"`
		StartMs     int64  `json:"start_ms"`
		Title       string `json:"title"`
		StartedAt   int64  `json:"started_at"` // Unix ms; 0 when unknown
		Note        string `json:"note,omitempty"`
		Material    string `json:"material,omitempty"`
		LocatorKind string `json:"locator_kind,omitempty"`
		LocatorN    int    `json:"locator_n,omitempty"`
		Heading     string `json:"heading,omitempty"`
	}
)

func answerJSON(st answer.State) (claims, missing, sources string, err error) {
	cw := make([]answerClaimWire, 0, len(st.Claims))
	for _, c := range st.Claims {
		cites := make([]answerCiteWire, 0, len(c.Cites))
		for _, ct := range c.Cites {
			cites = append(cites, answerCiteWire(ct))
		}
		cw = append(cw, answerClaimWire{Text: c.Text, Cites: cites})
	}
	sw := make([]answerSourceWire, 0, len(st.Sources))
	for _, s := range st.Sources {
		started := int64(0)
		if !s.StartedAt.IsZero() {
			started = jobMs(s.StartedAt)
		}
		sw = append(sw, answerSourceWire{
			Recording: string(s.Recording), Section: string(s.Section), Index: s.Index, StartMs: s.StartMs,
			Title: s.Title, StartedAt: started, Note: string(s.Note), Material: string(s.Material),
			LocatorKind: string(s.Locator.Kind), LocatorN: s.Locator.N, Heading: s.Heading,
		})
	}
	m := st.Missing
	if m == nil {
		m = []string{}
	}
	parts := make([]string, 0, 3)
	for _, v := range []any{cw, m, sw} {
		b, err := json.Marshal(v)
		if err != nil {
			return "", "", "", fmt.Errorf("encode answer: %w", err)
		}
		parts = append(parts, string(b))
	}
	return parts[0], parts[1], parts[2], nil
}

func answerFromRow(row sqlcgen.Answer) (*answer.Answer, error) {
	bad := func(what string, err error) (*answer.Answer, error) {
		return nil, fmt.Errorf("%s %s: %s: %w", opRestoreAnswer, row.ID, what, err)
	}
	var (
		cw []answerClaimWire
		m  []string
		sw []answerSourceWire
	)
	if err := strictJSON(row.Claims, &cw); err != nil {
		return bad("claims", err)
	}
	if err := strictJSON(row.Missing, &m); err != nil {
		return bad("missing", err)
	}
	if err := strictJSON(row.Sources, &sw); err != nil {
		return bad("sources", err)
	}
	claims := make([]answer.Claim, 0, len(cw))
	for _, c := range cw {
		cites := make([]answer.Cite, 0, len(c.Cites))
		for _, ct := range c.Cites {
			cites = append(cites, answer.Cite(ct))
		}
		claims = append(claims, answer.Claim{Text: c.Text, Cites: cites})
	}
	sources := make([]answer.Source, 0, len(sw))
	for _, s := range sw {
		src := answer.Source{
			Recording: shared.RecordingID(s.Recording), Section: answer.Section(s.Section), Index: s.Index,
			StartMs: s.StartMs, Title: s.Title, Note: shared.NoteID(s.Note), Material: shared.MaterialID(s.Material),
			Locator: answer.Locator{Kind: answer.LocatorKind(s.LocatorKind), N: s.LocatorN}, Heading: s.Heading,
		}
		if s.StartedAt != 0 {
			src.StartedAt = jobFromMs(s.StartedAt)
		}
		sources = append(sources, src)
	}
	a, err := answer.Restore(answer.State{
		ID: shared.AnswerID(row.ID), Key: row.RequestKey, Subject: shared.SubjectID(row.SubjectID.String),
		Question: row.Question, Claims: claims, Missing: m, Sources: sources, Fragments: int(row.Fragments),
		CreatedAt: jobFromMs(row.CreatedAt),
	})
	if err != nil {
		return bad("state", err)
	}
	return a, nil
}
