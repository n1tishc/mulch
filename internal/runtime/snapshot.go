package runtime

import (
	"os"
	"path/filepath"
)

func snapshotWorkdir(source string) (string, func(), error) {
	root, err := os.MkdirTemp("", "mulch-race-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { _ = os.RemoveAll(root) }
	err = filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(source, path)
		if relErr != nil {
			return relErr
		}
		if entry.IsDir() && entry.Name() == ".git" && relative != "." {
			return filepath.SkipDir
		}
		target := filepath.Join(root, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(target, contents, info.Mode().Perm())
	})
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	return root, cleanup, nil
}
