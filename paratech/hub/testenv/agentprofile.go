package testenv

import (
	"bytes"
	"fmt"

	"paratech/hub/internal/domain/llm/services/seal"
	"paratech/hub/internal/domain/llm/services/toolcatalog"
	"paratech/hub/pkg/prompts"
)

// PanelV1 returns the embedded server prompt and tool catalog of the panel profile, version 1, as app loads them.
func PanelV1() (system string, catalog *toolcatalog.Catalog, err error) {
	return Panel(1)
}

// Panel returns the embedded server prompt and tool catalog of the panel profile of the version, as app loads them.
func Panel(version uint32) (system string, catalog *toolcatalog.Catalog, err error) {
	versions, err := prompts.Agent("panel")
	if err != nil {
		return "", nil, err
	}
	for _, v := range versions {
		if v.Version != version {
			continue
		}
		c, err := toolcatalog.Parse(v.Tools)
		if err != nil {
			return "", nil, err
		}
		return string(v.System), c, nil
	}
	return "", nil, fmt.Errorf("testenv: no panel v%d", version)
}

// SealKey is an obviously fake seal key of the given version (never a real secret).
func SealKey(version byte) seal.Key {
	return seal.Key{Version: version, Secret: bytes.Repeat([]byte{'t', 'e', 's', 't', version}, 8)}
}

// Sealer is a sealer with SealKey(current) as the current key and the older versions also accepted.
func Sealer(current byte, older ...byte) (*seal.Sealer, error) {
	var keys []seal.Key
	for _, v := range older {
		keys = append(keys, SealKey(v))
	}
	return seal.New(SealKey(current), keys...)
}
