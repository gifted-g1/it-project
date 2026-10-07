package facedetect

import (
	"os"
	"path/filepath"
)

func resolveCascadePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}

	if _, err := os.Stat(path); err == nil {
		return path
	}

	if wd, err := os.Getwd(); err == nil {
		for dir := wd; ; {
			candidate := filepath.Join(dir, path)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	return path
}

func readCascade(path string) ([]byte, error) {
	resolvedPath := resolveCascadePath(path)
	return os.ReadFile(resolvedPath)
}
