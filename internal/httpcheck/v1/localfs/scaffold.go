package localfs

import (
	"errors"
	"io"
	"io/fs"
	"os"

	"github.com/aleksei-kislov/http-retry-check/internal/httpcheck/v1/scaffold"
)

type createdFile struct {
	name     string
	identity os.FileInfo
}

// CreateScaffold writes the selected scaffold to a new directory.
func CreateScaffold(language, destination string) (err error) {
	assets, assetErr := scaffold.Files(language)
	if assetErr != nil {
		return ErrTargetUnavailable
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
	absent, absenceErr := walk.absent(walk.leaf)
	if absenceErr != nil || !absent || !walk.stable() {
		return ErrTargetUnavailable
	}
	if createErr := walk.parent.Mkdir(walk.leaf, 0o755); createErr != nil {
		if errors.Is(createErr, fs.ErrExist) {
			return ErrTargetUnavailable
		}
		return ErrInternalFailure
	}
	createdDirectory := true
	committed := false
	var destinationRoot *os.Root
	var destinationIdentity os.FileInfo
	createdFiles := make([]createdFile, 0, len(assets))
	defer func() {
		if !committed && createdDirectory {
			if cleanupScaffold(walk, &destinationRoot, destinationIdentity, createdFiles) != nil {
				err = ErrInternalFailure
			}
		}
		if destinationRoot != nil {
			if closeErr := destinationRoot.Close(); closeErr != nil {
				err = ErrInternalFailure
			}
		}
	}()
	destinationIdentity, err = walk.parent.Lstat(walk.leaf)
	if err != nil || destinationIdentity.Mode()&os.ModeSymlink != 0 || !destinationIdentity.IsDir() {
		return ErrInternalFailure
	}
	destinationRoot, err = walk.parent.OpenRoot(walk.leaf)
	if err != nil {
		return ErrInternalFailure
	}
	opened, openedErr := destinationRoot.Stat(".")
	current, currentErr := walk.parent.Lstat(walk.leaf)
	if openedErr != nil || currentErr != nil || !stableDirectory(destinationIdentity, opened) ||
		!stableDirectory(destinationIdentity, current) {
		return ErrInternalFailure
	}
	for _, asset := range assets {
		if writeErr := createExactFile(destinationRoot, &createdFiles, asset.Name, asset.Contents); writeErr != nil {
			return ErrInternalFailure
		}
	}
	if !stableCreatedDirectory(walk, destinationRoot, destinationIdentity) ||
		!stableCreatedFiles(destinationRoot, createdFiles) || !exactCreatedInventory(destinationRoot, createdFiles) || !walk.stable() ||
		syncDirectory(destinationRoot) != nil || syncDirectory(walk.parent) != nil {
		return ErrInternalFailure
	}
	if !stableCreatedDirectory(walk, destinationRoot, destinationIdentity) ||
		!stableCreatedFiles(destinationRoot, createdFiles) || !exactCreatedInventory(destinationRoot, createdFiles) || !walk.stable() {
		return ErrInternalFailure
	}
	committed = true
	return nil
}

func createExactFile(root *os.Root, createdFiles *[]createdFile, name string, contents []byte) error {
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	identity, statErr := file.Stat()
	if statErr != nil || identity == nil {
		_ = file.Close()
		if statErr == nil {
			return errors.New("created file identity is unavailable")
		}
		return statErr
	}
	*createdFiles = append(*createdFiles, createdFile{name: name, identity: identity})
	var writeErr error
	if !identity.Mode().IsRegular() {
		writeErr = errors.New("created object is not a regular file")
	}
	if writeErr == nil {
		written, err := file.Write(contents)
		if err != nil {
			writeErr = err
		} else if written != len(contents) {
			writeErr = io.ErrShortWrite
		}
	}
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func stableCreatedDirectory(walk *targetWalk, root *os.Root, identity os.FileInfo) bool {
	if root == nil || identity == nil {
		return false
	}
	opened, openedErr := root.Stat(".")
	current, currentErr := walk.parent.Lstat(walk.leaf)
	return openedErr == nil && currentErr == nil && stableDirectory(identity, opened) &&
		stableDirectory(identity, current)
}

func stableCreatedFiles(root *os.Root, files []createdFile) bool {
	for _, file := range files {
		current, err := root.Lstat(file.name)
		if err != nil || !stableRegular(file.identity, current) {
			return false
		}
	}
	return true
}

func exactCreatedInventory(root *os.Root, files []createdFile) bool {
	directory, err := root.Open(".")
	if err != nil {
		return false
	}
	entries, readErr := directory.ReadDir(-1)
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil || len(entries) != len(files) {
		return false
	}
	want := make(map[string]bool, len(files))
	for _, file := range files {
		want[file.name] = true
	}
	for _, entry := range entries {
		if !want[entry.Name()] {
			return false
		}
	}
	return true
}

func cleanupScaffold(walk *targetWalk, rootPointer **os.Root, identity os.FileInfo, files []createdFile) error {
	root := *rootPointer
	if root == nil || identity == nil || !stableCreatedDirectory(walk, root, identity) ||
		!stableCreatedFiles(root, files) || !exactCreatedInventory(root, files) {
		return errors.New("created scaffold identity changed")
	}
	for index := len(files) - 1; index >= 0; index-- {
		file := files[index]
		current, err := root.Lstat(file.name)
		if err != nil || !stableRegular(file.identity, current) || root.Remove(file.name) != nil {
			return errors.New("created scaffold file changed")
		}
	}
	if closeErr := root.Close(); closeErr != nil {
		return closeErr
	}
	*rootPointer = nil
	current, err := walk.parent.Lstat(walk.leaf)
	if err != nil || !stableDirectory(identity, current) {
		return errors.New("created scaffold directory changed")
	}
	if removeErr := walk.parent.Remove(walk.leaf); removeErr != nil {
		return removeErr
	}
	return nil
}
