// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"text/template"
	"time"

	"github.com/ubuntu-core/snappy/logger"
)

var goarch = runtime.GOARCH

func init() {
	// golang does not init Seed() itself
	rand.Seed(time.Now().UTC().UnixNano())
}

// ChDir runs runs "f" inside the given directory
func ChDir(newDir string, f func() error) (err error) {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if err := os.Chdir(newDir); err != nil {
		return err
	}
	defer os.Chdir(cwd)
	return f()
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

// IsDevice checks if the given os.FileMode coresponds to a device (char/block)
func IsDevice(mode os.FileMode) bool {
	return (mode & (os.ModeDevice | os.ModeCharDevice)) != 0
}

// UnpackTarTransformFunc can be used to change the names during unpack
// or to return a error for files that are not acceptable
type UnpackTarTransformFunc func(path string) (newPath string, err error)

var mknod = syscall.Mknod

// UnpackTar unpacks the given tar file into the target directory
func UnpackTar(r io.Reader, targetDir string, fn UnpackTarTransformFunc) error {
	// ensure we we extract with the original permissions
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

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
			// tar is also doing this!
			if FileExists(path) {
				if err := os.Remove(path); err != nil {
					return err
				}
			}
			if err := os.Symlink(hdr.Linkname, path); err != nil {
				return err
			}
		case IsDevice(mode):
			switch {
			case (mode & os.ModeCharDevice) != 0:
				mode |= syscall.S_IFCHR
			case (mode & os.ModeDevice) != 0:
				mode |= syscall.S_IFBLK
			}
			devNum := Makedev(uint32(hdr.Devmajor), uint32(hdr.Devminor))
			err = mknod(path, uint32(mode), int(devNum))
			if err != nil {
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

// IsSupportedArchitecture returns true if the system architecture is in the
// list of architectures.
func IsSupportedArchitecture(architectures []string) bool {
	systemArch := UbuntuArchitecture()

	for _, arch := range architectures {
		if arch == "all" || arch == systemArch {
			return true
		}
	}

	return false
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

const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYabcdefghijklmnopqrstuvwxy"

// MakeRandomString returns a random string of length length
var MakeRandomString = func(length int) string {

	out := ""
	for i := 0; i < length; i++ {
		out += string(letters[rand.Intn(len(letters))])
	}

	return out
}

// NewSideloadVersion returns a version number such that later calls
// should return versions that compare larger.
func NewSideloadVersion() string {
	n := time.Now().UTC().UnixNano()
	bs := make([]byte, 12)
	for i := 11; i >= 0; i-- {
		bs[i] = letters[n&31]
		n = n >> 5
	}

	return string(bs)
}

// AtomicWriteFlags are a bitfield of flags for AtomicWriteFile
type AtomicWriteFlags uint

const (
	// AtomicWriteFollow makes AtomicWriteFile follows symlinks
	AtomicWriteFollow AtomicWriteFlags = 1 << iota
)

// AtomicWriteFile updates the filename atomically and works otherwise
// like io/ioutil.WriteFile()
//
// Note that it won't follow symlinks and will replace existing symlinks
// with the real file
func AtomicWriteFile(filename string, data []byte, perm os.FileMode, flags AtomicWriteFlags) (err error) {
	if flags&AtomicWriteFollow != 0 {
		if fn, err := os.Readlink(filename); err == nil || (fn != "" && os.IsNotExist(err)) {
			if filepath.IsAbs(fn) {
				filename = fn
			} else {
				filename = filepath.Join(filepath.Dir(filename), fn)
			}
		}
	}
	tmp := filename + "." + MakeRandomString(12)

	// XXX: if go switches to use aio_fsync, we need to open the dir for writing
	dir, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer dir.Close()

	fd, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, perm)
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

// MajorMinor returns the major/minor number of the given os.FileInfo
func MajorMinor(info os.FileInfo) (uint32, uint32, error) {
	if !IsDevice(info.Mode()) {
		return 0, 0, fmt.Errorf("No device %s", info.Name())
	}

	unixStat, ok := info.Sys().(*syscall.Stat_t)
	if ok {
		// see glibc: sysdeps/unix/sysv/linux/makedev.c
		dev := unixStat.Rdev
		major := uint32((dev>>8)&0xfff) | (uint32(dev>>32) & ^uint32(0xfff))
		minor := uint32(dev&0xff) | (uint32(dev>>12) & ^uint32(0xff))

		return major, minor, nil
	}

	return 0, 0, fmt.Errorf("failed to get syscall.stat_t for %v", info.Name())
}

// Makedev implements makedev(3)
func Makedev(major, minor uint32) uint32 {
	return uint32((minor & 0xff) | ((major & 0xfff) << 8))
}

// Getattr get the attribute of the given name
func Getattr(i interface{}, name string) interface{} {
	v := reflect.ValueOf(i)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	return v.FieldByName(name).Interface()
}

func fillSnapEnvVars(desc interface{}, vars []string) []string {
	for i, v := range vars {
		var templateOut bytes.Buffer
		t := template.Must(template.New("wrapper").Parse(v))
		if err := t.Execute(&templateOut, desc); err != nil {
			// this can never happen, except we forget a variable
			logger.Panicf("Unable to execute template: %v", err)
		}
		vars[i] = templateOut.String()
	}
	return vars
}

// GetBasicSnapEnvVars returns the app-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetBasicSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"TMPDIR=/tmp/snaps/{{.UdevAppName}}/{{.Version}}/tmp",
		"TEMPDIR=/tmp/snaps/{{.UdevAppName}}/{{.Version}}/tmp",
		"SNAP_APP_PATH={{.AppPath}}",
		"SNAP_APP_DATA_PATH=/var/lib{{.AppPath}}",
		"SNAP_APP_TMPDIR=/tmp/snaps/{{.UdevAppName}}/{{.Version}}/tmp",
		"SNAP_NAME={{.AppName}}",
		"SNAP_VERSION={{.Version}}",
		"SNAP_ORIGIN={{.Origin}}",
		"SNAP_FULLNAME={{.UdevAppName}}",
		"SNAP_ARCH={{.AppArch}}",
	})
}

// GetUserSnapEnvVars returns the user-level environment variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetUserSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAP_APP_USER_DATA_PATH={{.Home}}{{.AppPath}}",
	})
}

