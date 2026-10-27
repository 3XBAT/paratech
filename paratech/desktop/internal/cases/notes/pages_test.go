package notes

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/note"
	"paratech/desktop/internal/domain/shared"
)

// A photo of handwriting is a part like a file; the note counts its pages without text, in order with the others.
func TestAddPhotoPages(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	day := AnchorRef{Kind: note.AnchorDay, Date: date(t, 2026, 10, 20)}
	res, err := e.svc.Add(ctx, AddRequest{Anchor: day, Subject: phys, Uploads: []Upload{
		{Name: "стр1.jpg", Data: []byte("\xff\xd8\xff page 1")},
		{Name: "схема.txt", Data: []byte("схема")},
		{Name: "стр2.png", Data: []byte("\x89PNG\r\n\x1a\n page 2")},
	}})
	if err != nil || len(res.Parts) != 3 {
		t.Fatalf("Add = %+v, %v", res, err)
	}
	v := res.View
	if v.NoTextPages != 2 || len(v.Parts) != 3 || v.Parts[0].Type() != material.TypeJPEG || v.Parts[2].Type() != material.TypePNG {
		t.Fatalf("view: %d pages without text, parts %v", v.NoTextPages, v.Parts)
	}

	// A removed photo is not a page any more.
	if _, err := v.Parts[2].Remove(t0); err != nil {
		t.Fatal(err)
	}
	list, err := e.svc.ForSubject(ctx, phys)
	if err != nil || len(list) != 1 || list[0].NoTextPages != 1 {
		t.Fatalf("ForSubject = %+v, %v", list, err)
	}
}

func TestPageImage(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	res, err := e.svc.Add(ctx, AddRequest{Anchor: AnchorRef{Kind: note.AnchorUndated}, Subject: phys, Uploads: []Upload{
		{Name: "стр.jpg", Data: []byte("\xff\xd8\xff page")}, {Name: "a.txt", Data: []byte("текст")},
	}})
	if err != nil {
		t.Fatal(err)
	}
	photo, doc := res.View.Parts[0], res.View.Parts[1]

	p, err := e.svc.PageImage(ctx, photo.ID())
	if err != nil || p.Material.ID() != photo.ID() || string(p.Data) != string(photo.Digest()) {
		t.Fatalf("PageImage = %+v, %v", p, err)
	}
	tests := []struct {
		name string
		id   shared.MaterialID
		err  error // of the library
		want error
	}{
		{"not a photo", doc.ID(), nil, ErrMaterialNotFound},
		{"missing", "m404", nil, ErrMaterialNotFound},
		{"bad id", "../m1", nil, ErrInvalidRequest},
		{"empty id", "", nil, ErrInvalidRequest},
		{"damaged", photo.ID(), fmt.Errorf("fake: %w", material.ErrDamaged), ErrMaterialNotFound},
		{"store failed", photo.ID(), errors.New("disk"), ErrUnavailable},
	}
	for _, tt := range tests {
		e.materials.pageErr = tt.err
		if _, err := e.svc.PageImage(ctx, tt.id); !errors.Is(err, tt.want) {
			t.Errorf("%s: PageImage = %v, want %v", tt.name, err, tt.want)
		}
	}
}
