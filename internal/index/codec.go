// Copyright (c) 2026 OpenWALDO Project contributors
// Copyright (c) 2026 CtrlIQ, Inc.
// Copyright (c) 2026 Gregory M. Kurtzer
// SPDX-License-Identifier: Apache-2.0

package index

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	YAMLExtension = ".yaml"
	JSONExtension = ".json"
)

// MarshalYAML emits the JSON-tagged durable representation as readable YAML.
// Going through the type's JSON encoder preserves custom schema behavior such
// as Manifest's polymorphic shards field and keeps one authoritative field map.
func MarshalYAML(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 {
		return nil, fmt.Errorf("encode index YAML: expected one document")
	}
	normalizeYAMLStyle(document.Content[0])
	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(document.Content[0]); err != nil {
		return nil, err
	}
	if err := encoder.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func normalizeYAMLStyle(node *yaml.Node) {
	node.Style = 0
	for _, child := range node.Content {
		normalizeYAMLStyle(child)
	}
}

func decodeMetadata(path string, data []byte, target any) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case JSONExtension:
		return json.Unmarshal(data, target)
	case YAMLExtension, ".yml":
		compatible, err := decodeYAMLDocument(data)
		if err != nil {
			return err
		}
		encoded, err := json.Marshal(compatible)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, target)
	default:
		return fmt.Errorf("unsupported index metadata extension %q; use .yaml, .yml, or .json", filepath.Ext(path))
	}
}

func decodeYAMLDocument(data []byte) (any, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 {
		return nil, fmt.Errorf("YAML metadata must contain exactly one non-empty document")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("YAML metadata must contain exactly one document")
		}
		return nil, err
	}
	return yamlJSONValue(document.Content[0])
}

func yamlJSONValue(node *yaml.Node) (any, error) {
	switch node.Kind {
	case yaml.MappingNode:
		result := make(map[string]any, len(node.Content)/2)
		for position := 0; position < len(node.Content); position += 2 {
			key := node.Content[position]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" || key.Value == "" {
				return nil, fmt.Errorf("YAML metadata keys must be non-empty strings at line %d", key.Line)
			}
			if _, exists := result[key.Value]; exists {
				return nil, fmt.Errorf("duplicate YAML metadata key %q at line %d", key.Value, key.Line)
			}
			value, err := yamlJSONValue(node.Content[position+1])
			if err != nil {
				return nil, err
			}
			result[key.Value] = value
		}
		return result, nil
	case yaml.SequenceNode:
		result := make([]any, 0, len(node.Content))
		for _, child := range node.Content {
			value, err := yamlJSONValue(child)
			if err != nil {
				return nil, err
			}
			result = append(result, value)
		}
		return result, nil
	case yaml.ScalarNode:
		switch node.Tag {
		case "!!str", "!!timestamp":
			return node.Value, nil
		case "!!null":
			return nil, nil
		case "!!bool":
			var value bool
			if err := node.Decode(&value); err != nil {
				return nil, err
			}
			return value, nil
		case "!!int":
			var signed int64
			if err := node.Decode(&signed); err == nil {
				return signed, nil
			}
			var unsigned uint64
			if err := node.Decode(&unsigned); err != nil {
				return nil, err
			}
			return unsigned, nil
		case "!!float":
			var value float64
			if err := node.Decode(&value); err != nil {
				return nil, err
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return nil, fmt.Errorf("non-finite YAML number at line %d is not valid index metadata", node.Line)
			}
			return value, nil
		default:
			return nil, fmt.Errorf("unsupported YAML tag %q at line %d; index metadata must be JSON-compatible YAML", node.Tag, node.Line)
		}
	case yaml.AliasNode:
		return nil, fmt.Errorf("YAML aliases are not supported in deterministic index metadata at line %d", node.Line)
	default:
		return nil, fmt.Errorf("unsupported YAML node at line %d", node.Line)
	}
}
