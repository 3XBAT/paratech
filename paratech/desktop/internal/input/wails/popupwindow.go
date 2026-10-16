package wails

import "log/slog"

// PopupFitter resizes the popup window for its content; the shell implements it.
type PopupFitter interface {
	FitPopup(height int) (int, error)
}

// popupFitterFunc adapts a function to PopupFitter.
type popupFitterFunc func(height int) (int, error)

// FitPopup calls f.
func (f popupFitterFunc) FitPopup(height int) (int, error) { return f(height) }

// PopupWindowService is bound to the popup page ("<package path>.PopupWindowService.<Method>"). It only sizes the
// window: the page never moves it or makes it wider, and the shell keeps it inside the work area.
type PopupWindowService struct {
	fitter PopupFitter
	log    *slog.Logger
}

// NewPopupWindowService returns the window binding of the popup page.
func NewPopupWindowService(fitter PopupFitter, log *slog.Logger) *PopupWindowService {
	return &PopupWindowService{fitter: fitter, log: log}
}

// Fit asks for a popup of content height (DIP) and returns the height the window got: the shell clamps it to
// [popupHeight, popupMaxHeight] and the work area, so the page scrolls its texts when it got less than it asked.
func (s *PopupWindowService) Fit(height int) (int, error) {
	got, err := s.fitter.FitPopup(height)
	if err != nil {
		s.log.Warn("ui: fit popup", "height", height, "err", err)
		return 0, ErrActionFailed
	}
	return got, nil
}
