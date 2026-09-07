package importer

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed default-mapping.json
var defaultMappingJSON []byte

func DefaultMapping() (Mapping, error) {
	var mapping Mapping
	if err := json.Unmarshal(defaultMappingJSON, &mapping); err != nil {
		return Mapping{}, fmt.Errorf("decode embedded mapping: %w", err)
	}
	if len(mapping.Sheets) == 0 {
		return Mapping{}, fmt.Errorf("embedded mapping has no sheets")
	}
	return mapping, nil
}
