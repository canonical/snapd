/*
 * Copyright (C) 2014-2015 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package helpers

import (
	"archive/tar"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

var goarch = runtime.GOARCH

func init() {
	// golang does not init Seed() itself
	rand.Seed(time.Now().UTC().UnixNano())
}

// ChDir runs runs "f" inside the given directory
func ChDir(newDir string, f func()) (err error) {
	cwd, err := os.Getwd()
	os.Chdir(newDir)
	defer os.Chdir(cwd)
	if err != nil {
		return err
	}
	f()
	return err
}

// ExitCode extract the exit code from the error of a failed cmd.Run() or the
// original error if its not a exec.ExitError
func ExitCode(runErr error) (e int, err error) {
	// golang, you are kidding me, right?
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		waitStatus := exitErr.Sys().(syscall.WaitStatus)
		e = waitStatus.ExitStatus()
		return e, nil
	}
	return e, runErr
}

// TarIterFunc is called for each file inside a tar archive
type TarIterFunc func(r *tar.Reader, hdr *tar.Header) error

// TarIterate will take a io.Reader and call the fn callback on each tar
// archive member
func TarIterate(r io.Reader, fn TarIterFunc) error {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			// end of tar archive
			break
		}
		if err != nil {
			return err
		}

		if err := fn(tr, hdr); err != nil {
			return err
		}
	}

	return nil
}

// IsSymlink checks whether the given os.FileMode corresponds to a symlink
func IsSymlink(mode os.FileMode) bool {
	return (mode & os.ModeSymlink) == os.ModeSymlink
}

// UnpackTarTransformFunc can be used to change the names during unpack
// or to return a error for files that are not acceptable
type UnpackTarTransformFunc func(path string) (newPath string, err error)

// UnpackTar unpacks the given tar file into the target directory
func UnpackTar(r io.Reader, targetDir string, fn UnpackTarTransformFunc) error {
	return TarIterate(r, func(tr *tar.Reader, hdr *tar.Header) (err error) {
		// run tar transform func
		name := hdr.Name
		if fn != nil {
			name, err = fn(hdr.Name)
			if err != nil {
				return err
			}
		}

		path := filepath.Join(targetDir, name)
		mode := hdr.FileInfo().Mode()
		if err := os.MkdirAll(filepath.Dir(path), 0777); err != nil {
			return err
		}

		switch {
		case mode.IsDir():
			err := os.Mkdir(path, mode)
			if err != nil {
				return nil
			}
		case IsSymlink(mode):
			if err := os.Symlink(hdr.Linkname, path); err != nil {
				return err
			}
		case mode.IsRegular():
			out, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, mode)
			if err != nil {
				return err
			}
			defer out.Close()
			_, err = io.Copy(out, tr)
			if err != nil {
				return err
			}
		default:
			return &ErrUnsupportedFileType{path, mode}
		}

		return nil
	})
}

// ErrUnsupportedFileType is returned when trying to extract a file
// that is not a regular file, a directory, or a symlink.
type ErrUnsupportedFileType struct {
	Name string
	Mode os.FileMode
}

func (e ErrUnsupportedFileType) Error() string {
	return fmt.Sprintf("%s: unsupported filetype %s", e.Name, e.Mode)
}

// UbuntuArchitecture returns the debian equivalent architecture for the
// currently running architecture.
//
// If the architecture does not map any debian architecture, the
// GOARCH is returned.
func UbuntuArchitecture() string {
	switch goarch {
	case "386":
		return "i386"
	case "arm":
		return "armhf"
	default:
		return goarch
	}
}

// EnsureDir ensures that the given directory exists and if
// not creates it with the given permissions.
func EnsureDir(dir string, perm os.FileMode) (err error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if err := os.MkdirAll(dir, perm); err != nil {
			return err
		}
	}
	return nil
}

// Sha512sum returns the sha512 of the given file as a hexdigest
func Sha512sum(infile string) (hexdigest string, err error) {
	r, err := os.Open(infile)
	if err != nil {
		return "", err
	}
	defer r.Close()

	hasher := sha512.New()
	if _, err := io.Copy(hasher, r); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// MakeMapFromEnvList takes a string list of the form "key=value"
// and returns a map[string]string from that list
// This is useful for os.Environ() manipulation
func MakeMapFromEnvList(env []string) map[string]string {
	envMap := map[string]string{}
	for _, l := range env {
		split := strings.SplitN(l, "=", 2)
		if len(split) != 2 {
			return nil
		}
		envMap[split[0]] = split[1]
	}
	return envMap
}

// FileExists return true if given path can be stat()ed by us. Note that
// it may return false on e.g. permission issues.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDirectory return true if the given path can be stat()ed by us and
// is a directory. Note that it may return false on e.g. permission issues.
func IsDirectory(path string) bool {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return false
	}

	return fileInfo.IsDir()
}

// MakeRandomString returns a random string of length length
func MakeRandomString(length int) string {
	var letters = "abcdefghijklmnopqrstuvwxyABCDEFGHIJKLMNOPQRSTUVWXY"

	out := ""
	for i := 0; i < length; i++ {
		out += string(letters[rand.Intn(len(letters))])
	}

	return out
}

// AtomicWriteFile updates the filename atomically and works otherwise
// exactly like io/ioutil.WriteFile()
func AtomicWriteFile(filename string, data []byte, perm os.FileMode) (err error) {
	tmp := filename + ".new"

	// XXX: if go switches to use aio_fsync, we need to open the dir for writing
	dir, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer dir.Close()

	fd, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	defer func() {
		e := fd.Close()
		if err == nil {
			err = e
		}
		if err != nil {
			os.Remove(tmp)
		}
	}()

	// according to the docs, Write returns a non-nil error when n !=
	// len(b), so don't worry about short writes.
	if _, err := fd.Write(data); err != nil {
		return err
	}

	if err := fd.Sync(); err != nil {
		return err
	}

	if err := os.Rename(tmp, filename); err != nil {
		return err
	}

	return dir.Sync()
}

// CurrentHomeDir returns the homedir of the current user. It looks at
// $HOME first and then at passwd
func CurrentHomeDir() (string, error) {
	home := os.Getenv("HOME")
	if home != "" {
		return home, nil
	}

	user, err := user.Current()
	if err != nil {
		return "", err
	}

	return user.HomeDir, nil
}

// ShouldDropPrivs returns true if the application runs with sufficient
// privileges so that it should drop them
func ShouldDropPrivs() bool {
	if groups, err := syscall.Getgroups(); err == nil {
		for _, gid := range groups {
			if gid == 0 {
				return true
			}
		}
	}

	return syscall.Getuid() == 0 || syscall.Getgid() == 0

}

func makeDirMap(dirName string) (map[string]struct{}, error) {
	dir, err := ioutil.ReadDir(dirName)
	if err != nil {
		return nil, err
	}

	dirMap := make(map[string]struct{})
	for _, dent := range dir {
		if dent.IsDir() {
			return nil, fmt.Errorf("subdirs are not supported")
		}
		dirMap[dent.Name()] = struct{}{}
	}

	return dirMap, nil
}

// RSyncWithDelete syncs srcDir to destDir
func RSyncWithDelete(srcDirName, destDirName string) error {

	// first remove everything thats not in srcdir
	err := filepath.Walk(destDirName, func(path string, info os.FileInfo, err error) error {
		if !FileExists(filepath.Join(srcDirName, path[len(srcDirName):])) {
			if err := os.RemoveAll(filepath.Join(path)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// then copy or update the data from srcdir to destdir
	err = filepath.Walk(srcDirName, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return os.MkdirAll(filepath.Join(destDirName, path[len(destDirName):]), info.Mode())
		}
		src := path
		dst := filepath.Join(destDirName, path[len(destDirName):])
		if !FilesAreEqual(src, dst) {
			// XXX: on snappy-trunk we can use CopyFile here
			output, err := exec.Command("cp", "-va", src, dst).CombinedOutput()
			if err != nil {
				fmt.Errorf("Failed to copy %s to %s (%s)", src, dst, output)
			}
		}
		return nil
	})

	return err
}
