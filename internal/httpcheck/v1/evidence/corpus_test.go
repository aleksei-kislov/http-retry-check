package evidence

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

var evidenceCorpusRoot = filepath.Join("..", "..", "..", "..", "conformance", "http-retry-check", "v1")

type evidenceCorpusInvalidBundle struct {
	SchemaVersion string                      `json:"schema_version"`
	Category      string                      `json:"category"`
	Cases         []evidenceCorpusInvalidCase `json:"cases"`
}

type evidenceCorpusInvalidCase struct {
	ID               string   `json:"id"`
	Expected         string   `json:"expected"`
	Representability []string `json:"representability"`
	BaseCase         string   `json:"base_case,omitempty"`
	Operation        string   `json:"operation"`
	Target           string   `json:"target,omitempty"`
	Operand          string   `json:"operand,omitempty"`
	Count            uint64   `json:"count,omitempty"`
}

func TestCorpusProjectionsMatchStaticEvidence(t *testing.T) {
	for _, name := range []string{"positive", "unsafe", "inconclusive", "mixed"} {
		t.Run(name, func(t *testing.T) {
			reportBytes := evidenceReadCorpus(t, filepath.Join("projections", name, ReportName))
			report, err := DecodeReport(reportBytes)
			if err != nil {
				t.Fatal(err)
			}
			files, err := BuildArtifact(report)
			if err != nil || ValidateArtifact(files) != nil {
				t.Fatalf("build/validate corpus artifact = %v", err)
			}
			for index, specification := range artifactSpecs {
				want := evidenceReadCorpus(t, filepath.Join("projections", name, specification.name))
				if files[index].Name != specification.name || files[index].MediaType != specification.mediaType ||
					!bytes.Equal(files[index].Contents, want) {
					t.Fatalf("artifact projection %s differs from frozen corpus", specification.name)
				}
			}
		})
	}
}

