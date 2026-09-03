package evidence

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEvidencePackageUsesOnlyTheStandardLibrary(t *testing.T) {
	allowed := map[string]bool{
		"bytes": true, "crypto/sha256": true, "encoding/binary": true,
		"encoding/hex": true, "encoding/json": true, "encoding/xml": true,
		"errors": true, "io": true, "strconv": true, "strings": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil || !allowed[path] {
				t.Fatalf("%s gained non-static import %q", entry.Name(), path)
			}
		}
	}
	for _, forbidden := range []string{
		`"context"`, `"net"`, `"net/http"`, `"os"`, `"os/exec"`, `"reflect"`, `"runtime"`,
		`"syscall"`, `"unsafe"`, "pkg/httpcheck", "internal/scenariosuite", "internal/application",
		"internal/origin", "internal/runner", "exec.Command", "Getenv",
	} {
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			contents, err := os.ReadFile(entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(contents), forbidden) {
				t.Fatalf("%s contains forbidden runtime marker %q", entry.Name(), forbidden)
			}
		}
	}
}
