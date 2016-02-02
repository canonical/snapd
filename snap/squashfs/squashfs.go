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
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/snap"
)

func init() {
	snap.RegisterFormat([]byte{'h', 's', 'q', 's'}, func(path string) (snap.File, error) {
		return New(path), nil
	})
}

// BlobPath is a helper that calculates the blob path from the baseDir.
// FIXME: feels wrong (both location and approach). need something better.
func BlobPath(instDir string) string {
	l := strings.Split(filepath.Clean(instDir), string(filepath.Separator))
	if len(l) < 2 {
		panic(fmt.Sprintf("invalid path for BlobPath: %q", instDir))
	}

	return filepath.Join(dirs.SnapBlobDir, fmt.Sprintf("%s_%s.snap", l[len(l)-2], l[len(l)-1]))
}

// Snap is the squashfs based snap.
type Snap struct {
	path string
}

// Name returns the Name of the backing file.
func (s *Snap) Name() string {
	return filepath.Base(s.path)
}

// New returns a new Squashfs snap.
func New(path string) *Snap {
	return &Snap{path: path}
}

// MetaMember extracts from meta/. - COMPAT
func (s *Snap) MetaMember(metaMember string) ([]byte, error) {
	return s.ReadFile(filepath.Join("meta", metaMember))
}

// Install just copies the blob into place (unless it is used in the tests)
func (s *Snap) Install(instDir string) error {

	// ensure mount-point and blob dir.
	for _, dir := range []string{instDir, dirs.SnapBlobDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}

	// FIXME: HHAAAAAAAAAAAAAAAACKKKKKKKKKKKKK for the tests
	if os.Getenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS") != "" {
		if err := s.Unpack("*", instDir); err != nil {
			return err
		}
	}

	// FIXME: helpers.CopyFile() has no preserve attribute flag yet
	return runCommand("cp", "-a", s.path, BlobPath(instDir))
}

var runCommand = func(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("cmd: %q failed: %v (%q)", strings.Join(args, " "), err, output)
	}
	return nil
}

// Unpack unpacks the src (which may be a glob) into the given target dir.
func (s *Snap) Unpack(src, dstDir string) error {
	return runCommand("unsquashfs", "-f", "-i", "-d", dstDir, s.path, src)
}

// ReadFile returns the content of a single file inside a squashfs snap.
func (s *Snap) ReadFile(path string) (content []byte, err error) {
	tmpdir, err := ioutil.TempDir("", "read-file")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpdir)

	unpackDir := filepath.Join(tmpdir, "unpack")
	if err := runCommand("unsquashfs", "-i", "-d", unpackDir, s.path, path); err != nil {
		return nil, err
	}

	return ioutil.ReadFile(filepath.Join(unpackDir, path))
}

const (
	hashDigestBufSize = 2 * 1024 * 1024
)

// Info returns information like name, type etc about the package
func (s *Snap) Info() (*snap.Info, error) {
	snapYaml, err := s.ReadFile("meta/snap.yaml")
	if err != nil {
		return nil, fmt.Errorf("info failed for %s: %s", s.path, err)
	}

	return snap.InfoFromSnapYaml(snapYaml)
}

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

	return helpers.ChDir(buildDir, func() error {
		return runCommand(
			"mksquashfs",
			".", fullSnapPath,
			"-noappend",
			"-comp", "xz")
	})
}
