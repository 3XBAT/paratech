package values

import (
	"fmt"
	"strings"
)

// ModelFamily is the whisper model size a catalog entry belongs to, whatever its language and weight format
// (PRD section 8.1). Empty means no family.
type ModelFamily string

// Model families, from the smallest to the largest.
const (
	FamilyNone         ModelFamily = ""
	FamilyTiny         ModelFamily = "tiny"
	FamilyBase         ModelFamily = "base"
	FamilySmall        ModelFamily = "small"
	FamilyMedium       ModelFamily = "medium"
	FamilyLargeV3Turbo ModelFamily = "large-v3-turbo"
	FamilyLargeV3      ModelFamily = "large-v3"
)

// Validate reports whether the family is empty or one of the known families, in lowercase.
func (f ModelFamily) Validate() error {
	if _, ok := familyLoad[f]; ok || f == FamilyNone {
		return nil
	}
	return fmt.Errorf("%w: %q", ErrInvalidModelFamily, string(f))
}

// String returns the family.
func (f ModelFamily) String() string { return string(f) }

// ModelLoad is how hard a model works the computer on a scale of MinModelLoad..MaxModelLoad; LoadUnknown when the
// family is unknown. The rule and its reasons are in docs/refs/whisper.md.
type ModelLoad int

// Bounds of the load scale.
const (
	LoadUnknown  ModelLoad = 0
	MinModelLoad ModelLoad = 1
	MaxModelLoad ModelLoad = 5

	// HeavyModelLoad is the lowest load that gets the "demanding model" warning.
	HeavyModelLoad ModelLoad = 4
)

// familyLoad is the load of the full (f16) variant. large-v3-turbo has fewer decoder layers but the full large-v3
// encoder, which takes most of the CPU time, so it is as heavy as large-v3.
var familyLoad = map[ModelFamily]ModelLoad{
	FamilyTiny: 1, FamilyBase: 2, FamilySmall: 3, FamilyMedium: 4, FamilyLargeV3Turbo: 5, FamilyLargeV3: 5,
}

// LoadOf returns the load of a model of the family in the weight format: the family level for a full variant
// ("f16" or unknown format), one step lower, but at least MinModelLoad, for a quantized one ("q5_1", "q8_0" and so
// on); LoadUnknown for no or an unknown family.
func LoadOf(family ModelFamily, quantization Quantization) ModelLoad {
	load, ok := familyLoad[family]
	if !ok {
		return LoadUnknown
	}
	if strings.HasPrefix(string(quantization), "q") {
		load = max(load-1, MinModelLoad)
	}
	return load
}

// Heavy reports whether the load is high enough for the warning that recognition will be slow on a usual CPU.
func (l ModelLoad) Heavy() bool { return l >= HeavyModelLoad }
