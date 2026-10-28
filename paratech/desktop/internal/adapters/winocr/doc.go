// Package winocr recognises printed text on a photo page with Windows OCR (Windows.Media.Ocr), locally and without
// CGO: WinRT is reached through raw vtable calls with golang.org/x/sys/windows (ADR-0027 «Recognition of photos and
// scans»). The engine is created for Russian only; there is never a fallback to another language. IIDs, vtable slots
// and their sources are listed in docs/refs/windows-ocr.md. Off Windows every call returns ErrUnsupported.
package winocr
