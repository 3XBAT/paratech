package notes

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/domain/library/note"
)

func TestAddToAFullNoteStoresNothing(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	names := make([]string, 0, note.MaxParts-1)
	for i := range note.MaxParts - 1 {
		names = append(names, fmt.Sprintf("p%d.txt", i))
	}
	day := AnchorRef{Kind: note.AnchorDay, Date: date(t, 2026, 10, 1)}
	res, err := e.svc.Add(ctx, AddRequest{Anchor: day, Subject: phys, Uploads: uploads(names...)})
	if err != nil {
		t.Fatal(err)
	}
	id := res.View.Note.ID()

	tests := []struct {
		name string
		req  AddRequest
	}{
		{"by note id", AddRequest{Note: id, Uploads: uploads("x.txt", "y.txt")}},
		{"by lecture", AddRequest{Anchor: day, Subject: phys, Uploads: uploads("x.txt", "y.txt")}},
		// A part already counts: the bound is checked before anything is filed.
		{"with a part already", AddRequest{Note: id, Uploads: uploads("p0.txt", "x.txt")}},
	}
	for _, tt := range tests {
		calls, files := e.materials.calls(), len(e.materials.rows)
		if _, err := e.svc.Add(ctx, tt.req); !errors.Is(err, ErrTooManyParts) {
			t.Fatalf("%s: Add = %v, want ErrTooManyParts", tt.name, err)
		}
		if e.materials.calls() != calls || len(e.materials.rows) != files {
			t.Fatalf("%s: files were imported for a full note", tt.name)
		}
	}

	// The last free place is still taken.
	last, err := e.svc.Add(ctx, AddRequest{Note: id, Uploads: uploads("x.txt")})
	if err != nil || len(last.View.Note.Parts()) != note.MaxParts {
		t.Fatalf("Add(last place) = %+v, %v", last, err)
	}
	calls := e.materials.calls()
	if _, err := e.svc.Add(ctx, AddRequest{Note: id, Uploads: uploads("z.txt")}); !errors.Is(err, ErrTooManyParts) {
		t.Fatalf("Add(to a full note) = %v", err)
	}
	if e.materials.calls() != calls {
		t.Fatal("a file was imported for a full note")
	}
}
