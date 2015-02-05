package snappy

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"gopkg.in/yaml.v1"
)

var (
	needRootError = errors.New("This command requires root access. " +
		"Please re-run using 'sudo'.")
)

var goarch = runtime.GOARCH

// helper to run "f" inside the given directory
func chDir(newDir string, f func()) (err error) {
	cwd, err := os.Getwd()
	os.Chdir(newDir)
	defer os.Chdir(cwd)
	if err != nil {
		return err
	}
	f()
	return err
}

// extract the exit code from the error of a failed cmd.Run() or the
// original error if its not a exec.ExitError
func exitCode(runErr error) (e int, err error) {
	// golang, you are kidding me, right?
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		waitStatus := exitErr.Sys().(syscall.WaitStatus)
		e = waitStatus.ExitStatus()
		return e, nil
	}
	return e, runErr
}

func unpackTar(archive string, target string) error {

	var f io.Reader
	var err error

	f, err = os.Open(archive)
	if err != nil {
		return err
	}

	if strings.HasSuffix(archive, ".gz") {
		f, err = gzip.NewReader(f)
		if err != nil {
			return err
		}
	}

	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			return err
		}
		path := filepath.Join(target, hdr.Name)
		info := hdr.FileInfo()
		if info.IsDir() {
			err := os.MkdirAll(path, info.Mode())
			if err != nil {
				return nil
			}
		} else {
			err := os.MkdirAll(filepath.Dir(path), 0777)
			out, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, info.Mode())
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, tr)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func getMapFromYaml(data []byte) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	err := yaml.Unmarshal(data, &m)
	if err != nil {
		return m, err
	}
	return m, nil
}

// Architecture returns the debian equivalent architecture for the
// currently running architecture.
//
// If the architecture does not map any debian architecture, the
// GOARCH is returned.
func Architecture() string {
	switch goarch {
	case "386":
		return "i386"
	case "arm":
		return "armhf"
	default:
		return goarch
	}
}
