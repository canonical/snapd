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
	tmpdir, err := ioutil.TempDir("", "unpack-file")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpdir)

	cmd := exec.Command("unsquashfs", "-f", "-i", "-d", dstDir, s.path, src)
	//cmd.Stdout = os.Stdout
	//cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("unpack failed: %v", err)
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
	//cmd.Stdout = os.Stdout
	//cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ReadFile %s failed: %v", path, err)
	}

	return ioutil.ReadFile(filepath.Join(unpackDir, path))
}

// CopyBlob copies the snap to a new place
func (s *Snap) CopyBlob(targetFile string) error {
	cmd := exec.Command("cp", "-a", s.path, targetFile)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cp %s %s failed: %v", s.path, targetFile, err)
	}

	return nil
}

// Verify verifies the snap
func (s *Snap) Verify(unauthOk bool) error {
	// FIMXE: meh, meh, bä
	return nil
}

// Build builds the snap
func (s *Snap) Build(buildDir string) error {
	var err error
	fullSnapPath, err := filepath.Abs(s.path)
	if err != nil {
		return err
	}
	helpers.ChDir(buildDir, func() {
		cmd := exec.Command(
			"mksquashfs",
			".", fullSnapPath,
			"-all-root",
			"-noappend",
			"-comp", "xz")
		//cmd.Stdout = os.Stdout
		//cmd.Stderr = os.Stderr
		if aerr := cmd.Run(); aerr != nil {
			err = fmt.Errorf("mksquashfs failed: %v", aerr)
		}
	})

	return err
}
