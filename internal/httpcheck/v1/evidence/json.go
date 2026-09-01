package evidence

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

func newStrictJSONDecoder(encoded []byte) *json.Decoder {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	return decoder
}

func decoderAtEOF(decoder *json.Decoder) bool {
	var trailing any
	return errors.Is(decoder.Decode(&trailing), io.EOF)
}

// EncodeReport returns the canonical v1 JSON representation.
func EncodeReport(report Report) ([]byte, error) {
	if ValidateReport(report) != nil {
		return nil, invalidReport
	}
	encoded, ok := marshalCanonicalJSON(report)
	if !ok {
		return nil, invalidReport
	}
	return encoded, nil
}

// DecodeReport reads and validates a canonical v1 report within the size limit.
func DecodeReport(encoded []byte) (Report, error) {
	if len(encoded) == 0 || len(encoded) > MaxProjectionBytes || !boundedJSONShape(encoded) {
		return Report{}, invalidReport
	}
	decoder := newStrictJSONDecoder(encoded)
	var report Report
	if decoder.Decode(&report) != nil || !decoderAtEOF(decoder) || ValidateReport(report) != nil {
		return Report{}, invalidReport
	}
	canonical, ok := marshalCanonicalJSON(report)
	if !ok || !bytes.Equal(encoded, canonical) {
		return Report{}, invalidReport
	}
	return cloneReport(report), nil
}

func marshalCanonicalJSON(value any) ([]byte, bool) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, false
	}
	encoded = append(encoded, '\n')
	return encoded, len(encoded) <= MaxProjectionBytes
}

func boundedJSONShape(encoded []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if !consumeJSONValue(decoder, 0) {
		return false
	}
	_, err := decoder.Token()
	return errors.Is(err, io.EOF)
}

func consumeJSONValue(decoder *json.Decoder, parentDepth int) bool {
	token, err := decoder.Token()
	if err != nil || token == nil {
		return false
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		switch token.(type) {
		case string, bool, json.Number:
			return true
		default:
			return false
		}
	}
	if parentDepth+1 > maxJSONDepth {
		return false
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			if len(seen) == maxJSONObjectKeys {
				return false
			}
			keyToken, keyErr := decoder.Token()
			key, isString := keyToken.(string)
			if keyErr != nil || !isString {
				return false
			}
			if _, duplicate := seen[key]; duplicate {
				return false
			}
			seen[key] = struct{}{}
			if !consumeJSONValue(decoder, parentDepth+1) {
				return false
			}
		}
		closing, closeErr := decoder.Token()
		return closeErr == nil && closing == json.Delim('}')
	case '[':
		count := 0
		for decoder.More() {
			if count == maxJSONArrayItems || !consumeJSONValue(decoder, parentDepth+1) {
				return false
			}
			count++
		}
		closing, closeErr := decoder.Token()
		return closeErr == nil && closing == json.Delim(']')
	default:
		return false
	}
}

func cloneReport(report Report) Report {
	cloned := report
	cloned.Scenarios = make([]Scenario, len(report.Scenarios))
	copy(cloned.Scenarios, report.Scenarios)
	for index := range cloned.Scenarios {
		cloned.Scenarios[index].Findings = append([]Finding{}, report.Scenarios[index].Findings...)
	}
	return cloned
}
