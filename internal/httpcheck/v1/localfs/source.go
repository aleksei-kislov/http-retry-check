// Package localfs handles evidence and scaffold files for the HTTP Retry Check
// CLI. It does not run clients or make network requests.
package localfs

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/evidence"
)

var (
	ErrInvalidSource     = errors.New("HTTP Retry Check evidence source is invalid")
	ErrTargetUnavailable = errors.New("HTTP Retry Check output target is unavailable")
	ErrInternalFailure   = errors.New("HTTP Retry Check filesystem operation failed internally")
)

type Admission struct {
	Report   []byte
	Artifact []evidence.ArtifactFile
}

// ReadEvidence reads a report file, artifact directory, or standard input.
func ReadEvidence(source string, stdin io.Reader) (Admission, error) {
	if source == "-" {
		contents, err := readBounded(stdin, evidence.MaxProjectionBytes)
		if err != nil {
			return Admission{}, ErrInternalFailure
		}
		if len(contents) == 0 || len(contents) > evidence.MaxProjectionBytes {
			return Admission{}, ErrInvalidSource
		}
		return Admission{Report: contents}, nil
	}
	if source == "" {
		return Admission{}, ErrInvalidSource
	}
	before, err := os.Lstat(source)
	if err != nil || before.Mode()&os.ModeSymlink != 0 {
		return Admission{}, ErrInvalidSource
	}
	if before.Mode().IsRegular() {
		contents, readErr := readStableFile(source, before, evidence.MaxProjectionBytes)
		if readErr != nil {
			return Admission{}, readErr
		}
		return Admission{Report: contents}, nil
	}
	if before.IsDir() {
		files, readErr := readStableArtifact(source, before)
		if readErr != nil {
			return Admission{}, readErr
		}
		return Admission{Artifact: files}, nil
	}
	return Admission{}, ErrInvalidSource
}

func readStableFile(path string, before os.FileInfo, maximum int) ([]byte, error) {
	file, err := openNonblocking(path)
	if err != nil {
		return nil, ErrInternalFailure
	}
	opened, statErr := file.Stat()
	current, currentErr := os.Lstat(path)
	if statErr != nil || currentErr != nil || !stableRegular(before, opened) || !stableRegular(before, current) {
		if closeErr := file.Close(); closeErr != nil {
			return nil, ErrInternalFailure
		}
		return nil, ErrInvalidSource
	}
	contents, readErr := readBounded(file, maximum)
	post, postErr := file.Stat()
	current, currentErr = os.Lstat(path)
	closeErr := file.Close()
	if readErr != nil || postErr != nil || closeErr != nil {
		return nil, ErrInternalFailure
	}
	if currentErr != nil || !stableRegular(before, post) || !stableRegular(before, current) ||
		len(contents) == 0 || len(contents) > maximum {
		return nil, ErrInvalidSource
	}
	return contents, nil
}

func readStableArtifact(path string, before os.FileInfo) ([]evidence.ArtifactFile, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrInternalFailure
	}
	opened, openedErr := root.Stat(".")
	current, currentErr := os.Lstat(path)
	if openedErr != nil || currentErr != nil || !stableDirectory(before, opened) || !stableDirectory(before, current) {
		if closeErr := root.Close(); closeErr != nil {
			return nil, ErrInternalFailure
		}
		return nil, ErrInvalidSource
	}
	initial, inventoryErr := artifactInventory(root)
	if inventoryErr != nil {
		if closeErr := root.Close(); closeErr != nil {
			return nil, ErrInternalFailure
		}
		return nil, inventoryErr
	}
	files := make([]evidence.ArtifactFile, len(artifactFileSpecs))
	for index, specification := range artifactFileSpecs {
		contents, readErr := readArtifactChild(root, specification.name, initial[index], evidence.MaxProjectionBytes)
		if readErr != nil {
			if closeErr := root.Close(); closeErr != nil {
				return nil, ErrInternalFailure
			}
			return nil, readErr
		}
		files[index] = evidence.ArtifactFile{
			Name: specification.name, MediaType: specification.mediaType, Contents: contents,
		}
	}
	post, inventoryErr := artifactInventory(root)
	opened, openedErr = root.Stat(".")
	current, currentErr = os.Lstat(path)
	closeErr := root.Close()
	if openedErr != nil || closeErr != nil {
		return nil, ErrInternalFailure
	}
	if inventoryErr != nil {
		return nil, inventoryErr
	}
	if currentErr != nil || !stableDirectory(before, opened) || !stableDirectory(before, current) ||
		!sameInventory(initial, post) {
		return nil, ErrInvalidSource
	}
	return files, nil
}

