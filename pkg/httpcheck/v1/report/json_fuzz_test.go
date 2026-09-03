package report

import (
	"bytes"
	"reflect"
	"testing"
)

func FuzzDecode(f *testing.F) {
	canonical, err := Encode(mustReportForFuzz())
	if err != nil {
		f.Fatal(err)
	}
	f.Add(canonical)
	f.Add([]byte(`{"schema_version":"duplicate","schema_version":"duplicate"}`))
	f.Add([]byte(`[[[[[[[[[{}]]]]]]]]]`))
	f.Add([]byte("null\n"))
	f.Fuzz(func(t *testing.T, input []byte) {
		value, decodeErr := Decode(input)
		if decodeErr != nil {
			return
		}
		if Validate(value) != nil {
			t.Fatal("Decode returned an invalid report")
		}
		encoded, encodeErr := Encode(value)
		if encodeErr != nil || !bytes.Equal(encoded, input) {
			t.Fatal("Decode admitted noncanonical bytes")
		}
		second, secondErr := Decode(encoded)
		if secondErr != nil || !reflect.DeepEqual(second, value) {
			t.Fatal("accepted report is not a fixed point")
		}
	})
}

func mustReportForFuzz() Report {
	value, _ := newFromSource(positiveResult())
	return value
}
