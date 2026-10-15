package values

import "fmt"

// MaxImportSizeBytes bounds a model file imported outside the catalog (PRD section 8, AC-23). The largest whisper.cpp
// model (large, f16) has about 3 GiB; the bound leaves room for other formats and refuses a wrong pick (a video, a
// disk image) before it fills the disk.
const MaxImportSizeBytes int64 = 8 << 30

// CheckImportSize reports whether a file of size bytes may be imported outside the catalog: a valid model size of at
// most MaxImportSizeBytes. A larger file is ErrImportTooLarge.
func CheckImportSize(size int64) error {
	if err := ValidateSize(size); err != nil {
		return err
	}
	if size > MaxImportSizeBytes {
		return fmt.Errorf("%w: %d bytes, at most %d", ErrImportTooLarge, size, MaxImportSizeBytes)
	}
	return nil
}

// CheckImportSpace reports whether free bytes are enough to copy an imported file of size bytes into the data
// directory, DiskSpaceMargin included (PRD section 8). Too little space is an *InsufficientSpaceError.
func CheckImportSpace(size, free int64) error {
	if need := size + DiskSpaceMargin; free < need {
		return &InsufficientSpaceError{Required: need, Available: free}
	}
	return nil
}

// ImportConfirmation is the explicit user consent to install a model file outside the catalog (PRD section 8,
// AC-23). It is bound to the SHA-256 that was shown to the user, so a file that changed after it was shown is not
// installed. The zero value means "not confirmed".
type ImportConfirmation struct{ sum SHA256 }

// ConfirmImport records that the user confirmed the import of the file with the shown digest.
func ConfirmImport(shown SHA256) (ImportConfirmation, error) {
	if err := shown.Validate(); err != nil {
		return ImportConfirmation{}, err
	}
	return ImportConfirmation{sum: shown}, nil
}

// SHA256 returns the confirmed digest, empty when not confirmed.
func (c ImportConfirmation) SHA256() SHA256 { return c.sum }

// IsZero reports whether the import is not confirmed.
func (c ImportConfirmation) IsZero() bool { return c.sum == "" }

// Require refuses a missing confirmation with ErrConfirmationRequired: a cheap check before a file is copied.
func (c ImportConfirmation) Require() error {
	if c.IsZero() {
		return fmt.Errorf("%w: the user did not confirm the model file", ErrConfirmationRequired)
	}
	return nil
}

// Verify reports whether the file with digest actual is the one the user confirmed (PRD section 14 case 13). A
// missing confirmation is ErrConfirmationRequired, another digest ErrConfirmationMismatch.
func (c ImportConfirmation) Verify(actual SHA256) error {
	if err := c.Require(); err != nil {
		return err
	}
	if actual != c.sum {
		return fmt.Errorf("%w: confirmed %s, file has %s", ErrConfirmationMismatch, c.sum, actual)
	}
	return nil
}
