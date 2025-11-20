package fs

import (
	"os"
	"path/filepath"
)

type FileInfo struct {
	Path    string
	Size    int64
	IsDir   bool
}

func WalkDirectory(root string, fn func(FileInfo) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		fileInfo := FileInfo{
			Path:  path,
			Size:  info.Size(),
			IsDir: info.IsDir(),
		}

		return fn(fileInfo)
	})
}

func GetFileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

