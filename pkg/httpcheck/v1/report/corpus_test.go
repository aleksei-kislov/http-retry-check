package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

type corpusManifest struct {
	SchemaVersion       string                 `json:"schema_version"`
	ProductIdentity     string                 `json:"product_identity"`
	SuiteIdentity       string                 `json:"suite_identity"`
	ReportIdentity      string                 `json:"report_identity"`
	ConformanceIdentity string                 `json:"conformance_identity"`
	Files               []corpusFileDescriptor `json:"files"`
}

type corpusFileDescriptor struct {
	Path   string `json:"path"`
	Size   uint64 `json:"size"`
	SHA256 string `json:"sha256"`
}

type corpusInvalidBundle struct {
	SchemaVersion string              `json:"schema_version"`
	Category      string              `json:"category"`
	Marker        string              `json:"marker"`
	Cases         []corpusInvalidCase `json:"cases"`
}

type corpusInvalidCase struct {
	ID               string   `json:"id"`
	Expected         string   `json:"expected"`
	Representability []string `json:"representability"`
	BaseCase         string   `json:"base_case"`
	Operation        string   `json:"operation"`
	Target           string   `json:"target"`
	Operand          string   `json:"operand"`
	Count            int      `json:"count"`
}

func TestNeutralCorpusRootManifestBindsEveryFixture(t *testing.T) {
	root := corpusRoot(t)
	manifestBytes := readCorpus(t, "manifest.json")
	var manifest corpusManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	canonical, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	canonical = append(canonical, '\n')
	if !bytes.Equal(canonical, manifestBytes) ||
		manifest.SchemaVersion != "http_retry_check.conformance_corpus_manifest.v1" ||
		manifest.ProductIdentity != "http_retry_check.v1" || manifest.SuiteIdentity != SuiteIdentity ||
		manifest.ReportIdentity != SchemaVersion || manifest.ConformanceIdentity != "http_retry_check.conformance.v1" ||
		manifest.Files == nil || len(manifest.Files) == 0 {
		t.Fatal("corpus root manifest identity or canonical bytes drifted")
	}

	actual := make([]corpusFileDescriptor, 0, len(manifest.Files))
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == root || entry.IsDir() {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("non-regular corpus entry")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == "manifest.json" {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		actual = append(actual, corpusFileDescriptor{
			Path: relative, Size: uint64(len(contents)), SHA256: sha256Hex(contents),
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(actual, func(left, right int) bool { return actual[left].Path < actual[right].Path })
	if !reflect.DeepEqual(actual, manifest.Files) {
		t.Fatalf("corpus descriptors do not match inventory\nactual: %#v\nmanifest: %#v", actual, manifest.Files)
	}
	for index, descriptor := range manifest.Files {
		if descriptor.Path == "" || descriptor.Size == 0 || !validSHA256(descriptor.SHA256) ||
			(index != 0 && manifest.Files[index-1].Path >= descriptor.Path) {
			t.Fatalf("descriptor %d invalid: %#v", index, descriptor)
		}
	}
}

func TestCorpusProjectionBytesMatchGoldens(t *testing.T) {
	for _, caseID := range []string{"positive", "unsafe", "inconclusive", "mixed"} {
		t.Run(caseID, func(t *testing.T) {
			reportJSON := readCorpus(t, "projections/"+caseID+"/report.json")
			junitXML := readCorpus(t, "projections/"+caseID+"/junit.xml")
			summaryMarkdown := readCorpus(t, "projections/"+caseID+"/summary.md")
			manifestJSON := readCorpus(t, "projections/"+caseID+"/manifest.json")

			value, err := Decode(reportJSON)
			if err != nil || Validate(value) != nil {
				t.Fatalf("Decode/Validate = %#v/%v", value, err)
			}
			reencoded, encodeErr := Encode(value)
			junit, junitErr := JUnit(value)
			summary, summaryErr := GitHubSummary(value)
			files, artifactErr := BuildArtifact(value)
			if encodeErr != nil || junitErr != nil || summaryErr != nil || artifactErr != nil ||
				!bytes.Equal(reencoded, reportJSON) || !bytes.Equal(junit, junitXML) ||
				!bytes.Equal(summary, summaryMarkdown) {
				t.Fatal("native projections disagree with neutral corpus")
			}
			corpusFiles := []ArtifactFile{
				{Name: manifestName, MediaType: jsonMediaType, Contents: manifestJSON},
				{Name: reportName, MediaType: jsonMediaType, Contents: reportJSON},
				{Name: junitName, MediaType: xmlMediaType, Contents: junitXML},
				{Name: summaryName, MediaType: markdownMediaType, Contents: summaryMarkdown},
			}
			if !equalArtifacts(files, corpusFiles) || ValidateArtifact(corpusFiles) != nil {
				t.Fatal("native artifact disagrees with neutral corpus")
			}
		})
	}
}

func TestNeutralCorpusCanonicalJSONInvalidVectors(t *testing.T) {
	bundle := loadInvalidBundle(t, "canonical-json.json", "canonical_json")
	handled := 0
	for _, vector := range bundle.Cases {
		if !representedByGo(vector) {
			continue
		}
		t.Run(vector.ID, func(t *testing.T) {
			if vector.Expected != "invalid_report" {
				t.Fatalf("unexpected result class %q", vector.Expected)
			}
			base := readCorpus(t, "projections/"+vector.BaseCase+"/report.json")
			candidate := applyCanonicalJSONVector(t, vector, base)
			value, err := Decode(candidate)
			if err != invalidReport || !reflect.DeepEqual(value, Report{}) {
				t.Fatalf("Decode = %#v/%v", value, err)
			}
		})
		handled++
	}
	if handled != len(bundle.Cases) {
		t.Fatalf("handled %d/%d canonical JSON vectors", handled, len(bundle.Cases))
	}
}

func TestNeutralCorpusInvalidReportModelVectors(t *testing.T) {
	bundle := loadInvalidBundle(t, "report-model.json", "report_model")
	handled := 0
	for _, vector := range bundle.Cases {
		if !representedByGo(vector) {
			continue
		}
		t.Run(vector.ID, func(t *testing.T) {
			if vector.Expected != "invalid_report" {
				t.Fatalf("unexpected result class %q", vector.Expected)
			}
			consumeInvalidReportVector(t, vector)
		})
		handled++
	}
	if handled != len(bundle.Cases) {
		t.Fatalf("handled %d/%d report-model vectors", handled, len(bundle.Cases))
	}
}

func TestNeutralCorpusInvalidArtifactVectors(t *testing.T) {
	bundle := loadInvalidBundle(t, "artifact.json", "artifact")
	handled := 0
	for _, vector := range bundle.Cases {
		if !representedByGo(vector) {
			continue
		}
		t.Run(vector.ID, func(t *testing.T) {
			consumeArtifactVector(t, vector)
		})
		handled++
	}
	if handled == 0 {
		t.Fatal("no Go artifact vectors were consumed")
	}
}

func consumeInvalidReportVector(t *testing.T, vector corpusInvalidCase) {
	t.Helper()
	if vector.Operation == "construct_report" {
		result := positiveResult()
		result.Scenarios[0].Findings = append(result.Scenarios[0].Findings, "scenario_incomplete")
		value, err := newFromSource(result)
		if err != invalidReport || !reflect.DeepEqual(value, Report{}) {
			t.Fatalf("New = %#v/%v", value, err)
		}
		return
	}
	base := readCorpus(t, "projections/"+vector.BaseCase+"/report.json")
	value, err := Decode(base)
	if err != nil {
		t.Fatal(err)
	}
	switch vector.Operation {
	case "set":
		setReportModelValue(t, &value, vector.Target, vector.Operand)
	case "null":
		switch vector.Target {
		case "/scenarios":
			value.Scenarios = nil
		case "/scenarios/0/findings":
			value.Scenarios[0].Findings = nil
		default:
			t.Fatalf("unknown null target %q", vector.Target)
		}
	case "swap":
		switch vector.Target {
		case "/scenarios/0":
			value.Scenarios[0], value.Scenarios[1] = value.Scenarios[1], value.Scenarios[0]
		case "/scenarios/5/findings/0":
			value.Scenarios[5].Findings[0], value.Scenarios[5].Findings[1] =
				value.Scenarios[5].Findings[1], value.Scenarios[5].Findings[0]
		default:
			t.Fatalf("unknown swap target %q", vector.Target)
		}
	case "remove":
		candidate := removeJSONMember(t, base, "observation")
		if decoded, decodeErr := Decode(candidate); decodeErr != invalidReport || !reflect.DeepEqual(decoded, Report{}) {
			t.Fatalf("Decode removed member = %#v/%v", decoded, decodeErr)
		}
		return
	case "duplicate_member":
		candidate := duplicateJSONMemberOccurrence(t, base, "assessment", 2)
		if decoded, decodeErr := Decode(candidate); decodeErr != invalidReport || !reflect.DeepEqual(decoded, Report{}) {
			t.Fatalf("Decode duplicate member = %#v/%v", decoded, decodeErr)
		}
		return
	default:
		t.Fatalf("unknown report operation %q", vector.Operation)
	}
	if err := Validate(value); err != invalidReport {
		t.Fatalf("Validate = %v", err)
	}
	if encoded, err := Encode(value); encoded != nil || err != invalidReport {
		t.Fatalf("Encode = %q/%v", encoded, err)
	}
}

func consumeArtifactVector(t *testing.T, vector corpusInvalidCase) {
	t.Helper()
	if vector.Operation == "build_artifact" {
		value, err := Decode(readCorpus(t, "projections/positive/report.json"))
		if err != nil {
			t.Fatal(err)
		}
		value.SchemaVersion = "http_retry_check.report.v2"
		if files, buildErr := BuildArtifact(value); files != nil || buildErr != invalidArtifact {
			t.Fatalf("BuildArtifact = %#v/%v", files, buildErr)
		}
		return
	}
	if vector.Operation == "validate_nil" {
		if err := ValidateArtifact(nil); err != invalidArtifact {
			t.Fatalf("ValidateArtifact(nil) = %v", err)
		}
		return
	}
	files := corpusArtifact(t, vector.BaseCase)
	if vector.Operation == "native_alias_probe" {
		value, err := Decode(files[1].Contents)
		if err != nil {
			t.Fatal(err)
		}
		first, _ := BuildArtifact(value)
		second, _ := BuildArtifact(value)
		first[1].Contents[0] ^= 0xff
		if bytes.Equal(first[1].Contents, second[1].Contents) || ValidateArtifact(second) != nil {
			t.Fatal("artifact payloads are not detached")
		}
		return
	}
	if vector.Operation == "mutate_after_validate" {
		if ValidateArtifact(files) != nil {
			t.Fatal("base artifact invalid")
		}
		files[fileIndex(t, files, vector.Target)].Contents[0] ^= 0xff
		if err := ValidateArtifact(files); err != invalidArtifact {
			t.Fatalf("mutated artifact = %v", err)
		}
		return
	}
	switch vector.Operation {
	case "remove_file":
		index := fileIndex(t, files, vector.Target)
		files = append(files[:index:index], files[index+1:]...)
	case "add_file":
		files = append(files, ArtifactFile{Name: vector.Target, MediaType: "text/plain", Contents: []byte("marker")})
	case "swap_files":
		left, right := fileIndex(t, files, vector.Target), fileIndex(t, files, vector.Operand)
		files[left], files[right] = files[right], files[left]
	case "alias_contents":
		files[fileIndex(t, files, vector.Target)].Contents = files[fileIndex(t, files, vector.Operand)].Contents
	case "duplicate_file":
		index := fileIndex(t, files, vector.Target)
		files = append(files, files[index])
	case "flip_byte":
		index := fileIndex(t, files, vector.Target)
		position, err := strconv.Atoi(vector.Operand)
		if err != nil || position < 0 || position >= len(files[index].Contents) {
			t.Fatal("invalid flip vector")
		}
		files[index].Contents[position] ^= 0xff
	case "insert_whitespace":
		index := fileIndex(t, files, vector.Target)
		files[index].Contents = append([]byte(vector.Operand), files[index].Contents...)
	case "swap_members":
		index := fileIndex(t, files, manifestName)
		files[index].Contents = swapJSONMemberLines(t, files[index].Contents,
			pointerLeaf(vector.Target), pointerLeaf(vector.Operand))
	case "add_member":
		index := fileIndex(t, files, manifestName)
		files[index].Contents = addRootJSONMember(files[index].Contents, pointerLeaf(vector.Target), vector.Operand)
	case "set":
		index := fileIndex(t, files, manifestName)
		files[index].Contents = replaceJSONValue(t, files[index].Contents, pointerLeaf(vector.Target), vector.Operand)
	case "replace_file_from_case":
		index := fileIndex(t, files, vector.Target)
		files[index].Contents = readCorpus(t, "projections/"+vector.Operand+"/"+vector.Target)
	case "resize_file":
		index := fileIndex(t, files, vector.Target)
		files[index].Contents = bytes.Repeat([]byte(vector.Operand), vector.Count)
	case "resize_set":
		for index := range files {
			size := MaxProjectionBytes
			if index == len(files)-1 {
				size++
			}
			files[index].Contents = bytes.Repeat([]byte(vector.Operand), size)
		}
	case "mutate_file_name":
		files[fileIndex(t, files, vector.Target)].Name = vector.Operand
	default:
		t.Fatalf("unknown artifact operation %q", vector.Operation)
	}
	if vector.Expected != "invalid_artifact" {
		t.Fatalf("unexpected result class %q", vector.Expected)
	}
	before := cloneArtifact(files)
	if err := ValidateArtifact(files); err != invalidArtifact {
		t.Fatalf("ValidateArtifact = %v", err)
	}
	if !reflect.DeepEqual(files, before) {
		t.Fatal("ValidateArtifact mutated corpus candidate")
	}
}

func applyCanonicalJSONVector(t *testing.T, vector corpusInvalidCase, base []byte) []byte {
	t.Helper()
	switch vector.Operation {
	case "replace_base64":
		return decodeBase64(t, vector.Operand)
	case "oversize_ascii":
		return bytes.Repeat([]byte(vector.Operand), vector.Count)
	case "prefix_base64":
		return append(decodeBase64(t, vector.Operand), base...)
	case "suffix_base64":
		return append(append([]byte{}, base...), decodeBase64(t, vector.Operand)...)
	case "remove_terminal_lf":
		return append([]byte{}, base[:len(base)-1]...)
	case "reindent":
		var value any
		if err := json.Unmarshal(base, &value); err != nil {
			t.Fatal(err)
		}
		indent := strings.Repeat(" ", mustAtoi(t, vector.Operand))
		encoded, err := json.MarshalIndent(value, "", indent)
		if err != nil {
			t.Fatal(err)
		}
		return append(encoded, '\n')
	case "insert_whitespace":
		return append([]byte(vector.Operand), base...)
	case "swap_members":
		return swapJSONMemberLines(t, base, pointerLeaf(vector.Target), pointerLeaf(vector.Operand))
	case "replace_escape":
		return bytes.Replace(base, []byte(ClaimCeiling), []byte(vector.Operand+ClaimCeiling[1:]), 1)
	case "duplicate_member":
		return duplicateJSONMember(t, base, pointerLeaf(vector.Target))
	case "add_member":
		return addRootJSONMember(base, pointerLeaf(vector.Target), vector.Operand)
	case "replace_json":
		return append([]byte(vector.Operand), '\n')
	case "set":
		return replaceJSONValue(t, base, pointerLeaf(vector.Target), vector.Operand)
	case "replace_object_members":
		var output strings.Builder
		output.WriteString("{\n")
		for index := 0; index < vector.Count; index++ {
			fmt.Fprintf(&output, "  \"%s%d\": %d", vector.Operand, index, index)
			if index+1 != vector.Count {
				output.WriteByte(',')
			}
			output.WriteByte('\n')
		}
		output.WriteString("}\n")
		return []byte(output.String())
	case "replace_array_items":
		items := make([]string, vector.Count)
		for index := range items {
			items[index] = vector.Operand
		}
		return []byte("[" + strings.Join(items, ",") + "]\n")
	default:
		t.Fatalf("unknown canonical JSON operation %q", vector.Operation)
		return nil
	}
}

func setReportModelValue(t *testing.T, value *Report, target, operand string) {
	t.Helper()
	var stringValue string
	if strings.HasPrefix(operand, `"`) {
		if err := json.Unmarshal([]byte(operand), &stringValue); err != nil {
			t.Fatal(err)
		}
	}
	setString := func(destination any) { reflect.ValueOf(destination).Elem().SetString(stringValue) }
	switch target {
	case "/outcome":
		setString(&value.Outcome)
	case "/schema_version":
		value.SchemaVersion = stringValue
	case "/suite_identity":
		value.SuiteIdentity = stringValue
	case "/explanation_identity":
		value.ExplanationIdentity = stringValue
	case "/claim_ceiling":
		value.ClaimCeiling = stringValue
	case "/scenarios/0/scenario_text":
		value.Scenarios[0].ScenarioText = stringValue
	case "/assessment_text":
		value.AssessmentText = stringValue
	case "/scenarios/0/assessment_text":
		value.Scenarios[0].AssessmentText = stringValue
	case "/scenarios/5/findings/0/text":
		value.Scenarios[5].Findings[0].Text = stringValue
	case "/assessment":
		setString(&value.Assessment)
	case "/summary/passed":
		parsed, err := strconv.ParseUint(operand, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		value.Summary.Passed = uint32(parsed)
	default:
		t.Fatalf("unknown report target %q", target)
	}
}

func loadInvalidBundle(t *testing.T, name, category string) corpusInvalidBundle {
	t.Helper()
	contents := readCorpus(t, "invalid/"+name)
	var bundle corpusInvalidBundle
	if err := json.Unmarshal(contents, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.SchemaVersion != "http_retry_check.conformance_invalid.v1" ||
		bundle.Category != category || bundle.Cases == nil || len(bundle.Cases) == 0 {
		t.Fatalf("invalid bundle identity: %#v", bundle)
	}
	return bundle
}

func representedByGo(vector corpusInvalidCase) bool {
	for _, binding := range vector.Representability {
		if binding == "go" {
			return true
		}
	}
	return false
}

func corpusArtifact(t *testing.T, caseID string) []ArtifactFile {
	t.Helper()
	return []ArtifactFile{
		{Name: manifestName, MediaType: jsonMediaType, Contents: readCorpus(t, "projections/"+caseID+"/manifest.json")},
		{Name: reportName, MediaType: jsonMediaType, Contents: readCorpus(t, "projections/"+caseID+"/report.json")},
		{Name: junitName, MediaType: xmlMediaType, Contents: readCorpus(t, "projections/"+caseID+"/junit.xml")},
		{Name: summaryName, MediaType: markdownMediaType, Contents: readCorpus(t, "projections/"+caseID+"/summary.md")},
	}
}

func corpusRoot(t *testing.T) string {
	t.Helper()
	return filepath.Clean(filepath.Join(reportSourceDirectory(t), "..", "..", "..", "..",
		"conformance", "http-retry-check", "v1"))
}

func readCorpus(t *testing.T, relative string) []byte {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(corpusRoot(t), filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return contents
}

func fileIndex(t *testing.T, files []ArtifactFile, name string) int {
	t.Helper()
	for index, file := range files {
		if file.Name == name {
			return index
		}
	}
	t.Fatalf("artifact file %q not found", name)
	return -1
}

func pointerLeaf(pointer string) string {
	parts := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	return parts[len(parts)-1]
}

func replaceJSONValue(t *testing.T, input []byte, member, replacement string) []byte {
	t.Helper()
	prefix := []byte(`"` + member + `": `)
	at := bytes.Index(input, prefix)
	if at < 0 {
		t.Fatalf("JSON member %q not found", member)
	}
	start := at + len(prefix)
	end := jsonValueEnd(t, input, start)
	output := append([]byte{}, input[:start]...)
	output = append(output, replacement...)
	return append(output, input[end:]...)
}

func jsonValueEnd(t *testing.T, input []byte, start int) int {
	t.Helper()
	if start >= len(input) {
		t.Fatal("missing JSON value")
	}
	if input[start] == '"' {
		escaped := false
		for index := start + 1; index < len(input); index++ {
			if input[index] == '"' && !escaped {
				return index + 1
			}
			if input[index] == '\\' && !escaped {
				escaped = true
			} else {
				escaped = false
			}
		}
		t.Fatal("unterminated JSON string")
	}
	if input[start] == '{' || input[start] == '[' {
		open := input[start]
		close := byte('}')
		if open == '[' {
			close = ']'
		}
		depth := 0
		inString := false
		escaped := false
		for index := start; index < len(input); index++ {
			current := input[index]
			if inString {
				if current == '"' && !escaped {
					inString = false
				}
				if current == '\\' && !escaped {
					escaped = true
				} else {
					escaped = false
				}
				continue
			}
			if current == '"' {
				inString = true
				continue
			}
			if current == open {
				depth++
			}
			if current == close {
				depth--
				if depth == 0 {
					return index + 1
				}
			}
		}
		t.Fatal("unterminated JSON container")
	}
	for index := start; index < len(input); index++ {
		if input[index] == ',' || input[index] == '\n' || input[index] == '}' || input[index] == ']' {
			return index
		}
	}
	return len(input)
}

func duplicateJSONMember(t *testing.T, input []byte, member string) []byte {
	t.Helper()
	return duplicateJSONMemberOccurrence(t, input, member, 1)
}

func duplicateJSONMemberOccurrence(t *testing.T, input []byte, member string, occurrence int) []byte {
	t.Helper()
	prefix := []byte(`"` + member + `": `)
	at := -1
	searchFrom := 0
	for current := 0; current < occurrence; current++ {
		relative := bytes.Index(input[searchFrom:], prefix)
		if relative < 0 {
			at = -1
			break
		}
		at = searchFrom + relative
		searchFrom = at + len(prefix)
	}
	if occurrence < 1 || at < 0 {
		t.Fatalf("JSON member %q not found", member)
	}
	lineStart := bytes.LastIndex(input[:at], []byte("\n")) + 1
	lineEndRelative := bytes.IndexByte(input[at:], '\n')
	if lineEndRelative < 0 {
		t.Fatal("JSON member line has no LF")
	}
	lineEnd := at + lineEndRelative + 1
	line := append([]byte{}, input[lineStart:lineEnd]...)
	if newline := bytes.LastIndexByte(line, '\n'); newline > 0 && line[newline-1] != ',' {
		line = append(line[:newline], ',', '\n')
	}
	output := append([]byte{}, input[:lineStart]...)
	output = append(output, line...)
	return append(output, input[lineStart:]...)
}

func removeJSONMember(t *testing.T, input []byte, member string) []byte {
	t.Helper()
	prefix := []byte(`"` + member + `": `)
	at := bytes.Index(input, prefix)
	if at < 0 {
		t.Fatalf("JSON member %q not found", member)
	}
	lineStart := bytes.LastIndex(input[:at], []byte("\n")) + 1
	start := at + len(prefix)
	end := jsonValueEnd(t, input, start)
	for end < len(input) && (input[end] == ',' || input[end] == '\n') {
		end++
	}
	return append(append([]byte{}, input[:lineStart]...), input[end:]...)
}

func swapJSONMemberLines(t *testing.T, input []byte, left, right string) []byte {
	t.Helper()
	lines := bytes.SplitAfter(input, []byte("\n"))
	leftIndex, rightIndex := -1, -1
	leftPattern, rightPattern := []byte(`"`+left+`":`), []byte(`"`+right+`":`)
	for index, line := range lines {
		if leftIndex < 0 && bytes.Contains(line, leftPattern) {
			leftIndex = index
		}
		if rightIndex < 0 && bytes.Contains(line, rightPattern) {
			rightIndex = index
		}
	}
	if leftIndex < 0 || rightIndex < 0 {
		t.Fatalf("cannot swap JSON members %q/%q", left, right)
	}
	lines[leftIndex], lines[rightIndex] = lines[rightIndex], lines[leftIndex]
	return bytes.Join(lines, nil)
}

func addRootJSONMember(input []byte, member, value string) []byte {
	insertion := []byte("  \"" + member + "\": " + value + ",\n")
	return append(append([]byte{}, input[:2]...), append(insertion, input[2:]...)...)
}

func decodeBase64(t *testing.T, value string) []byte {
	t.Helper()
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func mustAtoi(t *testing.T, value string) int {
	t.Helper()
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func sha256Hex(contents []byte) string {
	digest := sha256.Sum256(contents)
	return hex.EncodeToString(digest[:])
}
