package localfs

import (
	"crypto/sha256"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFilesystemPackageUsesApprovedOperationsAndIsolatesUnsafeCode(t *testing.T) {
	allowed := map[string]bool{
		"errors": true, "io": true, "io/fs": true, "os": true,
		"path/filepath": true, "runtime": true, "strings": true, "syscall": true, "unsafe": true,
		"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/evidence": true,
		"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/scaffold": true,
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	unsafeFiles := 0
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
				t.Fatalf("%s gained import %q", entry.Name(), path)
			}
			if path == "unsafe" {
				unsafeFiles++
				if entry.Name() != "rename_darwin.go" && entry.Name() != "rename_linux.go" {
					t.Fatalf("unsafe escaped platform rename shim into %s", entry.Name())
				}
			}
		}
		contents, err := os.ReadFile(entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{
			"net/http", "os/exec", "exec.Command", "Getenv", "LookupEnv", "application.",
			"pkg/httpcheck", "internal/scenariosuite", ".RemoveAll(", ".MkdirAll(", ".Chmod(", "filepath.Walk(",
		} {
			if strings.Contains(string(contents), forbidden) {
				t.Fatalf("%s contains forbidden authority marker %q", entry.Name(), forbidden)
			}
		}
	}
	if unsafeFiles != 2 {
		t.Fatalf("unsafe platform files = %d, want 2 source-locked alternatives", unsafeFiles)
	}
}

func TestAtomicNoReplacePlatformSourcesMatchExpectedImplementations(t *testing.T) {
	want := map[string]string{
		"rename_darwin.go":            "f8c9d8fd72005337c11c18d7635e886a05a9225bfe92c5b7487bc38a266ea85d",
		"rename_darwin.s":             "a82cdf1b71ed124df0b7717333bfb9623e4db4bc72b0af786c10d4249105ed39",
		"rename_linux.go":             "222c2b08e5b8d18270741a503296c814be7be00de027b7cd52e3b3b58c1b775e",
		"rename_linux_amd64.go":       "f3787ea7a5550b0f86def393ead502c7e1e09f633aa74934bf4a9cb252fadde5",
		"rename_linux_arm64.go":       "1e876b016463a65933edbcd9ddeeba098998396dfda05f991ef04bd1940c041d",
		"rename_linux_unsupported.go": "d258d41dd0652db7cfe6a59780d9fecac0728a97fe144f85ee5d7ec382d93c3b",
		"rename_windows.go":           "2ae0a0c5d20995692bc4476c15349ff2ac064d245ef4afe1be23e0d728ad556e",
	}
	for name, expected := range want {
		contents, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		if got := hex.EncodeToString(digest[:]); got != expected {
			t.Fatalf("atomic no-replace platform source %s changed: %s", name, got)
		}
	}
}

func TestAtomicNoReplaceUsesOnlyApprovedPlatformCalls(t *testing.T) {
	allowed := map[string]map[string]bool{
		"rename_darwin.go": {
			"runtime.KeepAlive": true, "syscall.BytePtrFromString": true,
			"syscall.Syscall6": true, "unsafe.Pointer": true, "unsafe.Sizeof": true,
		},
		"rename_linux.go": {
			"runtime.KeepAlive": true, "syscall.BytePtrFromString": true,
			"syscall.Syscall6": true, "unsafe.Pointer": true,
		},
		"rename_linux_amd64.go":       {},
		"rename_linux_arm64.go":       {},
		"rename_linux_unsupported.go": {"errors.New": true},
		"rename_windows.go":           {"errors.New": true},
	}
	for name, allowedCalls := range allowed {
		parsed, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		importNames := make(map[string]bool)
		for _, imported := range parsed.Imports {
			path, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			importName := filepath.Base(path)
			if imported.Name != nil {
				importName = imported.Name.Name
			}
			importNames[importName] = true
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			owner, ok := selector.X.(*ast.Ident)
			if !ok || !importNames[owner.Name] {
				return true
			}
			qualified := owner.Name + "." + selector.Sel.Name
			if !allowedCalls[qualified] {
				t.Errorf("%s gained package call %s", name, qualified)
			}
			return true
		})
	}
	assembly, err := os.ReadFile("rename_darwin.s")
	if err != nil {
		t.Fatal(err)
	}
	source := string(assembly)
	if strings.Count(source, "JMP\tlibc_renameatx_np(SB)") != 1 || strings.Count(source, "JMP\t") != 1 {
		t.Fatal("Darwin assembly does not have the one allowed libc rename jump")
	}
	for _, forbidden := range []string{"\n\tCALL\t", "\n\tBL\t", "\n\tSVC\t", "SYSCALL"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Darwin assembly gained forbidden call marker %q", forbidden)
		}
	}
}

func TestCreatedFileIdentityIsRegisteredBeforeFallibleCompletion(t *testing.T) {
	contents, err := os.ReadFile("scaffold.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	registered := strings.Index(source, "*createdFiles = append(*createdFiles, createdFile{name: name, identity: identity})")
	if registered < 0 {
		t.Fatal("created-file identity registration is absent")
	}
	for _, later := range []string{
		"identity.Mode().IsRegular()", "file.Write(contents)", "file.Sync()", "file.Close()",
	} {
		if index := strings.Index(source[registered:], later); index <= 0 {
			t.Errorf("fallible completion %q is not after identity registration", later)
		}
	}
	for _, caller := range []string{
		"createExactFile(destinationRoot, &createdFiles, asset.Name, asset.Contents)",
		"createExactFile(stage.root, &stage.files, file.Name, file.Contents)",
	} {
		found := false
		for _, name := range []string{"scaffold.go", "export.go"} {
			candidate, readErr := os.ReadFile(name)
			if readErr != nil {
				t.Fatal(readErr)
			}
			found = found || strings.Contains(string(candidate), caller)
		}
		if !found {
			t.Errorf("created-file registry pointer is absent from caller %q", caller)
		}
	}
}
