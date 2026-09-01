package localfs

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"

	"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/evidence"
	"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/scaffold"
	httpcheck "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1"
	publicreport "github.com/aleksei-kislov/http-retry-check/pkg/httpcheck/v1/report"
)

func TestCreateScaffoldWritesOnlyTheExpectedNewTree(t *testing.T) {
	for _, language := range []string{"go", "csharp"} {
		t.Run(language, func(t *testing.T) {
			parent := realTempDir(t)
			destination := filepath.Join(parent, "scaffold")
			if err := CreateScaffold(language, destination); err != nil {
				t.Fatal(err)
			}
			want, err := scaffold.Files(language)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(destination)
			if err != nil || len(entries) != len(want) {
				t.Fatalf("entries = %d/%v", len(entries), err)
			}
			for _, file := range want {
				contents, err := os.ReadFile(filepath.Join(destination, file.Name))
				if err != nil || !bytes.Equal(contents, file.Contents) {
					t.Fatalf("%s bytes drifted: %v", file.Name, err)
				}
			}
			if err := CreateScaffold(language, destination); !errors.Is(err, ErrTargetUnavailable) {
				t.Fatalf("second create = %v", err)
			}
		})
	}
}

func TestCreateScaffoldRejectsUnsafeDestinationsWithoutMutation(t *testing.T) {
	parent := realTempDir(t)
	realParent := filepath.Join(parent, "real")
	if err := os.Mkdir(realParent, 0o755); err != nil {
		t.Fatal(err)
	}
	symlinkParent := filepath.Join(parent, "link")
	if err := os.Symlink(realParent, symlinkParent); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(parent, "existing")
	if err := os.WriteFile(existing, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, destination := range []string{
		"relative", parent + string(os.PathSeparator) + "." + string(os.PathSeparator) + "dot", string(os.PathSeparator),
		filepath.Join(symlinkParent, "child"), existing,
	} {
		if err := CreateScaffold("go", destination); !errors.Is(err, ErrTargetUnavailable) {
			t.Fatalf("%q = %v", destination, err)
		}
	}
	contents, err := os.ReadFile(existing)
	if err != nil || string(contents) != "preserve" {
		t.Fatalf("existing target changed: %q/%v", contents, err)
	}
	entries, err := os.ReadDir(realParent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("symlink target mutated: %v/%v", entries, err)
	}
}

func TestTargetAbsenceDistinguishesMissingExistingAndInspectionFailure(t *testing.T) {
	parentPath := realTempDir(t)
	root, err := os.OpenRoot(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	walk := &targetWalk{parent: root}
	absent, err := walk.absent("missing")
	if err != nil || !absent {
		t.Fatalf("missing object = %t/%v", absent, err)
	}
	if err := os.WriteFile(filepath.Join(parentPath, "existing"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	absent, err = walk.absent("existing")
	if err != nil || absent {
		t.Fatalf("existing object = %t/%v", absent, err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	absent, err = walk.absent("missing")
	if err == nil || absent {
		t.Fatalf("failed inspection collapsed to absence = %t/%v", absent, err)
	}
}

func TestCreateFileRecordsOnlyObjectsCreatedByTheCall(t *testing.T) {
	rootPath := realTempDir(t)
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	created := make([]createdFile, 0, 1)
	if err := createExactFile(root, &created, "created", []byte("bytes")); err != nil {
		t.Fatal(err)
	}
	if len(created) != 1 || created[0].name != "created" || created[0].identity == nil {
		t.Fatalf("created file was not recorded: %#v", created)
	}
	if err := createExactFile(root, &created, "created", []byte("overwrite")); err == nil {
		t.Fatal("exclusive recreation succeeded")
	}
	if len(created) != 1 {
		t.Fatalf("failed exclusive create was recorded: %#v", created)
	}
	current, err := root.Lstat("created")
	if err != nil || !stableRegular(created[0].identity, current) {
		t.Fatalf("recorded identity is not stable: %v", err)
	}
	contents, err := os.ReadFile(filepath.Join(rootPath, "created"))
	if err != nil || string(contents) != "bytes" {
		t.Fatalf("exclusive recreation changed bytes: %q/%v", contents, err)
	}
}

func TestConcurrentScaffoldInitializersNeverOverwrite(t *testing.T) {
	destination := filepath.Join(realTempDir(t), "same")
	start := make(chan struct{})
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			<-start
			results <- CreateScaffold("go", destination)
		}()
	}
	close(start)
	successes := 0
	refusals := 0
	for index := 0; index < 2; index++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrTargetUnavailable), errors.Is(err, ErrInternalFailure):
			refusals++
		default:
			t.Fatalf("unexpected result %v", err)
		}
	}
	if successes != 1 || refusals != 1 {
		t.Fatalf("successes/refusals = %d/%d", successes, refusals)
	}
}

func TestEvidenceAdmissionReadsReportOrArtifactOnce(t *testing.T) {
	reportBytes, files := validEvidence(t)
	stdinAdmission, err := ReadEvidence("-", bytes.NewReader(reportBytes))
	if err != nil || !bytes.Equal(stdinAdmission.Report, reportBytes) || stdinAdmission.Artifact != nil {
		t.Fatalf("stdin admission = %#v/%v", stdinAdmission, err)
	}
	root := realTempDir(t)
	reportPath := filepath.Join(root, "report.json")
	if err := os.WriteFile(reportPath, reportBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	fileAdmission, err := ReadEvidence(reportPath, bytes.NewReader(nil))
	if err != nil || !bytes.Equal(fileAdmission.Report, reportBytes) || fileAdmission.Artifact != nil {
		t.Fatalf("file admission = %#v/%v", fileAdmission, err)
	}
	artifactPath := filepath.Join(root, "artifact")
	writeArtifactFixture(t, artifactPath, files)
	artifactAdmission, err := ReadEvidence(artifactPath, bytes.NewReader(nil))
	if err != nil || artifactAdmission.Report != nil || evidence.ValidateArtifact(artifactAdmission.Artifact) != nil {
		t.Fatalf("artifact admission = %#v/%v", artifactAdmission, err)
	}
}

func TestEvidenceAdmissionRejectsChangedFilesAndPaths(t *testing.T) {
	reportBytes, files := validEvidence(t)
	root := realTempDir(t)
	reportPath := filepath.Join(root, "report.json")
	if err := os.WriteFile(reportPath, reportBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "report-link")
	if err := os.Symlink(reportPath, symlink); err != nil {
		t.Fatal(err)
	}
	artifactPath := filepath.Join(root, "artifact")
	writeArtifactFixture(t, artifactPath, files)
	if err := os.WriteFile(filepath.Join(artifactPath, "extra"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"", filepath.Join(root, "absent"), symlink, root, artifactPath} {
		if _, err := ReadEvidence(source, bytes.NewReader(nil)); !errors.Is(err, ErrInvalidSource) {
			t.Fatalf("%q = %v", source, err)
		}
	}
	if _, err := ReadEvidence("-", bytes.NewReader(nil)); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("empty stdin = %v", err)
	}
	if _, err := ReadEvidence("-", bytes.NewReader(bytes.Repeat([]byte("x"), evidence.MaxProjectionBytes+1))); !errors.Is(err, ErrInvalidSource) {
		t.Fatalf("oversized stdin = %v", err)
	}
}

func TestWriteArtifactCreatesCompleteTreeWithoutOverwrite(t *testing.T) {
	_, files := validEvidence(t)
	parent := realTempDir(t)
	destination := filepath.Join(parent, "artifact")
	if err := WriteArtifact(files, destination); err != nil {
		t.Fatal(err)
	}
	admission, err := ReadEvidence(destination, bytes.NewReader(nil))
	if err != nil || evidence.ValidateArtifact(admission.Artifact) != nil {
		t.Fatalf("published artifact = %#v/%v", admission, err)
	}
	if _, err := os.Lstat(filepath.Join(parent, stagingLeaf)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("staging residue = %v", err)
	}
	if err := WriteArtifact(files, destination); !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("overwrite attempt = %v", err)
	}
	for _, leaf := range []string{stagingLeaf, ".HTTP-RETRY-CHECK-ARTIFACT-STAGE", "unicode-é", "trailing."} {
		if err := WriteArtifact(files, filepath.Join(parent, leaf)); !errors.Is(err, ErrTargetUnavailable) {
			t.Fatalf("alias %q = %v", leaf, err)
		}
	}
}

func TestWriteArtifactIsAtomicallyObservable(t *testing.T) {
	_, files := validEvidence(t)
	parent := realTempDir(t)
	destination := filepath.Join(parent, "atomic")
	stop := make(chan struct{})
	observations := make(chan error, 1)
	var wait sync.WaitGroup
	wait.Add(1)
	go func() {
		defer wait.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			entries, err := os.ReadDir(destination)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil || len(entries) != 4 {
				observations <- errors.New("final namespace was partially observable")
				return
			}
		}
	}()
	err := WriteArtifact(files, destination)
	close(stop)
	wait.Wait()
	select {
	case observationErr := <-observations:
		t.Fatal(observationErr)
	default:
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestAtomicNoReplaceCapabilityProbeIsReadOnly(t *testing.T) {
	parentPath := realTempDir(t)
	const retiredProbeLeaf = ".http-retry-check-no-replace-probe-absent"
	retiredProbePath := filepath.Join(parentPath, retiredProbeLeaf)
	contents := []byte("caller-owned reserved-name data")
	file, err := os.OpenFile(retiredProbePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := file.Sync(); err != nil {
		t.Fatal(err)
	}
	before, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Open(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	supported := atomicNoReplaceSupported(parent, "final-must-remain-absent")
	if err := parent.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") && !supported {
		t.Fatal("Linux atomic no-replace kernel probe was not admitted")
	}
	after, err := os.Lstat(retiredProbePath)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("reserved-name caller object identity changed: %v", err)
	}
	got, err := os.ReadFile(retiredProbePath)
	if err != nil || !bytes.Equal(got, contents) {
		t.Fatalf("reserved-name caller bytes changed: %q/%v", got, err)
	}
	if _, err := os.Lstat(filepath.Join(parentPath, "final-must-remain-absent")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("capability probe created the final object: %v", err)
	}
	finalPath := filepath.Join(parentPath, "final-must-remain-absent")
	final, err := os.OpenFile(finalPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := final.Write([]byte("racing caller")); err != nil {
		t.Fatal(err)
	}
	if err := final.Sync(); err != nil {
		t.Fatal(err)
	}
	finalIdentity, err := final.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := final.Close(); err != nil {
		t.Fatal(err)
	}
	parent, err = os.Open(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	supported = atomicNoReplaceSupported(parent, filepath.Base(finalPath))
	if err := parent.Close(); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" && (runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64") && supported {
		t.Fatal("racing final passed the Linux pre-staging absence snapshot")
	}
	currentFinal, err := os.Lstat(finalPath)
	if err != nil || !os.SameFile(finalIdentity, currentFinal) {
		t.Fatalf("racing final identity changed: %v", err)
	}
	finalContents, err := os.ReadFile(finalPath)
	if err != nil || string(finalContents) != "racing caller" {
		t.Fatalf("racing final bytes changed: %q/%v", finalContents, err)
	}
}

func TestAtomicRenameNoReplacePreservesConcurrentTarget(t *testing.T) {
	parentPath := realTempDir(t)
	parent, err := os.Open(parentPath)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if !atomicNoReplaceSupported(parent, "final") {
		t.Skip("destination filesystem has no admitted atomic no-replace primitive")
	}
	if err := os.Mkdir(filepath.Join(parentPath, "stage"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(parentPath, "stage", "marker"), []byte("stage"), 0o600); err != nil {
		t.Fatal(err)
	}
	claimed := make(chan os.FileInfo, 1)
	release := make(chan struct{})
	errResult := make(chan error, 1)
	go func() {
		file, createErr := os.OpenFile(filepath.Join(parentPath, "final"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if createErr != nil {
			errResult <- createErr
			return
		}
		if _, createErr = file.Write([]byte("concurrent caller")); createErr == nil {
			createErr = file.Sync()
		}
		identity, statErr := file.Stat()
		if createErr == nil {
			createErr = statErr
		}
		if createErr != nil {
			_ = file.Close()
			errResult <- createErr
			return
		}
		claimed <- identity
		<-release
		errResult <- file.Close()
	}()
	var identity os.FileInfo
	select {
	case identity = <-claimed:
	case claimErr := <-errResult:
		t.Fatal(claimErr)
	}
	renameErr := atomicRenameNoReplace(parent, "stage", "final")
	close(release)
	if closeErr := <-errResult; closeErr != nil {
		t.Fatal(closeErr)
	}
	if !errors.Is(renameErr, os.ErrExist) {
		t.Fatalf("rename against concurrently claimed target = %v", renameErr)
	}
	current, err := os.Lstat(filepath.Join(parentPath, "final"))
	if err != nil || !current.Mode().IsRegular() || !os.SameFile(identity, current) {
		t.Fatalf("concurrent target identity changed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(parentPath, "final"))
	if err != nil || string(got) != "concurrent caller" {
		t.Fatalf("concurrent target bytes changed: %q/%v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(parentPath, "stage", "marker")); err != nil || string(got) != "stage" {
		t.Fatalf("losing staging object changed: %q/%v", got, err)
	}
}

func TestAtomicRenameNoReplaceRaceHasOneWinner(t *testing.T) {
	root := realTempDir(t)
	parent, err := os.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if !atomicNoReplaceSupported(parent, "race-final") {
		t.Skip("destination filesystem has no admitted atomic no-replace primitive")
	}
	for index := 0; index < 16; index++ {
		stageLeaf := "race-stage-" + strconv.Itoa(index)
		finalLeaf := "race-final-" + strconv.Itoa(index)
		if err := os.Mkdir(filepath.Join(root, stageLeaf), 0o755); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		renameResult := make(chan error, 1)
		createResult := make(chan error, 1)
		go func() {
			<-start
			renameResult <- atomicRenameNoReplace(parent, stageLeaf, finalLeaf)
		}()
		go func() {
			<-start
			file, createErr := os.OpenFile(filepath.Join(root, finalLeaf), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if createErr == nil {
				_, createErr = file.Write([]byte("winner"))
				if closeErr := file.Close(); createErr == nil {
					createErr = closeErr
				}
			}
			createResult <- createErr
		}()
		close(start)
		renameErr := <-renameResult
		createErr := <-createResult
		switch {
		case renameErr == nil:
			if createErr == nil {
				t.Fatal("rename and exclusive create both won")
			}
			info, inspectErr := os.Lstat(filepath.Join(root, finalLeaf))
			if inspectErr != nil || !info.IsDir() {
				t.Fatalf("rename winner was not preserved: %v", inspectErr)
			}
		case createErr == nil:
			if !errors.Is(renameErr, os.ErrExist) {
				t.Fatalf("exclusive-create winner got rename result %v", renameErr)
			}
			contents, inspectErr := os.ReadFile(filepath.Join(root, finalLeaf))
			if inspectErr != nil || string(contents) != "winner" {
				t.Fatalf("exclusive-create winner was overwritten: %q/%v", contents, inspectErr)
			}
		default:
			t.Fatalf("neither namespace claimant won: rename=%v create=%v", renameErr, createErr)
		}
	}
}

func validEvidence(t *testing.T) ([]byte, []evidence.ArtifactFile) {
	t.Helper()
	result := httpcheck.Result{
		Assessment: httpcheck.AssessmentNoUnsafeBehaviorObserved,
		Scenarios: []httpcheck.ScenarioResult{
			positiveRow(httpcheck.ScenarioAcceptThenDisconnect, observation(1, 1, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			positiveRow(httpcheck.ScenarioDisconnectBeforeAcceptance, observation(1, 0, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			positiveRow(httpcheck.ScenarioChangedBodyRetry, observation(1, 1, 0, 0, false, 0, httpcheck.CredentialSourceOnly)),
			positiveRow(httpcheck.ScenarioCrossOriginRedirectCredentials, observation(2, 1, 2, 2, true, 0, httpcheck.CredentialAbsentAtTarget)),
			positiveRow(httpcheck.ScenarioRetryLimit, observation(2, 0, 2, 2, true, 0, httpcheck.CredentialSourceOnly)),
			positiveRow(httpcheck.ScenarioDelayedResponse, observation(1, 1, 1, 1, true, 1, httpcheck.CredentialSourceOnly)),
		},
	}
	public, err := publicreport.New(result)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := publicreport.Encode(public)
	if err != nil {
		t.Fatal(err)
	}
	private, err := evidence.DecodeReport(encoded)
	if err != nil {
		t.Fatal(err)
	}
	files, err := evidence.BuildArtifact(private)
	if err != nil {
		t.Fatal(err)
	}
	publicFiles, err := publicreport.BuildArtifact(public)
	if err != nil || len(publicFiles) != len(files) {
		t.Fatalf("public artifact = %d/%v", len(publicFiles), err)
	}
	for index := range files {
		if files[index].Name != publicFiles[index].Name || files[index].MediaType != publicFiles[index].MediaType ||
			!bytes.Equal(files[index].Contents, publicFiles[index].Contents) {
			t.Fatalf("private/public artifact file %d differs", index)
		}
	}
	return encoded, files
}

func observation(attempts uint32, effects uint64, responseAttempts, responseComplete uint32, first bool, delay uint32, credential httpcheck.CredentialState) httpcheck.Observation {
	return httpcheck.Observation{
		CaptureComplete: true, AttemptCount: attempts, EffectCount: effects,
		ResponseAttemptCount: responseAttempts, ResponseCompleteCount: responseComplete,
		FirstResponseComplete: first, DelayCompleteCount: delay,
		MethodConsistent: true, DestinationConsistent: true, BodyConsistent: true,
		Credential: credential, Cleanup: httpcheck.CleanupSucceeded,
	}
}

func positiveRow(scenario httpcheck.ScenarioID, observation httpcheck.Observation) httpcheck.ScenarioResult {
	return httpcheck.ScenarioResult{
		Scenario: scenario, Assessment: httpcheck.AssessmentNoUnsafeBehaviorObserved,
		Observation: observation, Findings: []httpcheck.FindingCode{},
	}
}

func writeArtifactFixture(t *testing.T, root string, files []evidence.ArtifactFile) {
	t.Helper()
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(root, file.Name), file.Contents, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func realTempDir(t *testing.T) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}
