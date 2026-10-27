package wails

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"paratech/desktop/internal/cases/notes"
	"paratech/desktop/internal/cases/transcriptview"
	"paratech/desktop/internal/domain/library/material"
	"paratech/desktop/internal/domain/shared"
)

const (
	opMedia = "ui: play recording"

	// maxMediaChunk bounds one response. The Windows asset server of Wails (v3.0.0-beta.29, webview
	// responsewriter_windows.go) buffers the whole body in memory before WebView2 gets it, and a recording is up to
	// about 2 GB, so every response is a range of at most this size; the audio element asks for the next one.
	maxMediaChunk = 4 << 20

	mediaContentType = "audio/wav"

	opNotePage = "ui: show a page of notes"

	// mediaNotePagePath is the path below which the asset server shows a photo page of a note: mediaNotePagePath +
	// <material id>.
	mediaNotePagePath = "/media/notepage/"

	// notePageCSP keeps a page image from ever running as a document, should one be opened directly.
	notePageCSP = "default-src 'none'; sandbox"
)

// AudioSource opens the final track of a recording; satisfied by transcriptview.Service.
type AudioSource interface {
	OpenAudio(ctx context.Context, id shared.RecordingID) (transcriptview.Audio, error)
}

// PageSource reads a photo page of a note by its material id; satisfied by notes.Service.
type PageSource interface {
	PageImage(ctx context.Context, id shared.MaterialID) (notes.Page, error)
}

// notePageURL is the address a photo page of a note is shown from.
func notePageURL(id shared.MaterialID) string { return mediaNotePagePath + url.PathEscape(string(id)) }

// pageContentType is the content type of a photo page, by the type stored for its material (told from its content
// when it was imported and encoded again), never by a name or by sniffing; false for any other type.
func pageContentType(t material.Type) (string, bool) {
	switch t {
	case material.TypeJPEG:
		return "image/jpeg", true
	case material.TypePNG:
		return "image/png", true
	}
	return "", false
}

// mediaHandler serves GET and HEAD of mediaRecordingPath<id> from the recording's final track, with byte ranges, and
// of mediaNotePagePath<id> from a photo page of a note, and passes every other path to the asset handler. The id is
// the only input: no path ever comes from the page. Until the core is open (setSource, setPages) nothing is found.
type mediaHandler struct {
	next  http.Handler
	src   atomic.Pointer[AudioSource]
	pages atomic.Pointer[PageSource]
	log   *slog.Logger
}

func newMediaHandler(next http.Handler, log *slog.Logger) *mediaHandler {
	return &mediaHandler{next: next, log: log}
}

// setSource makes the recordings playable.
func (h *mediaHandler) setSource(src AudioSource) { h.src.Store(&src) }

// setPages makes the photo pages of notes visible.
func (h *mediaHandler) setPages(src PageSource) { h.pages.Store(&src) }

