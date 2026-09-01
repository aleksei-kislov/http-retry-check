package localfs

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/evidence"
)

const stagingLeaf = ".http-retry-check-artifact-stage"

type stagedArtifact struct {
	root     *os.Root
	identity os.FileInfo
	files    []createdFile
	created  bool
}

// WriteArtifact writes a complete artifact atomically and never replaces an
// existing path.
func WriteArtifact(files []evidence.ArtifactFile, destination string) (err error) {
	if evidence.ValidateArtifact(files) != nil {
		return ErrInternalFailure
	}
	walk, walkErr := openTargetWalk(destination)
	if walkErr != nil {
		return ErrTargetUnavailable
	}
	defer func() {
		if closeErr := walk.close(); closeErr != nil {
			err = ErrInternalFailure
		}
	}()
	finalAbsent, finalAbsenceErr := walk.absent(walk.leaf)
	stageAbsent, stageAbsenceErr := walk.absent(stagingLeaf)
	if !namespaceDistinct(walk.leaf, stagingLeaf) || finalAbsenceErr != nil || !finalAbsent ||
		stageAbsenceErr != nil || !stageAbsent || !walk.stable() {
		return ErrTargetUnavailable
	}
	parentHandle, handleErr := walk.parent.Open(".")
	if handleErr != nil {
		return ErrTargetUnavailable
	}
	defer func() {
		if closeErr := parentHandle.Close(); closeErr != nil {
			err = ErrInternalFailure
		}
	}()
	openedParent, openedErr := parentHandle.Stat()
	rootParent, rootErr := walk.parent.Stat(".")
	if openedErr != nil || rootErr != nil || !stableDirectory(rootParent, openedParent) ||
		!atomicNoReplaceSupported(parentHandle, walk.leaf) || syncDirectory(walk.parent) != nil {
		return ErrTargetUnavailable
	}
	stage := stagedArtifact{}
	published := false
	defer func() {
		if !published && stage.created {
			if cleanupStagedArtifact(walk, &stage) != nil {
				err = ErrInternalFailure
			}
		}
		if stage.root != nil {
			if closeErr := stage.root.Close(); closeErr != nil {
				err = ErrInternalFailure
			}
		}
	}()
	if createErr := walk.parent.Mkdir(stagingLeaf, 0o755); createErr != nil {
		if errors.Is(createErr, fs.ErrExist) {
			return ErrTargetUnavailable
		}
		return ErrInternalFailure
	}
	stage.created = true
	stage.identity, err = walk.parent.Lstat(stagingLeaf)
	if err != nil || stage.identity.Mode()&os.ModeSymlink != 0 || !stage.identity.IsDir() {
		return ErrInternalFailure
	}
	stage.root, err = walk.parent.OpenRoot(stagingLeaf)
	if err != nil || !stableStaging(walk, &stage) {
		return ErrInternalFailure
	}
	// This is the sole mutation-following target refusal: an alias or a racing
	// final object is accepted as exit two only after the still-empty stage is
	// removed with its exact identity intact.
	finalAbsent, finalAbsenceErr = walk.absent(walk.leaf)
	if finalAbsenceErr != nil {
		return ErrInternalFailure
	}
	if !finalAbsent || !namespaceDistinct(walk.leaf, stagingLeaf) {
		if cleanupStagedArtifact(walk, &stage) != nil {
			return ErrInternalFailure
		}
		return ErrTargetUnavailable
	}
	for _, file := range files {
		if writeErr := createExactFile(stage.root, &stage.files, file.Name, file.Contents); writeErr != nil {
			return ErrInternalFailure
		}
	}
	readBack, readErr := readArtifactRoot(stage.root)
	finalAbsent, finalAbsenceErr = walk.absent(walk.leaf)
	if readErr != nil || evidence.ValidateArtifact(readBack) != nil || !stableStaging(walk, &stage) ||
		finalAbsenceErr != nil || !finalAbsent || !walk.stable() || syncDirectory(stage.root) != nil {
		return ErrInternalFailure
	}
	if renameErr := atomicRenameNoReplace(parentHandle, stagingLeaf, walk.leaf); renameErr != nil {
		return ErrInternalFailure
	}
	published = true
	stage.created = false
	if syncDirectory(walk.parent) != nil || !walk.stable() {
		return ErrInternalFailure
	}
	finalIdentity, inspectErr := walk.parent.Lstat(walk.leaf)
	if inspectErr != nil || !stableDirectory(stage.identity, finalIdentity) {
		return ErrInternalFailure
	}
	finalRoot, openErr := walk.parent.OpenRoot(walk.leaf)
	if openErr != nil {
		return ErrInternalFailure
	}
	finalOpened, finalStatErr := finalRoot.Stat(".")
	finalReadBack, finalReadErr := readArtifactRoot(finalRoot)
	currentFinal, currentErr := walk.parent.Lstat(walk.leaf)
	finalCloseErr := finalRoot.Close()
	if finalStatErr != nil || finalReadErr != nil || currentErr != nil || finalCloseErr != nil ||
		!stableDirectory(stage.identity, finalOpened) || !stableDirectory(stage.identity, currentFinal) ||
		evidence.ValidateArtifact(finalReadBack) != nil {
		return ErrInternalFailure
	}
	return nil
}

