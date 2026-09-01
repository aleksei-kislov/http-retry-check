package localfs

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type directoryBinding struct {
	parent   *os.Root
	name     string
	root     *os.Root
	identity os.FileInfo
}

type targetWalk struct {
	rootPath     string
	rootIdentity os.FileInfo
	roots        []*os.Root
	bindings     []directoryBinding
	parent       *os.Root
	leaf         string
}

func openTargetWalk(destination string) (*targetWalk, error) {
	if destination == "" || !filepath.IsAbs(destination) || filepath.Clean(destination) != destination {
		return nil, ErrTargetUnavailable
	}
	volume := filepath.VolumeName(destination)
	rootPath := volume + string(os.PathSeparator)
	relative, err := filepath.Rel(rootPath, destination)
	if err != nil || relative == "." || filepath.IsAbs(relative) {
		return nil, ErrTargetUnavailable
	}
	components := strings.Split(relative, string(os.PathSeparator))
	if len(components) == 0 {
		return nil, ErrTargetUnavailable
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, ErrTargetUnavailable
		}
	}
	rootIdentity, err := os.Lstat(rootPath)
	if err != nil || rootIdentity.Mode()&os.ModeSymlink != 0 || !rootIdentity.IsDir() {
		return nil, ErrTargetUnavailable
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, ErrTargetUnavailable
	}
	walk := &targetWalk{
		rootPath: rootPath, rootIdentity: rootIdentity, roots: []*os.Root{root},
		parent: root, leaf: components[len(components)-1],
	}
	openedRoot, openedErr := root.Stat(".")
	if openedErr != nil || !stableDirectory(rootIdentity, openedRoot) {
		_ = walk.close()
		return nil, ErrTargetUnavailable
	}
	for _, component := range components[:len(components)-1] {
		before, inspectErr := walk.parent.Lstat(component)
		if inspectErr != nil || before.Mode()&os.ModeSymlink != 0 || !before.IsDir() {
			_ = walk.close()
			return nil, ErrTargetUnavailable
		}
		child, openErr := walk.parent.OpenRoot(component)
		if openErr != nil {
			_ = walk.close()
			return nil, ErrTargetUnavailable
		}
		opened, statErr := child.Stat(".")
		current, currentErr := walk.parent.Lstat(component)
		if statErr != nil || currentErr != nil || !stableDirectory(before, opened) ||
			!stableDirectory(before, current) {
			_ = child.Close()
			_ = walk.close()
			return nil, ErrTargetUnavailable
		}
		walk.bindings = append(walk.bindings, directoryBinding{
			parent: walk.parent, name: component, root: child, identity: before,
		})
		walk.roots = append(walk.roots, child)
		walk.parent = child
	}
	if !walk.stable() {
		_ = walk.close()
		return nil, ErrTargetUnavailable
	}
	return walk, nil
}

func (walk *targetWalk) stable() bool {
	openedRoot, openedErr := walk.roots[0].Stat(".")
	currentRoot, currentErr := os.Lstat(walk.rootPath)
	if openedErr != nil || currentErr != nil || !stableDirectory(walk.rootIdentity, openedRoot) ||
		!stableDirectory(walk.rootIdentity, currentRoot) {
		return false
	}
	for _, binding := range walk.bindings {
		opened, openedErr := binding.root.Stat(".")
		current, currentErr := binding.parent.Lstat(binding.name)
		if openedErr != nil || currentErr != nil || !stableDirectory(binding.identity, opened) ||
			!stableDirectory(binding.identity, current) {
			return false
		}
	}
	return true
}

func (walk *targetWalk) absent(name string) (bool, error) {
	_, err := walk.parent.Lstat(name)
	if err == nil {
		return false, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	return false, err
}

func (walk *targetWalk) close() error {
	var closeErr error
	for index := len(walk.roots) - 1; index >= 0; index-- {
		if err := walk.roots[index].Close(); err != nil {
			closeErr = err
		}
	}
	walk.roots = nil
	return closeErr
}

func syncDirectory(root *os.Root) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}