// mediaHeaders sets the headers of every media response and refuses methods other than GET and HEAD.
func mediaHeaders(w http.ResponseWriter, r *http.Request) bool {
	hdr := w.Header()
	hdr.Set("Cache-Control", "no-store")
	hdr.Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		hdr.Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func (h *mediaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if rest, ok := strings.CutPrefix(r.URL.Path, mediaNotePagePath); ok {
		h.servePage(w, r, rest)
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, mediaRecordingPath)
	if !ok {
		h.next.ServeHTTP(w, r)
		return
	}

	hdr := w.Header()
	if !mediaHeaders(w, r) {
		return
	}

	// r.URL.Path is decoded; a valid id is one file-name segment (no separators, dots-only or control characters).
	id := shared.RecordingID(rest)
	src := h.src.Load()
	if id.Validate() != nil || src == nil {
		http.NotFound(w, r)
		return
	}

	audio, err := (*src).OpenAudio(r.Context(), id)
	switch {
	case errors.Is(err, transcriptview.ErrRecordingNotFound), errors.Is(err, transcriptview.ErrNoAudio),
		errors.Is(err, transcriptview.ErrInvalidRequest):
		http.NotFound(w, r)
		return
	case err != nil:
		h.log.Error(opMedia, "err", err)
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	defer func() { _ = audio.Reader.Close() }()

	rng, ok := clampRange(r.Header.Get("Range"), audio.Size)
	if !ok {
		hdr.Set("Content-Range", fmt.Sprintf("bytes */%d", audio.Size))
		http.Error(w, "range not satisfiable", http.StatusRequestedRangeNotSatisfiable)
		return
	}

	// No validators: a 304 hangs WebView2 (Wails turns it into a 500), and the file of a recording does not change.
	req := r.Clone(r.Context())
	for _, k := range []string{"If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since", "If-Range"} {
		req.Header.Del(k)
	}
	if rng != "" {
		req.Header.Set("Range", rng)
	}
	hdr.Set("Content-Type", mediaContentType)
	http.ServeContent(w, req, "", time.Time{}, audio.Reader)
}

// servePage serves a photo page of a note whole (at most material.MaxImageBytes), with the content type of its stored
// type. Anything that is not an active photo is not found.
func (h *mediaHandler) servePage(w http.ResponseWriter, r *http.Request, rest string) {
	if !mediaHeaders(w, r) {
		return
	}
	// r.URL.Path is decoded; a valid id is one file-name segment (no separators, dots-only or control characters).
	id := shared.MaterialID(rest)
	src := h.pages.Load()
	if id.Validate() != nil || src == nil {
		http.NotFound(w, r)
		return
	}
	page, err := (*src).PageImage(r.Context(), id)
	switch {
	case errors.Is(err, notes.ErrMaterialNotFound), errors.Is(err, notes.ErrInvalidRequest):
		h.log.Info(opNotePage, "result", "not found")
		http.NotFound(w, r)
		return
	case err != nil:
		h.log.Error(opNotePage, "err", err)
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	ctype, ok := pageContentType(page.Material.Type())
	if !ok || page.Material.ID() != id {
		http.NotFound(w, r)
		return
	}

	hdr := w.Header()
	hdr.Set("Content-Type", ctype)
	hdr.Set("Content-Length", strconv.Itoa(len(page.Data)))
	hdr.Set("Content-Security-Policy", notePageCSP)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(page.Data); err != nil && r.Context().Err() == nil {
		h.log.Warn(opNotePage, "err", err)
	}
}

// clampRange turns the Range header of the request into one range of at most maxMediaChunk bytes: the first range of
// the header (an audio element asks for one), cut to the chunk. Without a header a file up to the chunk is served
// whole (""), a larger one from its first chunk. ok is false for a range that cannot be satisfied or parsed.
func clampRange(header string, size int64) (string, bool) {
	header = strings.TrimSpace(header)
	if header == "" {
		if size <= maxMediaChunk {
			return "", true
		}
		return fmt.Sprintf("bytes=0-%d", maxMediaChunk-1), true
	}

	spec, ok := strings.CutPrefix(header, "bytes=")
	if !ok {
		return "", false
	}
	spec, _, _ = strings.Cut(spec, ",")
	first, last, ok := strings.Cut(strings.TrimSpace(spec), "-")
	if !ok {
		return "", false
	}
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)

	var start, end int64
	if first == "" { // the last n bytes
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 {
			return "", false
		}
		start, end = max(size-n, 0), size-1
	} else {
		s, err := strconv.ParseInt(first, 10, 64)
		if err != nil || s < 0 {
			return "", false
		}
		start, end = s, size-1
		if last != "" {
			e, err := strconv.ParseInt(last, 10, 64)
			if err != nil || e < s {
				return "", false
			}
			end = min(e, size-1)
		}
	}
	if start >= size {
		return "", false
	}
	end = min(end, start+maxMediaChunk-1)
	return fmt.Sprintf("bytes=%d-%d", start, end), true
}
