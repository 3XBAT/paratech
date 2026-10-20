package knowledgebase

import (
	"context"
	"testing"
)

// TASK-110: whisper's marks are not searched; the speech around them is, at its own index.
func TestSearchSkipsMarks(t *testing.T) {
	e, ctx := newEnv(t), context.Background()
	e.add(t, lessonSpec{id: "r1", title: "Оптика", subject: "Физика"})
	e.addTranscript(t, "r1", " [BLANK_AUDIO]", " [MUSIC] Свет и линзы.", "(music)")

	if res, err := e.svc.Search(ctx, "BLANK"); err != nil || len(res.Hits) != 0 {
		t.Fatalf("Search(BLANK) = %+v, %v; want no hit", res.Hits, err)
	}
	if res, err := e.svc.Search(ctx, "music"); err != nil || len(res.Hits) != 0 {
		t.Fatalf("Search(music) = %+v, %v; want no hit", res.Hits, err)
	}
	res, err := e.svc.Search(ctx, "линз")
	if err != nil || len(res.Hits) != 1 || res.Hits[0].Index != 0 || res.Hits[0].StartMs != 1000 {
		t.Fatalf("Search(линз) = %+v, %v", res.Hits, err)
	}
}
