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

type MountedFilesystemWriter struct {
	rootDir string
	ps      *PositionedStructure
}

// NewMountedFilesystemWriter returns a writer capable of deploying provided
// structure, with content of the structure stored in the given root directory.
func NewMountedFilesystemWriter(rootDir string, ps *PositionedStructure) (*MountedFilesystemWriter, error) {
	if ps.IsBare() {
		return nil, fmt.Errorf("structure %v has no filesystem", ps)
	}
	fw := &MountedFilesystemWriter{
		rootDir: rootDir,
		ps:      ps,
	}
	return fw, nil
}

func remapPreserve(dstDir string, preserve []string) []string {
	preserveInDst := make([]string, len(preserve))
	for i, p := range preserve {
		preserveInDst[i] = filepath.Join(dstDir, p)
	}
	sort.Strings(preserveInDst)

	return preserveInDst
}

// Deploy deploys structure data into provided directory. All existing files are
// overwritten, unless their paths, relative to target directory, are listed in
// the preserve list.
func (m *MountedFilesystemWriter) Deploy(whereDir string, preserve []string) error {
	preserveInDst := remapPreserve(whereDir, preserve)
	for _, c := range m.ps.Content {
		if err := m.deployOneContent(whereDir, &c, preserveInDst); err != nil {
			return fmt.Errorf("cannot deploy filesystem content of %s: %v", c, err)
		}
	}
	return nil
}

// deployDirectory deploys the source directory, or its contents under target
// location dst. Follows rsync like semantics, that is:
//   /foo/ -> /bar - deploys contents of foo under /bar
//   /foo  -> /bar - deploys foo and its subtree under /bar
func deployDirectory(src, dst string, preserveInDst []string) error {
	fis, err := ioutil.ReadDir(src)
	if err != nil {
		return fmt.Errorf("cannot list directory entries: %v", err)
	}

	if !strings.HasSuffix(src, "/") {
		dst = filepath.Join(dst, filepath.Base(src))
	}

	for _, fi := range fis {
		fpSrc := filepath.Join(src, fi.Name())
		fpDst := filepath.Join(dst, fi.Name())

		deploy := deployFile
		if fi.IsDir() {
			if err := os.MkdirAll(fpDst, 0755); err != nil {
				return fmt.Errorf("cannot deploy directory prefix: %v", err)
			}

			deploy = deployDirectory
			fpSrc += "/"
		}
		if err := deploy(fpSrc, fpDst, preserveInDst); err != nil {
			return err
		}
	}

	return nil
}

// deployDirectory deploys the source file at given location or under given directory.
// Follows rsync like semantics, that is:
//   /foo -> /bar/ - deploys foo as /bar/foo
//   /foo  -> /bar - deploys foo as /bar
// The destination location is overwritten.
func deployFile(src, dst string, preserveInDst []string) error {
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

	// overwrite & sync by default
	copyFlags := osutil.CopyFlagOverwrite | osutil.CopyFlagSync

	// TODO use osutil.AtomicFile
	if err := osutil.CopyFile(src, dst, copyFlags); err != nil {
		return fmt.Errorf("cannot copy %s: %v", src, err)
	}
	return nil
}

func (m *MountedFilesystemWriter) deployOneContent(whereDir string, content *VolumeContent, preserveInDst []string) error {
	realSource := filepath.Join(m.rootDir, content.Source)
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
		return deployDirectory(realSource, realTarget, preserveInDst)
	} else {
		// deploy a file
		return deployFile(realSource, realTarget, preserveInDst)
	}
}
