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

package snapfs

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"

	"launchpad.net/snappy/helpers"
)

// Snap is the squashfs based snap
type Snap struct {
	path string
}

// Name returns the Name of the backing file
func (s *Snap) Name() string {
	return filepath.Base(s.path)
}

// New returns a new Snapfs snap
func New(path string) *Snap {
	return &Snap{path: path}
}

// UnpackMeta unpacks just the meta/* directory of the given snap
func (s *Snap) UnpackMeta(dst string) error {
	if err := s.Unpack("meta/*", dst); err != nil {
		return err
	}

	return s.Unpack(".click/*", dst)
}

// Unpack unpacks the src (which may be a glob into the given target dir
func (s *Snap) Unpack(src, dstDir string) error {
	cmd := exec.Command("unsquashfs", "-f", "-i", "-d", dstDir, s.path, src)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("unpack failed: %v (%v)", err, output)
	}

	return nil
}

// ReadFile returns the content of a single file inside a snapfs snap
func (s *Snap) ReadFile(path string) (content []byte, err error) {
	tmpdir, err := ioutil.TempDir("", "read-file")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpdir)

	unpackDir := filepath.Join(tmpdir, "unpack")
	cmd := exec.Command("unsquashfs", "-i", "-d", unpackDir, s.path, path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("ReadFile %q failed: %v (%v)", path, err, output)
	}

	return ioutil.ReadFile(filepath.Join(unpackDir, path))
}

// CopyBlob copies the snap to a new place
func (s *Snap) CopyBlob(targetFile string) error {
	// FIXME: helpers.CopyFile() has no preserve attribute flag yet
	cmd := exec.Command("cp", "-a", s.path, targetFile)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cp %q %q failed: %v (%v)", s.path, targetFile, err, output)
	}

	return nil
}

// Verify verifies the snap
func (s *Snap) Verify(unauthOk bool) error {
	// FIMXE: there is no verification yet for snapfs packages, this
	//        will be done via assertions later for now we rely on
	//        the https security
	return nil
}

// Build builds the snap
func (s *Snap) Build(buildDir string) error {
	fullSnapPath, err := filepath.Abs(s.path)
	if err != nil {
		return err
	}

	return helpers.ChDir(buildDir, func() error {
		cmd := exec.Command(
			"mksquashfs",
			".", fullSnapPath,
			"-all-root",
			"-noappend",
			"-comp", "xz")
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("mksquashfs failed: %v (%v)", err, output)
		}
		return nil
	})
}
