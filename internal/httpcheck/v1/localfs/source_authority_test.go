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
		"rename_darwin.go":            "60542e96d8c4f6cdd2f597e070fc4b8cfd6793b599a25d215b492ea9def75c9c",
		"rename_darwin.s":             "9c66c5e6393d92b96a8fe59f676ce8b61500e28d7f3e4f887f99e628d250dedc",
		"rename_linux.go":             "afa18c5f662152035ddeed6d19dd429581794db9c0d415797758f430299ce9f0",
		"rename_linux_amd64.go":       "f3787ea7a5550b0f86def393ead502c7e1e09f633aa74934bf4a9cb252fadde5",
		"rename_linux_arm64.go":       "1e876b016463a65933edbcd9ddeeba098998396dfda05f991ef04bd1940c041d",
		"rename_linux_unsupported.go": "5e7adee25a656efbde87163e06d25bd1b61bb4a3247b70b1fd42a309d8173fda",
		"rename_other.go":             "1fe9087559be90aa73ab10ca09008a5f5c045cf903a6c54131703d4a65ff5a75",
		"rename_windows.go":           "450413b3f0a81d469228f4ffba9567a5b2465e39523e8d9d2a94111110feabc8",
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
			"errors.Is": true, "runtime.KeepAlive": true,
			"syscall.BytePtrFromString": true, "unsafe.Pointer": true,
		},
		"rename_linux.go": {
			"errors.Is": true, "runtime.KeepAlive": true, "syscall.BytePtrFromString": true,
			"syscall.Syscall6": true, "unsafe.Pointer": true,
		},
		"rename_linux_amd64.go":       {},
		"rename_linux_arm64.go":       {},
		"rename_linux_unsupported.go": {"errors.New": true},
		"rename_other.go":             {"errors.New": true},
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
	jumps := make([]string, 0, 2)
	for _, line := range strings.Split(source, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "JMP" {
			continue
		}
		if len(fields) != 2 {
			t.Fatalf("Darwin assembly gained malformed jump %q", line)
		}
		jumps = append(jumps, fields[1])
	}
	if len(jumps) != 2 || jumps[0] != "libc_renameatx_np(SB)" || jumps[1] != "syscall·syscall6(SB)" {
		t.Fatalf("Darwin assembly jump targets = %q, want only libc_renameatx_np(SB) and syscall·syscall6(SB)", jumps)
	}
	if strings.Count(source, "TEXT ·syscallSyscall6(SB),NOSPLIT,$0-80") != 1 {
		t.Fatal("Darwin assembly does not have the two exact ABI0 forwarding jumps")
	}
	for _, forbidden := range []string{"\n\tCALL\t", "\n\tBL\t", "\n\tSVC\t", "SYSCALL"} {
		if strings.Contains(source, forbidden) {
			t.Fatalf("Darwin assembly gained forbidden call marker %q", forbidden)
		}
	}
	darwinSource, err := os.ReadFile("rename_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"go:linkname", "fgetattrlist", "SYS_FGETATTRLIST", "syscall.Syscall6", "unsafe.Sizeof"} {
		if strings.Contains(string(darwinSource), forbidden) {
			t.Fatalf("Darwin source retained forbidden mechanism %q", forbidden)
		}
	}
	if strings.Count(string(darwinSource), "//go:uintptrescapes\nfunc syscallSyscall6(") != 1 {
		t.Fatal("Darwin syscall6 ABI0 declaration lost uintptr escape authority")
	}
}

func TestArtifactProbeFollowsStableOpenEmptyOwnedStage(t *testing.T) {
	contents, err := os.ReadFile("export.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	markers := []string{
		"walk.parent.Mkdir(stagingLeaf, 0o755)",
		"stage.root, err = walk.parent.OpenRoot(stagingLeaf)",
		"!stableStaging(walk, &stage) || !exactCreatedInventory(stage.root, stage.files) || !walk.stable()",
		"renameNoReplace(parentHandle, stagingLeaf, stagingLeaf)",
		"for _, file := range files",
	}
	last := -1
	for _, marker := range markers {
		index := strings.Index(source, marker)
		if index <= last {
			t.Fatalf("artifact probe authority marker %q is absent or out of order", marker)
		}
		last = index
	}
	if !strings.Contains(source, "return writeArtifact(files, destination, atomicRenameNoReplace)") {
		t.Fatal("public artifact writer bypasses the narrow rename seam")
	}
}

func TestUnixReadFlagsPreventFollowingAndControllingTerminal(t *testing.T) {
	contents, err := os.ReadFile("open_unix.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(contents)
	const flags = "syscall.O_NONBLOCK | syscall.O_NOFOLLOW | syscall.O_NOCTTY"
	if strings.Count(source, flags) != 1 ||
		!strings.Contains(source, "os.OpenFile(path, os.O_RDONLY|safeReadFlags, 0)") ||
		!strings.Contains(source, "return safeReadFlags") {
		t.Fatal("Unix source reads do not use the exact nonblocking no-follow no-controlling-terminal flags")
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
