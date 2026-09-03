package report

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

const (
	maxJSONDepth      = 8
	maxJSONObjectKeys = 32
	maxJSONArrayItems = 32
)

// Encode validates and returns canonical two-space-indented JSON plus one LF.
func Encode(value Report) ([]byte, error) {
	if Validate(value) != nil {
		return nil, invalidReport
	}
	encoded, ok := marshalCanonicalJSON(value)
	if !ok {
		return nil, invalidReport
	}
	return encoded, nil
}

// Decode reads and validates a canonical v2 report within the size limit.
func Decode(encoded []byte) (Report, error) {
	if len(encoded) == 0 || len(encoded) > MaxProjectionBytes {
		return Report{}, invalidReport
	}
	var value Report
	if !decodeCanonicalJSON(encoded, &value) || Validate(value) != nil {
		return Report{}, invalidReport
	}
	return cloneReport(value), nil
}

func marshalCanonicalJSON(value any) ([]byte, bool) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, false
	}
	encoded = append(encoded, '\n')
	if len(encoded) > MaxProjectionBytes {
		return nil, false
	}
	return encoded, true
}

func decodeCanonicalJSON(encoded []byte, destination any) bool {
	if len(encoded) == 0 || len(encoded) > MaxProjectionBytes {
		return false
	}
	if destination == nil || !validateJSONStructure(encoded) {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(destination) != nil {
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false
	}
	canonical, ok := marshalCanonicalJSON(destination)
	return ok && bytes.Equal(encoded, canonical)
}

func validateJSONStructure(encoded []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if !walkJSONValue(decoder, 0) {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

func walkJSONValue(decoder *json.Decoder, parentDepth int) bool {
	token, err := decoder.Token()
	if err != nil || token == nil {
		return false
	}
	switch value := token.(type) {
	case json.Delim:
		if parentDepth+1 > maxJSONDepth {
			return false
		}
		switch value {
		case '{':
			return walkJSONObject(decoder, parentDepth+1)
		case '[':
			return walkJSONArray(decoder, parentDepth+1)
		default:
			return false
		}
	case string, bool, json.Number:
		return true
	default:
		return false
	}
}

func walkJSONObject(decoder *json.Decoder, depth int) bool {
	seen := make(map[string]struct{}, maxJSONObjectKeys)
	for decoder.More() {
		if len(seen) == maxJSONObjectKeys {
			return false
		}
		keyToken, err := decoder.Token()
		key, isString := keyToken.(string)
		if err != nil || !isString {
			return false
		}
		if _, duplicate := seen[key]; duplicate {
			return false
		}
		seen[key] = struct{}{}
		if !walkJSONValue(decoder, depth) {
			return false
		}
	}
	closing, err := decoder.Token()
	return err == nil && closing == json.Delim('}')
}

func walkJSONArray(decoder *json.Decoder, depth int) bool {
	count := 0
	for decoder.More() {
		if count == maxJSONArrayItems || !walkJSONValue(decoder, depth) {
			return false
		}
		count++
	}
	closing, err := decoder.Token()
	return err == nil && closing == json.Delim(']')
}

func cloneReport(value Report) Report {
	cloned := value
	cloned.Scenarios = make([]Scenario, len(value.Scenarios))
	copy(cloned.Scenarios, value.Scenarios)
	for index := range cloned.Scenarios {
		cloned.Scenarios[index].Findings = append([]Finding{}, value.Scenarios[index].Findings...)
	}
	return cloned
}