func readArtifactRoot(root *os.Root) ([]evidence.ArtifactFile, error) {
	inventory, err := artifactInventory(root)
	if err != nil {
		return nil, err
	}
	files := make([]evidence.ArtifactFile, len(artifactFileSpecs))
	for index, specification := range artifactFileSpecs {
		contents, readErr := readArtifactChild(root, specification.name, inventory[index], evidence.MaxProjectionBytes)
		if readErr != nil {
			return nil, readErr
		}
		files[index] = evidence.ArtifactFile{
			Name: specification.name, MediaType: specification.mediaType, Contents: contents,
		}
	}
	post, err := artifactInventory(root)
	if err != nil || !sameInventory(inventory, post) {
		return nil, ErrInternalFailure
	}
	return files, nil
}

func stableStaging(walk *targetWalk, stage *stagedArtifact) bool {
	if stage.root == nil || stage.identity == nil {
		return false
	}
	opened, openedErr := stage.root.Stat(".")
	current, currentErr := walk.parent.Lstat(stagingLeaf)
	return openedErr == nil && currentErr == nil && stableDirectory(stage.identity, opened) &&
		stableDirectory(stage.identity, current) && stableCreatedFiles(stage.root, stage.files)
}

func cleanupStagedArtifact(walk *targetWalk, stage *stagedArtifact) error {
	if !stableStaging(walk, stage) || !exactCreatedInventory(stage.root, stage.files) {
		return errors.New("staged artifact identity changed")
	}
	for index := len(stage.files) - 1; index >= 0; index-- {
		file := stage.files[index]
		current, err := stage.root.Lstat(file.name)
		if err != nil || !stableRegular(file.identity, current) || stage.root.Remove(file.name) != nil {
			return errors.New("staged artifact file changed")
		}
	}
	if closeErr := stage.root.Close(); closeErr != nil {
		return closeErr
	}
	stage.root = nil
	current, err := walk.parent.Lstat(stagingLeaf)
	if err != nil || !stableDirectory(stage.identity, current) {
		return errors.New("staged artifact directory changed")
	}
	if removeErr := walk.parent.Remove(stagingLeaf); removeErr != nil {
		return removeErr
	}
	stage.created = false
	return nil
}

func namespaceDistinct(finalLeaf, stageLeaf string) bool {
	if finalLeaf == "" || strings.EqualFold(finalLeaf, stageLeaf) {
		return false
	}
	for _, character := range finalLeaf {
		// Restrict the proof to a portable ASCII namespace. This makes exact,
		// case-folded, canonical-normalized, trailing-dot and trailing-space
		// aliases of the fixed ASCII stage name impossible before mutation.
		if character < 0x21 || character > 0x7e || character == '/' || character == '\\' {
			return false
		}
	}
	return finalLeaf[len(finalLeaf)-1] != '.'
}
