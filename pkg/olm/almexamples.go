package olm

import (
	"encoding/json"
	"fmt"

	k8sjson "k8s.io/apimachinery/pkg/util/json"
)

// GetALMExampleItem retrieves the ALM example item at the specified index as a raw JSON,
// which can be later unmarshalled into custom resource objects. This can be useful if
// the original JSON needs to be modified, e.g. patched.
func GetALMExampleItem(index int, almExample string) (json.RawMessage, error) {
	examples, err := GetALMExampleItems(almExample)
	if err != nil {
		return nil, fmt.Errorf("failed to get ALM example items: %w", err)
	}

	if index < 0 || len(examples) < index+1 {
		return nil, fmt.Errorf("no alm examples item exists at index %d", index)
	}

	return examples[index], nil
}

// GetALMExampleItems retrieves a slice of raw JSON entries from an ALM example string.
// The raw entries can be later unmarshalled into custom resource objects. This can be
// useful if the original JSON needs to be modified (e.g. patched), or each entry will
// be unmarshalled into an object of a different type.
func GetALMExampleItems(almExample string) ([]json.RawMessage, error) {
	if almExample == "" {
		return nil, fmt.Errorf("almExample is an empty string")
	}

	var examples []json.RawMessage
	err := k8sjson.Unmarshal([]byte(almExample), &examples)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ALM examples: %w", err)
	}

	return examples, nil
}
