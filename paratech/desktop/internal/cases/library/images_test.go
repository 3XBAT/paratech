package library

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/library/subject"
	"paratech/desktop/internal/domain/shared"
)

// Signatures are enough for the fake cleaner; the real decoding is the imagepage adapter's.
var (
	photoJPEG = []byte("\xff\xd8\xff\xe1 fake EXIF GPS 00.0N 00.0E, then pixels")
	photoPNG  = []byte("\x89PNG\r\n\x1a\n fake tEXt, then pixels")
)

// A photo of a note is stored as the cleaner gave it, as a page without text filed under the subject.
func TestFileNotePartPhoto(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	phys := createSubject(t, e, "Физика")
	tests := []struct {
		name string
		data []byte
		typ  material.Type
	}{
		{"jpeg", photoJPEG, material.TypeJPEG},
		{"png", photoPNG, material.TypePNG},
	}
	for _, tt := range tests {
		m, existing, err := e.svc.FileNotePart(ctx, phys.ID(), "lecture", "IMG_0001."+string(tt.typ), tt.data)
		if err != nil || existing {
			t.Fatalf("%s: FileNotePart = %v, %v", tt.name, existing, err)
		}
		clean := append([]byte(cleanPrefix), tt.data...)
		if m.Type() != tt.typ || m.TextStatus() != material.TextNoText || m.Size() != int64(len(clean)) ||
			m.Digest() != material.DigestOf(clean) || m.Folder() != subject.SystemFolderID(phys.ID(), subject.SystemLectures) {
			t.Fatalf("%s: material %+v", tt.name, m.State())
		}
		ref, _ := material.BlobRef(m.Digest())
		if stored := e.blobs.files[ref.String()]; !bytes.Equal(stored, clean) {
			t.Fatalf("%s: stored %q, want the clean bytes", tt.name, stored)
		}
		raw, _ := material.BlobRef(material.DigestOf(tt.data))
		if _, ok := e.blobs.files[raw.String()]; ok {
			t.Fatalf("%s: the bytes as uploaded were stored", tt.name)
		}
	}

	// The same photo again is the same material (dedupe on the clean bytes).
	first, _, err := e.svc.FileNotePart(ctx, phys.ID(), "lecture", "again.jpg", photoJPEG)
	if err != nil {
		t.Fatal(err)
	}
	again, existing, err := e.svc.FileNotePart(ctx, phys.ID(), "seminar", "copy.jpg", photoJPEG)
	if err != nil || !existing || again.ID() != first.ID() {
		t.Fatalf("FileNotePart(same photo) = %v, %v", existing, err)
	}
	if e.texts.calls != 0 {
		t.Fatalf("the text of a photo was extracted %d times", e.texts.calls)
	}
}

func TestImportPhotoRefused(t *testing.T) {
	tests := []struct {
		name  string
		data  []byte
		clean error         // returned by the cleaner
		typ   material.Type // claimed by the cleaner
		want  error
		calls int
	}{
		{"too large", photoJPEG, material.ErrImageTooLarge, "", ErrImageTooLarge, 1},
		{"unreadable", photoPNG, material.ErrImageUnreadable, "", ErrImageUnreadable, 1},
		{"unsupported", photoPNG, material.ErrUnsupportedImage, "", ErrUnsupportedImage, 1},
		{"cleaner failed", photoJPEG, errors.New("context canceled"), "", ErrUnavailable, 1},
		{"cleaner changed the type", photoJPEG, nil, material.TypePNG, ErrUnavailable, 1},
		{"heic", []byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00"), nil, "", ErrUnsupportedImage, 0},
		{"svg", []byte(`<svg onload="x"/>`), nil, "", ErrUnsupportedImage, 0},
		{"over the image limit", append([]byte("\xff\xd8\xff"), make([]byte, material.MaxImageBytes)...), nil, "", ErrImageTooLarge, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEnv(t)
			e.images.err, e.images.typ = tt.clean, tt.typ
			_, err := e.svc.Import(context.Background(), Upload{Name: "x", Data: tt.data})
			if !errors.Is(err, tt.want) {
				t.Fatalf("Import = %v, want %v", err, tt.want)
			}
			if e.images.calls != tt.calls || len(e.blobs.files) != 0 || len(e.materials.rows) != 0 {
				t.Fatalf("cleaner calls %d, blobs %d, materials %d", e.images.calls, len(e.blobs.files), len(e.materials.rows))
			}
		})
	}

	// The image errors are still their class for callers that know only those.
	for _, c := range []struct{ err, class error }{
		{ErrImageTooLarge, ErrFileTooLarge}, {ErrUnsupportedImage, ErrUnsupportedFile}, {ErrImageUnreadable, ErrUnsupportedFile},
	} {
		if !errors.Is(c.err, c.class) {
			t.Errorf("%v does not wrap %v", c.err, c.class)
		}
	}
}

