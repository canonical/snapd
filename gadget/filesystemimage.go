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

	"github.com/snapcore/snapd/logger"
)

type MkfsFunc func(imgFile, label, contentsRootDir string) error

var (
	mkfsHandlers = map[string]MkfsFunc{
		"vfat": MkfsVfat,
		"ext4": MkfsExt4,
	}
)

// FilesystemImageWriter is capable of creating filesystem images described by
// positioned structures.
type FilesystemImageWriter struct {
	contentDir string
	ps         *PositionedStructure
	workDir    string
}

// PostStageFunc is called after the filesystem contents for the given structure
// have been staged at a temporary location
type PostStageFunc func(rootDir string, ps *PositionedStructure) error

// Write creates the filesystem inside the provided image file and populates it
// with data according to content declartion of the structure. Content data is
// staged in a temporary location. An optional post-stage helper function can be
// used to manipulate the data before it is copied over to the image.
func (f *FilesystemImageWriter) Write(fname string, postStage PostStageFunc) error {
	st, err := os.Stat(fname)
	if err != nil {
		return fmt.Errorf("cannot stat image file: %v", err)
	}
	if sz := st.Size(); sz != int64(f.ps.Size) {
		return fmt.Errorf("size of image file %v is different from declared structure size %v", sz, f.ps.Size)
	}

	mkfs := mkfsHandlers[f.ps.Filesystem]
	if mkfs == nil {
		return fmt.Errorf("internal error: filesystem %q has no handler", f.ps.Filesystem)
	}

	where, err := ioutil.TempDir(f.workDir, "snap-stage-content-")
	if err != nil {
		return fmt.Errorf("cannot prepare staging directory: %v", err)
	}

	if os.Getenv("SNAP_DEBUG_IMAGE_NO_CLEANUP") == "" {
		defer func() {
			if err := os.RemoveAll(where); err != nil {
				logger.Noticef("cannot remove filesystem staging directory %q: %v", where, err)
			}
		}()
	}

	mrw, err := NewMountedFilesystemWriter(f.contentDir, f.ps)
	if err != nil {
		return fmt.Errorf("cannot prepare filesystem writer for %v: %v", f.ps, err)
	}
	// drop all contents to the staging directory
	if err := mrw.Write(where, nil); err != nil {
		return fmt.Errorf("cannot prepare filesystem content: %v", err)
	}

	if postStage != nil {
		if err := postStage(where, f.ps); err != nil {
			return fmt.Errorf("post stage callback failed: %v", err)
		}
	}

	if err := mkfs(fname, f.ps.Label, where); err != nil {
		return fmt.Errorf("cannot create %q filesystem: %v", f.ps.Filesystem, err)
	}

	return nil
}

// NewFilesystemImageWriter returns a writer capable of creating filesystem
// images corresponding to the provided positiioned structure, with content from
// the given content directory. A staging directory will be created in
// optionally prided work directory, otherwise the default temporary storage
// location will be used.
func NewFilesystemImageWriter(contentDir string, ps *PositionedStructure, workDir string) (*FilesystemImageWriter, error) {
	if ps == nil {
		return nil, fmt.Errorf("internal error: *PositionedStructure is nil")
	}
	if ps.IsBare() {
		return nil, fmt.Errorf("internal error: structure has no filesystem")
	}
	if contentDir == "" {
		return nil, fmt.Errorf("internal error: gadget content directory cannot be unset")
	}
	if _, ok := mkfsHandlers[ps.Filesystem]; !ok {
		return nil, fmt.Errorf("internal error: filesystem %q is not supported", ps.Filesystem)
	}

	fiw := &FilesystemImageWriter{
		contentDir: contentDir,
		ps:         ps,
		workDir:    workDir,
	}
	return fiw, nil
}
