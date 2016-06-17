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

package squashfs

import (
	"crypto"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"path"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// Magic is the magic prefix of squashfs snap files.
var Magic = []byte{'h', 's', 'q', 's'}

// Snap is the squashfs based snap.
type Snap struct {
	path string
}

// Path returns the path of the backing file.
func (s *Snap) Path() string {
	return s.path
}

// New returns a new Squashfs snap.
func New(snapPath string) *Snap {
	return &Snap{path: snapPath}
}

// Install just copies the blob into place (unless it is used in the tests)
func (s *Snap) Install(targetPath, mountDir string) error {

	// ensure mount-point and blob target dir.
	for _, dir := range []string{mountDir, filepath.Dir(targetPath)} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// This is required so that the tests can simulate a mounted
	// snap when we "install" a squashfs snap in the tests.
	// We can not mount it for real in the tests, so we just unpack
	// it to the location which is good enough for the tests.
	if os.Getenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS") != "" {
		if err := s.unpack("*", mountDir); err != nil {
			return err
		}
	}

	// FIXME: cp.CopyFile() has no preserve attribute flag yet
	return runCommand("cp", "-a", s.path, targetPath)
}

var runCommandWithOutput = func(args ...string) ([]byte, error) {
	cmd := exec.Command(args[0], args[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("cmd: %q failed: %v (%q)", strings.Join(args, " "), err, output)
	}

	return output, nil
}

var runCommand = func(args ...string) error {
	_, err := runCommandWithOutput(args...)
	return err
}

func (s *Snap) unpack(src, dstDir string) error {
	return runCommand("unsquashfs", "-f", "-i", "-d", dstDir, s.path, src)
}

// ReadFile returns the content of a single file inside a squashfs snap.
func (s *Snap) ReadFile(filePath string) (content []byte, err error) {
	tmpdir, err := ioutil.TempDir("", "read-file")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpdir)

	unpackDir := filepath.Join(tmpdir, "unpack")
	if err := runCommand("unsquashfs", "-i", "-d", unpackDir, s.path, filePath); err != nil {
		return nil, err
	}

	return ioutil.ReadFile(filepath.Join(unpackDir, filePath))
}

// ListDir returns the content of a single directory inside a squashfs snap.
func (s *Snap) ListDir(dirPath string) ([]string, error) {
	output, err := runCommandWithOutput(
		"unsquashfs", "-no-progress", "-dest", "_", "-l", s.path, dirPath)
	if err != nil {
		return nil, err
	}

	prefixPath := path.Join("_", dirPath)
	pattern, err := regexp.Compile("(?m)^" + regexp.QuoteMeta(prefixPath) + "/([^/\r\n]+)$")
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot compile squashfs list dir regexp for %q: %s", dirPath, err)
	}

	var directoryContents []string
	for _, groups := range pattern.FindAllSubmatch(output, -1) {
		if len(groups) > 1 {
			directoryContents = append(directoryContents, string(groups[1]))
		}
	}

	return directoryContents, nil
}

const (
	hashDigestBufSize = 2 * 1024 * 1024
)

// HashDigest computes a hash digest of the snap file using the given hash.
// It also returns its size.
func (s *Snap) HashDigest(hash crypto.Hash) (uint64, []byte, error) {
	f, err := os.Open(s.path)
	if err != nil {
		return 0, nil, err
	}
	defer f.Close()
	h := hash.New()
	size, err := io.CopyBuffer(h, f, make([]byte, hashDigestBufSize))
	if err != nil {
		return 0, nil, err
	}
	return uint64(size), h.Sum(nil), nil
}

// Build builds the snap.
func (s *Snap) Build(buildDir string) error {
	fullSnapPath, err := filepath.Abs(s.path)
	if err != nil {
		return err
	}

	return osutil.ChDir(buildDir, func() error {
		return runCommand(
			"mksquashfs",
			".", fullSnapPath,
			"-noappend",
			"-comp", "xz",
			"-no-xattrs",
		)
	})
}