type artifactFileSpec struct {
	name      string
	mediaType string
}

var artifactFileSpecs = [...]artifactFileSpec{
	{name: evidence.ManifestName, mediaType: evidence.JSONMediaType},
	{name: evidence.ReportName, mediaType: evidence.JSONMediaType},
	{name: evidence.JUnitName, mediaType: evidence.XMLMediaType},
	{name: evidence.SummaryName, mediaType: evidence.MarkdownMediaType},
}

func artifactInventory(root *os.Root) ([4]os.FileInfo, error) {
	var inventory [4]os.FileInfo
	directory, err := root.Open(".")
	if err != nil {
		return inventory, ErrInternalFailure
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil || len(entries) != len(artifactFileSpecs) {
		if readErr != nil || closeErr != nil {
			return inventory, ErrInternalFailure
		}
		return inventory, ErrInvalidSource
	}
	seen := make(map[string]bool, len(entries))
	for _, entry := range entries {
		seen[entry.Name()] = true
	}
	for index, specification := range artifactFileSpecs {
		if !seen[specification.name] {
			return inventory, ErrInvalidSource
		}
		info, inspectErr := root.Lstat(specification.name)
		if inspectErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			if inspectErr != nil && !isNotExist(inspectErr) {
				return inventory, ErrInternalFailure
			}
			return inventory, ErrInvalidSource
		}
		inventory[index] = info
	}
	return inventory, nil
}

func readArtifactChild(root *os.Root, name string, before os.FileInfo, maximum int) ([]byte, error) {
	file, err := root.OpenFile(name, os.O_RDONLY|nonblockingFlag(), 0)
	if err != nil {
		return nil, ErrInternalFailure
	}
	opened, statErr := file.Stat()
	current, currentErr := root.Lstat(name)
	if statErr != nil || currentErr != nil || !stableRegular(before, opened) || !stableRegular(before, current) {
		if closeErr := file.Close(); closeErr != nil {
			return nil, ErrInternalFailure
		}
		return nil, ErrInvalidSource
	}
	contents, readErr := readBounded(file, maximum)
	post, postErr := file.Stat()
	current, currentErr = root.Lstat(name)
	closeErr := file.Close()
	if readErr != nil || postErr != nil || closeErr != nil {
		return nil, ErrInternalFailure
	}
	if currentErr != nil || !stableRegular(before, post) || !stableRegular(before, current) ||
		len(contents) == 0 || len(contents) > maximum {
		return nil, ErrInvalidSource
	}
	return contents, nil
}

func readBounded(source io.Reader, maximum int) ([]byte, error) {
	limited := &io.LimitedReader{R: source, N: int64(maximum) + 1}
	return io.ReadAll(limited)
}

func stableRegular(expected, current os.FileInfo) bool {
	return expected != nil && current != nil && current.Mode()&os.ModeSymlink == 0 &&
		current.Mode().IsRegular() && os.SameFile(expected, current)
}

func stableDirectory(expected, current os.FileInfo) bool {
	return expected != nil && current != nil && current.Mode()&os.ModeSymlink == 0 &&
		current.IsDir() && os.SameFile(expected, current)
}

func sameInventory(left, right [4]os.FileInfo) bool {
	for index := range left {
		if !stableRegular(left[index], right[index]) {
			return false
		}
	}
	return true
}

func isNotExist(err error) bool {
	return errors.Is(err, fs.ErrNotExist)
}
