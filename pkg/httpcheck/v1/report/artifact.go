package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strconv"
)

const (
	artifactManifestSchema = "http_retry_check.artifact_manifest.v1"
	artifactDigestDomain   = "http_retry_check.artifact_set.v1"

	manifestName = "manifest.json"
	reportName   = "report.json"
	junitName    = "junit.xml"
	summaryName  = "summary.md"

	jsonMediaType     = "application/json"
	xmlMediaType      = "application/xml"
	markdownMediaType = "text/markdown; charset=utf-8"
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

// BuildArtifact returns the manifest, report, JUnit, and Markdown files in that
// order. Each returned file contains an independent copy of its bytes.
func BuildArtifact(value Report) ([]ArtifactFile, error) {
	reportJSON, err := Encode(value)
	if err != nil {
		return nil, invalidArtifact
	}
	junit, err := JUnit(value)
	if err != nil {
		return nil, invalidArtifact
	}
	summary, err := GitHubSummary(value)
	if err != nil {
		return nil, invalidArtifact
	}
	payloads := []ArtifactFile{
		{Name: reportName, MediaType: jsonMediaType, Contents: append([]byte{}, reportJSON...)},
		{Name: junitName, MediaType: xmlMediaType, Contents: append([]byte{}, junit...)},
		{Name: summaryName, MediaType: markdownMediaType, Contents: append([]byte{}, summary...)},
	}
	manifest, ok := newArtifactManifest(payloads)
	if !ok {
		return nil, invalidArtifact
	}
	manifestJSON, ok := marshalCanonicalJSON(manifest)
	if !ok {
		return nil, invalidArtifact
	}
	files := make([]ArtifactFile, 0, 4)
	files = append(files, ArtifactFile{
		Name: manifestName, MediaType: jsonMediaType, Contents: append([]byte{}, manifestJSON...),
	})
	files = append(files, payloads...)
	if !validArtifactBounds(files) {
		return nil, invalidArtifact
	}
	return cloneArtifact(files), nil
}

// ValidateArtifact checks the four files, their order and metadata, and all
// report projections and digests.
func ValidateArtifact(files []ArtifactFile) error {
	if !validArtifactBounds(files) || !validArtifactShape(files) {
		return invalidArtifact
	}
	var manifest artifactManifest
	if !decodeCanonicalJSON(files[0].Contents, &manifest) ||
		!validateArtifactManifest(manifest, files[1:]) {
		return invalidArtifact
	}
	value, err := Decode(files[1].Contents)
	if err != nil {
		return invalidArtifact
	}
	want, err := BuildArtifact(value)
	if err != nil || !equalArtifacts(files, want) {
		return invalidArtifact
	}
	return nil
}

func newArtifactManifest(payloads []ArtifactFile) (artifactManifest, bool) {
	if len(payloads) != 3 {
		return artifactManifest{}, false
	}
	descriptors := make([]artifactDescriptor, len(payloads))
	for index, file := range payloads {
		descriptors[index] = descriptorFor(file)
	}
	manifest := artifactManifest{
		SchemaVersion: artifactManifestSchema, ReportSchemaVersion: SchemaVersion,
		ClaimCeiling: ClaimCeiling, DigestDomain: artifactDigestDomain, Files: descriptors,
	}
	manifest.AggregateSHA256 = aggregateArtifactDigest(manifest.Files)
	return manifest, true
}

func validateArtifactManifest(manifest artifactManifest, payloads []ArtifactFile) bool {
	if manifest.SchemaVersion != artifactManifestSchema ||
		manifest.ReportSchemaVersion != SchemaVersion || manifest.ClaimCeiling != ClaimCeiling ||
		manifest.DigestDomain != artifactDigestDomain || len(manifest.Files) != 3 ||
		len(payloads) != len(manifest.Files) || !validSHA256(manifest.AggregateSHA256) {
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
	writeDigestAtom(digest, artifactDigestDomain)
	writeDigestAtom(digest, artifactManifestSchema)
	writeDigestAtom(digest, SchemaVersion)
	writeDigestAtom(digest, ClaimCeiling)
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
	if files == nil || len(files) != 4 {
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
	specifications := artifactSpecifications()
	for index, specification := range specifications {
		if files[index].Name != specification.name || files[index].MediaType != specification.mediaType {
			return false
		}
	}
	return true
}

func artifactSpecifications() [4]artifactSpec {
	return [4]artifactSpec{
		{name: manifestName, mediaType: jsonMediaType},
		{name: reportName, mediaType: jsonMediaType},
		{name: junitName, mediaType: xmlMediaType},
		{name: summaryName, mediaType: markdownMediaType},
	}
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
		var contents []byte
		if file.Contents != nil {
			contents = append([]byte{}, file.Contents...)
		}
		cloned[index] = ArtifactFile{
			Name: file.Name, MediaType: file.MediaType, Contents: contents,
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
