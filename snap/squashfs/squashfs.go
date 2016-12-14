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
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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
	if osutil.GetenvBool("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS") {
		if err := s.Unpack("*", mountDir); err != nil {
			return err
		}
	}

	// nothing to do, happens on e.g. first-boot when we already
	// booted with the OS snap but its also in the seed.yaml
	if s.path == targetPath || osutil.FilesAreEqual(s.path, targetPath) {
		return nil
	}

	// try to (hard)link the file, but go on to trying to copy it
	// if it fails for whatever reason
	//
	// link(2) returns EPERM on filesystems that don't support
	// hard links (like vfat), so checking the error here doesn't
	// make sense vs just trying to copy it.
	if err := os.Link(s.path, targetPath); err == nil {
		return nil
	}

	return osutil.CopyFile(s.path, targetPath, osutil.CopyFlagPreserveAll|osutil.CopyFlagSync)
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

func (s *Snap) Unpack(src, dstDir string) error {
	return runCommand("unsquashfs", "-f", "-i", "-d", dstDir, s.path, src)
}

// Size returns the size of a squashfs snap.
func (s *Snap) Size() (size int64, err error) {
	st, err := os.Stat(s.path)
	if err != nil {
		return 0, err
	}

	return st.Size(), nil
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
