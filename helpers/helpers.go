package helpers

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha512"
	"encoding/hex"
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

	"errors"

	"gopkg.in/yaml.v2"
)

var goarch = runtime.GOARCH

// Returns name of lockfile created to serialise privileged operations
var lockfileName = func() string {
	return "/run/snappy.lock"
}

// FileLock is a Lock file object used to serialise access for
// privileged operations.
type FileLock struct {
	Filename string
	realFile *os.File
}

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
	return (err == nil)
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
func AtomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	tmp := filename + ".new"

	if err := ioutil.WriteFile(tmp, data, perm); err != nil {
		os.Remove(tmp)
		return err
	}

	return os.Rename(tmp, filename)
}

// Determine if caller is running as the superuser
var isRoot = func() bool {
	return syscall.Getuid() == 0
}

// StartPrivileged should be called when a privileged operation begins.
func StartPrivileged() (lock *FileLock, err error) {
	if !isRoot() {
		// FIXME: return ErrRequiresRoot
		return nil, errors.New("command requires sudo (root)")
	}

	lock = NewFileLock(lockfileName())

	if err = lock.Lock(); err != nil {
		// FIXME: return ErrPrivOpInProgress
		return nil, errors.New("privileged operation already in progress")
	}

	return lock, nil
}

// StopPrivileged should be called to flag the end of a privileged operation.
func StopPrivileged(lock *FileLock) (err error) {
	return lock.Unlock()
}

// NewFileLock creates a new lock object (but does not lock it).
func NewFileLock(path string) (lock *FileLock) {

	lock = new(FileLock)
	lock.Filename = path

	return lock
}

// Lock the FileLock object.
func (l *FileLock) Lock() (err error) {

	// XXX: don't try to create exclusively - we care if the file failed to
	// be created, but we don't care if it already existed as the lock _on_ the
	// file is the most important thing.
	flags := (os.O_CREATE | os.O_WRONLY)

	l.realFile, err = os.OpenFile(l.Filename, flags, 0600)
	if err != nil {
		return err
	}

	err = syscall.Flock(int(l.realFile.Fd()), syscall.LOCK_EX)
	if err != nil {
		return err
	}

	return nil
}

// Unlock the FileLock object.
func (l *FileLock) Unlock() (err error) {
	// unlink first
	if err = os.Remove(l.Filename); err != nil {
		return err
	}

	if err = l.realFile.Close(); err != nil {
		return err
	}

	return nil
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
