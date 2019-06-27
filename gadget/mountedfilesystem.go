// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package gadget

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

// MountedFilesystemWriter assists in writing contents of a structure to a
// mounted filesystem.
type MountedFilesystemWriter struct {
	contentDir string
	ps         *PositionedStructure
}

// NewMountedFilesystemWriter returns a writer capable of deploying provided
// structure, with content of the structure stored in the given root directory.
func NewMountedFilesystemWriter(contentDir string, ps *PositionedStructure) (*MountedFilesystemWriter, error) {
	if ps == nil {
		return nil, fmt.Errorf("internal error: *PositionedStructure is nil")
	}
	if ps.IsBare() {
		return nil, fmt.Errorf("structure %v has no filesystem", ps)
	}
	if contentDir == "" {
		return nil, fmt.Errorf("internal error: gadget content directory cannot be unset")
	}
	fw := &MountedFilesystemWriter{
		contentDir: contentDir,
		ps:         ps,
	}
	return fw, nil
}

func prefixPreserve(dstDir string, preserve []string) []string {
	preserveInDst := make([]string, len(preserve))
	for i, p := range preserve {
		preserveInDst[i] = filepath.Join(dstDir, p)
	}
	sort.Strings(preserveInDst)

	return preserveInDst
}

// Write writes structure data into provided directory. All existing files are
// overwritten, unless their paths, relative to target directory, are listed in
// the preserve list. Permission bits and ownership of updated entries is not
// preserved.
func (m *MountedFilesystemWriter) Write(whereDir string, preserve []string) error {
	if whereDir == "" {
		return fmt.Errorf("internal error: destination directory cannot be unset")
	}
	preserveInDst := prefixPreserve(whereDir, preserve)
	for _, c := range m.ps.Content {
		if err := m.writeVolumeContent(whereDir, &c, preserveInDst); err != nil {
			return fmt.Errorf("cannot write filesystem content of %s: %v", c, err)
		}
	}
	return nil
}

// writeDirectory copies the source directory, or its contents under target
// location dst. Follows rsync like semantics, that is:
//   /foo/ -> /bar - deploys contents of foo under /bar
//   /foo  -> /bar - deploys foo and its subtree under /bar
func writeDirectory(src, dst string, preserveInDst []string) error {
	hasDirSourceSlash := strings.HasSuffix(src, "/")

	if !osutil.IsDirectory(src) {
		if hasDirSourceSlash {
			// make the error sufficiently descriptive
			return fmt.Errorf("cannot specify trailing / for a source which is not a directory")
		}
		return fmt.Errorf("source is not a directory")
	}

	if !hasDirSourceSlash {
		// /foo -> /bar (deploy foo and subtree)
		dst = filepath.Join(dst, filepath.Base(src))
	}

	fis, err := ioutil.ReadDir(src)
	if err != nil {
		return fmt.Errorf("cannot list directory entries: %v", err)
	}

	for _, fi := range fis {
		pSrc := filepath.Join(src, fi.Name())
		pDst := filepath.Join(dst, fi.Name())

		write := writeFile
		if fi.IsDir() {
			if err := os.MkdirAll(pDst, 0755); err != nil {
				return fmt.Errorf("cannot create directory prefix: %v", err)
			}

			write = writeDirectory
			pSrc += "/"
		}
		if err := write(pSrc, pDst, preserveInDst); err != nil {
			return err
		}
	}

	return nil
}

// writeFile copies the source file at given location or under given directory.
// Follows rsync like semantics, that is:
//   /foo -> /bar/ - deploys foo as /bar/foo
//   /foo  -> /bar - deploys foo as /bar
// The destination location is overwritten.
func writeFile(src, dst string, preserveInDst []string) error {
	if strings.HasSuffix(dst, "/") {
		// deploy to directory
		dst = filepath.Join(dst, filepath.Base(src))
	}

	if osutil.FileExists(dst) && strutil.SortedListContains(preserveInDst, dst) {
		// entry shall be preserved
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return fmt.Errorf("cannot create prefix directory: %v", err)
	}

	if osutil.IsSymlink(src) {
		to, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("cannot read symlink: %v", err)
		}
		if err := os.Symlink(to, dst); err != nil {
			return fmt.Errorf("cannot deploy a symlink: %v", err)
		}
		return nil
	}

	// overwrite & sync by default
	copyFlags := osutil.CopyFlagOverwrite | osutil.CopyFlagSync

	// TODO use osutil.AtomicFile
	// TODO try to preserve ownership and permission bits
	if err := osutil.CopyFile(src, dst, copyFlags); err != nil {
		return fmt.Errorf("cannot copy %s: %v", src, err)
	}
	return nil
}

func (m *MountedFilesystemWriter) writeVolumeContent(whereDir string, content *VolumeContent, preserveInDst []string) error {
	if content.Source == "" {
		return fmt.Errorf("internal error: source cannot be unset")
	}
	if content.Target == "" {
		return fmt.Errorf("internal error: target cannot be unset")
	}
	realSource := filepath.Join(m.contentDir, content.Source)
	realTarget := filepath.Join(whereDir, content.Target)

	// filepath trims the trailing /, restore if needed
	if strings.HasSuffix(content.Target, "/") {
		realTarget += "/"
	}
	if strings.HasSuffix(content.Source, "/") {
		realSource += "/"
	}

	if osutil.IsDirectory(realSource) || strings.HasSuffix(content.Source, "/") {
		// deploy a directory
		return writeDirectory(realSource, realTarget, preserveInDst)
	} else {
		// deploy a file
		return writeFile(realSource, realTarget, preserveInDst)
	}
}
