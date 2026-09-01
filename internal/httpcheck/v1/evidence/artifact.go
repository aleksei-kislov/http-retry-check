package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strconv"
)

const (
	ManifestName = "manifest.json"
	ReportName   = "report.json"
	JUnitName    = "junit.xml"
	SummaryName  = "summary.md"

	JSONMediaType     = "application/json"
	XMLMediaType      = "application/xml"
	MarkdownMediaType = "text/markdown; charset=utf-8"
)

type artifactManifest struct {
	SchemaVersion       string               `json:"schema_version"`
	ReportSchemaVersion string               `json:"report_schema_version"`
	ClaimCeiling        string               `json:"claim_ceiling"`
	DigestDomain        string               `json:"digest_domain"`
	AggregateSHA256     string               `json:"aggregate_sha256"`
	Files               []artifactDescriptor `json:"files"`
}

type artifactDescriptor struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	SizeBytes uint64 `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type artifactSpec struct {
	name      string
	mediaType string
}

var artifactSpecs = [...]artifactSpec{
	{name: ManifestName, mediaType: JSONMediaType},
	{name: ReportName, mediaType: JSONMediaType},
	{name: JUnitName, mediaType: XMLMediaType},
	{name: SummaryName, mediaType: MarkdownMediaType},
}

func BuildArtifact(report Report) ([]ArtifactFile, error) {
	reportJSON, err := EncodeReport(report)
	if err != nil {
		return nil, invalidArtifact
	}
	junit, err := JUnit(report)
	if err != nil {
		return nil, invalidArtifact
	}
	summary, err := GitHubSummary(report)
	if err != nil {
		return nil, invalidArtifact
	}
	payloads := []ArtifactFile{
		{Name: ReportName, MediaType: JSONMediaType, Contents: append([]byte{}, reportJSON...)},
		{Name: JUnitName, MediaType: XMLMediaType, Contents: append([]byte{}, junit...)},
		{Name: SummaryName, MediaType: MarkdownMediaType, Contents: append([]byte{}, summary...)},
	}
	descriptors := make([]artifactDescriptor, len(payloads))
	for index, file := range payloads {
		descriptors[index] = descriptorFor(file)
	}
	manifest := artifactManifest{
		SchemaVersion: ArtifactManifestSchema, ReportSchemaVersion: SchemaVersion,
		ClaimCeiling: ClaimCeiling, DigestDomain: ArtifactDigestDomain, Files: descriptors,
	}
	manifest.AggregateSHA256 = aggregateArtifactDigest(descriptors)
	manifestJSON, ok := marshalCanonicalJSON(manifest)
	if !ok {
		return nil, invalidArtifact
	}
	files := append([]ArtifactFile{{
		Name: ManifestName, MediaType: JSONMediaType, Contents: append([]byte{}, manifestJSON...),
	}}, payloads...)
	if !validArtifactBounds(files) {
		return nil, invalidArtifact
	}
	return cloneArtifact(files), nil
}

func ValidateArtifact(files []ArtifactFile) error {
	if !validArtifactBounds(files) || !validArtifactShape(files) {
		return invalidArtifact
	}
	var manifest artifactManifest
	if !decodeCanonicalManifest(files[0].Contents, &manifest) ||
		!validateArtifactManifest(manifest, files[1:]) {
		return invalidArtifact
	}
	report, err := DecodeReport(files[1].Contents)
	if err != nil {
		return invalidArtifact
	}
	want, err := BuildArtifact(report)
	if err != nil || !equalArtifacts(files, want) {
		return invalidArtifact
	}
	return nil
}

func decodeCanonicalManifest(encoded []byte, manifest *artifactManifest) bool {
	if len(encoded) == 0 || len(encoded) > MaxProjectionBytes || !boundedJSONShape(encoded) {
		return false
	}
	decoder := newStrictJSONDecoder(encoded)
	if decoder.Decode(manifest) != nil || !decoderAtEOF(decoder) {
		return false
	}
	canonical, ok := marshalCanonicalJSON(*manifest)
	return ok && bytes.Equal(encoded, canonical)
}

func validateArtifactManifest(manifest artifactManifest, payloads []ArtifactFile) bool {
	if manifest.SchemaVersion != ArtifactManifestSchema || manifest.ReportSchemaVersion != SchemaVersion ||
		manifest.ClaimCeiling != ClaimCeiling || manifest.DigestDomain != ArtifactDigestDomain ||
		manifest.Files == nil || len(manifest.Files) != 3 || len(payloads) != len(manifest.Files) ||
		!validSHA256(manifest.AggregateSHA256) {
		return false
	}
	for index, descriptor := range manifest.Files {
		if descriptor != descriptorFor(payloads[index]) {
			return false
		}
	}
	return manifest.AggregateSHA256 == aggregateArtifactDigest(manifest.Files)
}

func descriptorFor(file ArtifactFile) artifactDescriptor {
	digest := sha256.Sum256(file.Contents)
	return artifactDescriptor{
		Name: file.Name, MediaType: file.MediaType, SizeBytes: uint64(len(file.Contents)),
		SHA256: hex.EncodeToString(digest[:]),
	}
}

func aggregateArtifactDigest(files []artifactDescriptor) string {
	digest := sha256.New()
	for _, atom := range []string{ArtifactDigestDomain, ArtifactManifestSchema, SchemaVersion, ClaimCeiling} {
		writeDigestAtom(digest, atom)
	}
	for _, file := range files {
		writeDigestAtom(digest, file.Name)
		writeDigestAtom(digest, file.MediaType)
		writeDigestAtom(digest, strconv.FormatUint(file.SizeBytes, 10))
		writeDigestAtom(digest, file.SHA256)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

type digestWriter interface {
	Write([]byte) (int, error)
}

func writeDigestAtom(destination digestWriter, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write([]byte(value))
}

func validArtifactBounds(files []ArtifactFile) bool {
	if files == nil || len(files) != len(artifactSpecs) {
		return false
	}
	total := 0
	for _, file := range files {
		if file.Contents == nil || len(file.Contents) == 0 || len(file.Contents) > MaxProjectionBytes {
			return false
		}
		total += len(file.Contents)
	}
	return total <= MaxArtifactBytes
}

func validArtifactShape(files []ArtifactFile) bool {
	for index, specification := range artifactSpecs {
		if files[index].Name != specification.name || files[index].MediaType != specification.mediaType {
			return false
		}
	}
	return true
}

func equalArtifacts(left, right []ArtifactFile) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Name != right[index].Name || left[index].MediaType != right[index].MediaType ||
			!bytes.Equal(left[index].Contents, right[index].Contents) {
			return false
		}
	}
	return true
}

func cloneArtifact(files []ArtifactFile) []ArtifactFile {
	if files == nil {
		return nil
	}
	cloned := make([]ArtifactFile, len(files))
	for index, file := range files {
		cloned[index] = ArtifactFile{
			Name: file.Name, MediaType: file.MediaType, Contents: append([]byte{}, file.Contents...),
		}
	}
	return cloned
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
