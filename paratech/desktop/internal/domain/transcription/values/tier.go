package values

import "fmt"

// ModelTier is the speed and accuracy class of a catalog model, for the labels of the models page (PRD section 8.1).
// Empty means no tier.
type ModelTier string

// Model tiers, from the fastest to the most accurate.
const (
	TierNone     ModelTier = ""
	TierFast     ModelTier = "fast"
	TierBalanced ModelTier = "balanced"
	TierAccurate ModelTier = "accurate"
	TierMax      ModelTier = "max"
)

// Validate reports whether the tier is empty or one of the known tiers, in lowercase.
func (t ModelTier) Validate() error {
	switch t {
	case TierNone, TierFast, TierBalanced, TierAccurate, TierMax:
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidModelTier, string(t))
}

// String returns the tier.
func (t ModelTier) String() string { return string(t) }
