package report

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"strconv"
	"testing"
)

func TestArtifactOrderManifestBindingAndIndependence(t *testing.T) {
	value := mustReport(t, mixedResult())
	before := cloneReport(value)
	files, err := BuildArtifact(value)
	if err != nil || ValidateArtifact(files) != nil || !reflect.DeepEqual(value, before) {
		t.Fatalf("artifact invalid or report mutated: %v", err)
	}
	specifications := artifactSpecifications()
	if len(files) != len(specifications) {
		t.Fatalf("artifact count = %d", len(files))
	}
	total := 0
	for index, specification := range specifications {
		file := files[index]
		total += len(file.Contents)
		if file.Name != specification.name || file.MediaType != specification.mediaType ||
			file.Contents == nil || len(file.Contents) == 0 || len(file.Contents) > MaxProjectionBytes {
			t.Fatalf("artifact file %d = %#v", index, file)
		}
	}
	if total > MaxArtifactBytes {
		t.Fatalf("artifact bytes = %d", total)
	}
	var manifest artifactManifest
	if !decodeCanonicalJSON(files[0].Contents, &manifest) ||
		manifest.SchemaVersion != artifactManifestSchema || manifest.ReportSchemaVersion != SchemaVersion ||
		manifest.ClaimCeiling != ClaimCeiling || manifest.DigestDomain != artifactDigestDomain ||
		len(manifest.Files) != 3 || manifest.AggregateSHA256 != aggregateArtifactDigest(manifest.Files) {
		t.Fatalf("manifest drift: %#v", manifest)
	}
	for index, descriptor := range manifest.Files {
		if descriptor != descriptorFor(files[index+1]) {
			t.Fatalf("descriptor %d drift: %#v", index, descriptor)
		}
	}

	second, err := BuildArtifact(value)
	if err != nil || !equalArtifacts(files, second) {
		t.Fatal("repeated artifact drift")
	}
	beforeSecond := cloneArtifact(second)
	files[1].Contents[0] ^= 0xff
	if !reflect.DeepEqual(second, beforeSecond) || bytes.Equal(files[1].Contents, second[1].Contents) {
		t.Fatal("artifact results alias")
	}
	for changed := range second {
		candidate, _ := BuildArtifact(value)
		snapshot := cloneArtifact(candidate)
		candidate[changed].Contents[0] ^= 0xff
		for index := range candidate {
			if index == changed {
				continue
			}
			if !bytes.Equal(candidate[index].Contents, snapshot[index].Contents) {
				t.Fatalf("artifact files %d and %d alias", changed, index)
			}
		}
	}
}

func TestArtifactAggregateUsesIndependentFramedPreimage(t *testing.T) {
	files := mustArtifact(t, mixedResult())
	var manifest artifactManifest
	if !decodeCanonicalJSON(files[0].Contents, &manifest) {
		t.Fatal("decode manifest")
	}
	atoms := []string{artifactDigestDomain, artifactManifestSchema, SchemaVersion, ClaimCeiling}
	for _, descriptor := range manifest.Files {
		atoms = append(atoms, descriptor.Name, descriptor.MediaType,
			strconv.FormatUint(descriptor.SizeBytes, 10), descriptor.SHA256)
	}
	var preimage bytes.Buffer
	for _, atom := range atoms {
		var prefix [8]byte
		binary.BigEndian.PutUint64(prefix[:], uint64(len([]byte(atom))))
		preimage.Write(prefix[:])
		preimage.WriteString(atom)
	}
	if sha256Hex(preimage.Bytes()) != manifest.AggregateSHA256 {
		t.Fatal("aggregate differs from independently framed preimage")
	}
	remaining := preimage.Bytes()
	for index, atom := range atoms {
		if len(remaining) < 8 {
			t.Fatalf("atom %d has no frame", index)
		}
		length := binary.BigEndian.Uint64(remaining[:8])
		remaining = remaining[8:]
		if length != uint64(len([]byte(atom))) || length > uint64(len(remaining)) ||
			!bytes.Equal(remaining[:length], []byte(atom)) {
			t.Fatalf("atom %d frame drift", index)
		}
		remaining = remaining[length:]
	}
	if len(remaining) != 0 {
		t.Fatal("aggregate preimage trailing bytes")
	}
	var unicode bytes.Buffer
	writeDigestAtom(&unicode, "é")
	if !bytes.Equal(unicode.Bytes(), []byte{0, 0, 0, 0, 0, 0, 0, 2, 0xc3, 0xa9}) {
		t.Fatalf("UTF-8 framing = %x", unicode.Bytes())
	}
}