func TestInvalidArtifactCorpusVectorsAreRejected(t *testing.T) {
	var bundle evidenceCorpusInvalidBundle
	if err := json.Unmarshal(evidenceReadCorpus(t, "invalid/artifact.json"), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.SchemaVersion != "http_retry_check.conformance_invalid.v1" || bundle.Category != "artifact" {
		t.Fatalf("artifact invalid bundle identity drift: %#v", bundle)
	}
	applied := 0
	for _, candidate := range bundle.Cases {
		candidate := candidate
		if !evidenceCorpusRepresents(candidate, "go") {
			continue
		}
		applied++
		t.Run(candidate.ID, func(t *testing.T) {
			evidenceApplyArtifactVector(t, candidate)
		})
	}
	if applied != 19 {
		t.Fatalf("Go-representable artifact vectors = %d, want 19", applied)
	}
}

func evidenceApplyArtifactVector(t *testing.T, candidate evidenceCorpusInvalidCase) {
	t.Helper()
	base := evidenceCorpusArtifact(t, "positive")
	if candidate.ID == "artifact_payload_detachment" {
		before := cloneArtifact(base)
		base[1].Contents[0] ^= 1
		if !bytes.Equal(base[0].Contents, before[0].Contents) || !bytes.Equal(base[2].Contents, before[2].Contents) ||
			!bytes.Equal(base[3].Contents, before[3].Contents) {
			t.Fatal("artifact payloads alias one another")
		}
		return
	}
	if candidate.ID == "artifact_build_invalid_report" {
		report, err := DecodeReport(base[1].Contents)
		if err != nil {
			t.Fatal(err)
		}
		report.SchemaVersion = "http_retry_check.report.v2"
		if _, err := BuildArtifact(report); err == nil {
			t.Fatal("artifact built from invalid report")
		}
		return
	}
	if candidate.ID == "artifact_validate_nil" {
		if ValidateArtifact(nil) == nil {
			t.Fatal("nil artifact was accepted")
		}
		return
	}
	if candidate.ID == "artifact_mutation_after_validation" {
		if ValidateArtifact(base) != nil {
			t.Fatal("baseline artifact is invalid")
		}
		base[evidenceArtifactIndex(t, base, candidate.Target)].Contents[0] ^= 1
		if ValidateArtifact(base) == nil {
			t.Fatal("post-validation artifact mutation was accepted")
		}
		return
	}

	switch candidate.Operation {
	case "remove_file":
		index := evidenceArtifactIndex(t, base, candidate.Target)
		base = append(base[:index], base[index+1:]...)
	case "add_file":
		base = append(base, ArtifactFile{Name: candidate.Target, MediaType: "text/plain", Contents: []byte("marker")})
	case "swap_files":
		left := evidenceArtifactIndex(t, base, candidate.Target)
		right := evidenceArtifactIndex(t, base, candidate.Operand)
		base[left], base[right] = base[right], base[left]
	case "alias_contents":
		left := evidenceArtifactIndex(t, base, candidate.Target)
		right := evidenceArtifactIndex(t, base, candidate.Operand)
		base[left].Contents = base[right].Contents
	case "duplicate_file":
		index := evidenceArtifactIndex(t, base, candidate.Target)
		base = append(base, base[index])
	case "flip_byte":
		index := evidenceArtifactIndex(t, base, candidate.Target)
		offset, err := strconv.Atoi(candidate.Operand)
		if err != nil || offset < 0 || offset >= len(base[index].Contents) {
			t.Fatalf("invalid artifact byte offset %q", candidate.Operand)
		}
		base[index].Contents[offset] ^= 1
	case "insert_whitespace":
		index := evidenceArtifactIndex(t, base, candidate.Target)
		colon := bytes.IndexByte(base[index].Contents, ':')
		if colon < 0 {
			t.Fatal("manifest colon not found")
		}
		base[index].Contents = append(append(append([]byte{}, base[index].Contents[:colon+1]...), candidate.Operand...), base[index].Contents[colon+1:]...)
	case "swap_members":
		base[0].Contents = evidenceSwapFirstTwoObjectLines(t, base[0].Contents)
	case "add_member":
		base[0].Contents = append([]byte("{\n  \"marker_unknown\": "+candidate.Operand+",\n"), base[0].Contents[2:]...)
	case "set":
		base[0].Contents = bytes.Replace(
			base[0].Contents,
			[]byte("\"schema_version\": \""+ArtifactManifestSchema+"\""),
			[]byte("\"schema_version\": "+candidate.Operand),
			1,
		)
	case "replace_file_from_case":
		index := evidenceArtifactIndex(t, base, candidate.Target)
		base[index].Contents = evidenceReadCorpus(t, filepath.Join("projections", candidate.Operand, candidate.Target))
	case "resize_file":
		index := evidenceArtifactIndex(t, base, candidate.Target)
		base[index].Contents = bytes.Repeat([]byte(candidate.Operand), int(candidate.Count))
	case "resize_set":
		remaining := int(candidate.Count)
		for index := range base {
			size := remaining
			minimum := remaining - MaxProjectionBytes*(len(base)-index-1)
			if size > MaxProjectionBytes && minimum <= MaxProjectionBytes {
				size = MaxProjectionBytes
			} else if minimum > MaxProjectionBytes {
				size = minimum
			}
			base[index].Contents = bytes.Repeat([]byte(candidate.Operand), size)
			remaining -= size
		}
		if remaining != 0 {
			t.Fatalf("aggregate resize left %d bytes", remaining)
		}
	case "mutate_file_name":
		base[evidenceArtifactIndex(t, base, candidate.Target)].Name = candidate.Operand
	default:
		t.Fatalf("unsupported Go artifact vector operation %q", candidate.Operation)
	}
	if ValidateArtifact(base) == nil {
		t.Fatal("invalid frozen-corpus artifact vector was accepted")
	}
}

func evidenceCorpusArtifact(t *testing.T, name string) []ArtifactFile {
	t.Helper()
	files := make([]ArtifactFile, len(artifactSpecs))
	for index, specification := range artifactSpecs {
		files[index] = ArtifactFile{
			Name: specification.name, MediaType: specification.mediaType,
			Contents: evidenceReadCorpus(t, filepath.Join("projections", name, specification.name)),
		}
	}
	if ValidateArtifact(files) != nil {
		t.Fatalf("frozen %s artifact is invalid", name)
	}
	return files
}

func evidenceArtifactIndex(t *testing.T, files []ArtifactFile, name string) int {
	t.Helper()
	for index := range files {
		if files[index].Name == name {
			return index
		}
	}
	t.Fatalf("artifact file %q not found", name)
	return -1
}

func evidenceSwapFirstTwoObjectLines(t *testing.T, source []byte) []byte {
	t.Helper()
	firstStart := bytes.IndexByte(source, '\n') + 1
	firstEndRelative := bytes.IndexByte(source[firstStart:], '\n')
	if firstStart == 0 || firstEndRelative < 0 {
		t.Fatal("first manifest member line not found")
	}
	firstEnd := firstStart + firstEndRelative + 1
	secondEndRelative := bytes.IndexByte(source[firstEnd:], '\n')
	if secondEndRelative < 0 {
		t.Fatal("second manifest member line not found")
	}
	secondEnd := firstEnd + secondEndRelative + 1
	result := append([]byte{}, source[:firstStart]...)
	result = append(result, source[firstEnd:secondEnd]...)
	result = append(result, source[firstStart:firstEnd]...)
	return append(result, source[secondEnd:]...)
}

func evidenceReadCorpus(t *testing.T, relative string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(evidenceCorpusRoot, filepath.FromSlash(relative)))
	if err != nil || len(contents) == 0 {
		t.Fatalf("read frozen corpus %s: %v", relative, err)
	}
	return contents
}

func evidenceCorpusRepresents(candidate evidenceCorpusInvalidCase, binding string) bool {
	for _, representability := range candidate.Representability {
		if representability == binding {
			return true
		}
	}
	return false
}
