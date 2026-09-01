// Package scaffold provides the embedded Go and C# test scaffolds.
package scaffold

import (
	_ "embed"
	"errors"
)

var ErrInvalidLanguage = errors.New("HTTP Retry Check scaffold language is invalid")

type File struct {
	Name     string
	Contents []byte
}

//go:embed templates/go/README.md
var goReadme []byte

//go:embed templates/go/http_retry_test.go.txt
var goTest []byte

//go:embed templates/csharp/README.md
var csharpReadme []byte

//go:embed templates/csharp/HttpRetryTests.cs
var csharpTest []byte

// Files returns copies of the selected scaffold files in their expected order.
func Files(language string) ([]File, error) {
	var source []File
	switch language {
	case "go":
		source = []File{{Name: "README.md", Contents: goReadme}, {Name: "http_retry_test.go", Contents: goTest}}
	case "csharp":
		source = []File{{Name: "README.md", Contents: csharpReadme}, {Name: "HttpRetryTests.cs", Contents: csharpTest}}
	default:
		return nil, ErrInvalidLanguage
	}
	files := make([]File, len(source))
	for index, file := range source {
		files[index] = File{Name: file.Name, Contents: append([]byte{}, file.Contents...)}
	}
	return files, nil
}