// GetDeprecatedBasicSnapEnvVars returns the app-level deprecated environment
// variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetDeprecatedBasicSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAPP_APP_PATH={{.AppPath}}",
		"SNAPP_APP_DATA_PATH=/var/lib{{.AppPath}}",
		"SNAPP_APP_TMPDIR=/tmp/snaps/{{.UdevAppName}}/{{.Version}}/tmp",
		"SNAPPY_APP_ARCH={{.AppArch}}",
	})
}

// GetDeprecatedUserSnapEnvVars returns the user-level deprecated environment
// variables for a snap.
// Despite this being a bit snap-specific, this is in helpers.go because it's
// used by so many other modules, we run into circular dependencies if it's
// somewhere more reasonable like the snappy module.
func GetDeprecatedUserSnapEnvVars(desc interface{}) []string {
	return fillSnapEnvVars(desc, []string{
		"SNAPP_APP_USER_DATA_PATH={{.Home}}{{.AppPath}}",
	})
}

// RSyncWithDelete syncs srcDir to destDir
func RSyncWithDelete(srcDirName, destDirName string) error {
	// first remove everything thats not in srcdir
	err := filepath.Walk(destDirName, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// relative to the root "destDirName"
		relPath := path[len(destDirName):]
		if !FileExists(filepath.Join(srcDirName, relPath)) {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			if info.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// then copy or update the data from srcdir to destdir
	err = filepath.Walk(srcDirName, func(src string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// relative to the root "srcDirName"
		relPath := src[len(srcDirName):]
		dst := filepath.Join(destDirName, relPath)
		if info.IsDir() {
			if err := os.MkdirAll(dst, info.Mode()); err != nil {
				return err
			}

			// this can panic. The alternative would be to use the "st, ok" pattern, and then if !ok... panic?
			st := info.Sys().(*syscall.Stat_t)
			ts := []syscall.Timespec{st.Atim, st.Mtim}

			return syscall.UtimesNano(dst, ts)
		}
		if !FilesAreEqual(src, dst) {
			// XXX: we should (eventually) use CopyFile here,
			//      but we need to teach it about preserving
			//      of atime/mtime and permissions
			output, err := exec.Command("cp", "-va", src, dst).CombinedOutput()
			if err != nil {
				return fmt.Errorf("Failed to copy %s to %s (%s)", src, dst, output)
			}
		}
		return nil
	})

	return err
}

// CopyIfDifferent copies src to dst only if dst is different that src
func CopyIfDifferent(src, dst string) error {
	if !FilesAreEqual(src, dst) {
		if err := CopyFile(src, dst, CopyFlagSync|CopyFlagOverwrite); err != nil {
			return err
		}
	}

	return nil
}
