package app

import (
	"context"
	"strings"
	"time"

	"paratech/desktop/internal/adapters/winocr"
)

// ocrLanguageTimeout bounds the language check of the settings page: it comes with every reply of the page.
const ocrLanguageTimeout = 5 * time.Second

// ocrLanguages is the preferences.RecognitionLanguages port over Windows OCR (TASK-293, TASK-294). It has a reader of
// its own, so the check never waits behind the recognition of a page. Off Windows the reader returns
// winocr.ErrUnsupported, which the use case reads as an unknown language.
type ocrLanguages struct{ r *winocr.Reader }

func newOCRLanguages() ocrLanguages {
	return ocrLanguages{r: winocr.New(winocr.WithPageTimeout(ocrLanguageTimeout))}
}

// RussianAvailable reports whether Windows has the recognizer of winocr.Language, as "ru" or a regional "ru-RU".
func (o ocrLanguages) RussianAvailable(ctx context.Context) (bool, error) {
	tags, err := o.r.AvailableLanguages(ctx)
	if err != nil {
		return false, err
	}
	return hasRussian(tags), nil
}

func hasRussian(tags []string) bool {
	for _, t := range tags {
		t = strings.ToLower(t)
		if t == winocr.Language || strings.HasPrefix(t, winocr.Language+"-") {
			return true
		}
	}
	return false
}
