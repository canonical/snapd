package partition

import (
	"os"
)

// Return nil if given path exists.
// FIXME: put into utils package
func FileExists(path string) (err error) {
	_, err = os.Stat(path)

	return err
}

func IsDirectory(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}

	return fileInfo.IsDir()
}