// Without the cleaner a photo is refused, never stored as it came.
func TestImportPhotoWithoutCleaner(t *testing.T) {
	e := newEnv(t)
	svc, err := New(Deps{
		Subjects: e.subjects, Materials: e.materials, Blobs: e.blobs, Lessons: e.lessons, Strings: e.strings,
		Clock: e.clock, IDs: &seqIDs{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Import(context.Background(), Upload{Data: photoJPEG}); !errors.Is(err, ErrUnsupportedImage) || len(e.blobs.files) != 0 {
		t.Fatalf("Import = %v, %d blobs", err, len(e.blobs.files))
	}
}

// A photo has no text: it is never extracted, inspected or sent to the reader, so it is not searched or cited.
func TestPhotoHasNoText(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	im, err := e.svc.Import(ctx, Upload{Name: "page.png", Data: photoPNG})
	if err != nil {
		t.Fatal(err)
	}
	id := im.Material.ID()
	if _, _, err := e.svc.MaterialText(ctx, id); !errors.Is(err, ErrNoText) || !errors.Is(err, material.ErrTextNotExtracted) {
		t.Fatalf("MaterialText(photo) = %v, want ErrNoText", err)
	}
	got, err := e.svc.Inspect(ctx, id)
	if err != nil || got.Text != material.TextNoText || got.Answer != nil {
		t.Fatalf("Inspect(photo) = %+v, %v", got, err)
	}
	if e.texts.calls != 0 || len(e.reader.requests()) != 0 {
		t.Fatalf("extractor calls %d, reader requests %d", e.texts.calls, len(e.reader.requests()))
	}
	if m, _ := e.svc.Material(ctx, id); m.TextStatus() != material.TextNoText {
		t.Fatalf("text status after Inspect = %s", m.TextStatus())
	}
}

func TestPageImage(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	photo, err := e.svc.Import(ctx, Upload{Name: "page.jpg", Data: photoJPEG})
	if err != nil {
		t.Fatal(err)
	}
	m, data, err := e.svc.PageImage(ctx, photo.Material.ID())
	if err != nil || m.ID() != photo.Material.ID() || !bytes.Equal(data, append([]byte(cleanPrefix), photoJPEG...)) {
		t.Fatalf("PageImage = %v, %q, %v", m, data, err)
	}

	doc := importText(t, e, "лекция")
	gone, err := e.svc.Import(ctx, Upload{Name: "gone.png", Data: photoPNG})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.RemoveMaterial(ctx, gone.Material.ID()); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]shared.MaterialID{
		"not a photo": doc.ID(), "removed": gone.Material.ID(), "missing": "m999",
	} {
		if _, _, err := e.svc.PageImage(ctx, id); !errors.Is(err, ErrMaterialNotFound) {
			t.Errorf("%s: PageImage = %v, want ErrMaterialNotFound", name, err)
		}
	}
	if _, _, err := e.svc.PageImage(ctx, "../m1"); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("PageImage(bad id) = %v", err)
	}

	// Bytes that changed or are gone are damaged, never served.
	ref, _ := material.BlobRef(photo.Material.Digest())
	e.blobs.files[ref.String()] = []byte("other bytes")
	if _, _, err := e.svc.PageImage(ctx, photo.Material.ID()); !errors.Is(err, ErrFileDamaged) {
		t.Fatalf("PageImage(changed) = %v", err)
	}
	delete(e.blobs.files, ref.String())
	if _, _, err := e.svc.PageImage(ctx, photo.Material.ID()); !errors.Is(err, ErrFileDamaged) {
		t.Fatalf("PageImage(gone) = %v", err)
	}
}