func TestValidateArtifactRejectsMalformedCorruptAndMixedArtifacts(t *testing.T) {
	base := mustArtifact(t, unsafeResult())
	positive := mustArtifact(t, positiveResult())
	tests := []struct {
		name string
		edit func([]ArtifactFile) []ArtifactFile
	}{
		{"nil", func([]ArtifactFile) []ArtifactFile { return nil }},
		{"missing", func(files []ArtifactFile) []ArtifactFile { return files[:3] }},
		{"extra", func(files []ArtifactFile) []ArtifactFile { return append(files, files[3]) }},
		{"reordered", func(files []ArtifactFile) []ArtifactFile { files[1], files[2] = files[2], files[1]; return files }},
		{"name alias", func(files []ArtifactFile) []ArtifactFile { files[1].Name = "./report.json"; return files }},
		{"case alias", func(files []ArtifactFile) []ArtifactFile { files[1].Name = "REPORT.JSON"; return files }},
		{"duplicate name", func(files []ArtifactFile) []ArtifactFile { files[2].Name = files[1].Name; return files }},
		{"media alias", func(files []ArtifactFile) []ArtifactFile { files[2].MediaType = "text/xml"; return files }},
		{"nil content", func(files []ArtifactFile) []ArtifactFile { files[2].Contents = nil; return files }},
		{"empty content", func(files []ArtifactFile) []ArtifactFile { files[2].Contents = []byte{}; return files }},
		{"oversized", func(files []ArtifactFile) []ArtifactFile {
			files[2].Contents = make([]byte, MaxProjectionBytes+1)
			return files
		}},
		{"report corrupt", func(files []ArtifactFile) []ArtifactFile { files[1].Contents[20] ^= 1; return files }},
		{"junit corrupt", func(files []ArtifactFile) []ArtifactFile { files[2].Contents[20] ^= 1; return files }},
		{"summary corrupt", func(files []ArtifactFile) []ArtifactFile { files[3].Contents[20] ^= 1; return files }},
		{"crossed junit", func(files []ArtifactFile) []ArtifactFile {
			files[2].Contents = append([]byte{}, positive[2].Contents...)
			return rebindManifest(files)
		}},
		{"crossed summary", func(files []ArtifactFile) []ArtifactFile {
			files[3].Contents = append([]byte{}, positive[3].Contents...)
			return rebindManifest(files)
		}},
		{"manifest whitespace", func(files []ArtifactFile) []ArtifactFile {
			files[0].Contents = append([]byte(" "), files[0].Contents...)
			return files
		}},
		{"manifest duplicate", func(files []ArtifactFile) []ArtifactFile {
			files[0].Contents = bytes.Replace(files[0].Contents, []byte("  \"schema_version\": "),
				[]byte("  \"schema_version\": \"duplicate\",\n  \"schema_version\": "), 1)
			return files
		}},
		{"manifest unknown", func(files []ArtifactFile) []ArtifactFile {
			files[0].Contents = bytes.Replace(files[0].Contents, []byte("  \"schema_version\": "),
				[]byte("  \"unknown\": true,\n  \"schema_version\": "), 1)
			return files
		}},
		{"manifest schema", mutateManifest(func(v *artifactManifest) { v.SchemaVersion = sanitationMarker })},
		{"manifest report schema", mutateManifest(func(v *artifactManifest) { v.ReportSchemaVersion = sanitationMarker })},
		{"manifest claim", mutateManifest(func(v *artifactManifest) { v.ClaimCeiling = sanitationMarker })},
		{"manifest domain", mutateManifest(func(v *artifactManifest) { v.DigestDomain = sanitationMarker })},
		{"manifest aggregate", mutateManifest(func(v *artifactManifest) { v.AggregateSHA256 = strings64("0") })},
		{"descriptor order", mutateManifest(func(v *artifactManifest) {
			v.Files[0], v.Files[1] = v.Files[1], v.Files[0]
			v.AggregateSHA256 = aggregateArtifactDigest(v.Files)
		})},
		{"descriptor size", mutateManifest(func(v *artifactManifest) {
			v.Files[0].SizeBytes++
			v.AggregateSHA256 = aggregateArtifactDigest(v.Files)
		})},
		{"descriptor hash", mutateManifest(func(v *artifactManifest) {
			v.Files[0].SHA256 = strings64("0")
			v.AggregateSHA256 = aggregateArtifactDigest(v.Files)
		})},
		{"manifest self descriptor", mutateManifest(func(v *artifactManifest) {
			v.Files = append(v.Files, artifactDescriptor{Name: manifestName, MediaType: jsonMediaType, SizeBytes: 1, SHA256: strings64("0")})
			v.AggregateSHA256 = aggregateArtifactDigest(v.Files)
		})},
		{"canonical semantic report", func(files []ArtifactFile) []ArtifactFile {
			value := mustReport(t, unsafeResult())
			value.Outcome = OutcomePass
			files[1].Contents, _ = marshalCanonicalJSON(value)
			return rebindManifest(files)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := test.edit(cloneArtifact(base))
			before := cloneArtifact(candidate)
			if err := ValidateArtifact(candidate); err != invalidArtifact {
				t.Fatalf("ValidateArtifact = %v", err)
			}
			if !reflect.DeepEqual(candidate, before) {
				t.Fatal("ValidateArtifact mutated candidate")
			}
		})
	}
}

func mutateManifest(edit func(*artifactManifest)) func([]ArtifactFile) []ArtifactFile {
	return func(files []ArtifactFile) []ArtifactFile {
		var manifest artifactManifest
		if !decodeCanonicalJSON(files[0].Contents, &manifest) {
			return files
		}
		edit(&manifest)
		files[0].Contents, _ = marshalCanonicalJSON(manifest)
		return files
	}
}

func rebindManifest(files []ArtifactFile) []ArtifactFile {
	manifest, ok := newArtifactManifest(files[1:])
	if ok {
		files[0].Contents, _ = marshalCanonicalJSON(manifest)
	}
	return files
}

func strings64(value string) string {
	return value + value + value + value + value + value + value + value +
		value + value + value + value + value + value + value + value +
		value + value + value + value + value + value + value + value +
		value + value + value + value + value + value + value + value +
		value + value + value + value + value + value + value + value +
		value + value + value + value + value + value + value + value +
		value + value + value + value + value + value + value + value +
		value + value + value + value + value + value + value + value
}
