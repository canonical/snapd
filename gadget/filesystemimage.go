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
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

type MkfsFunc func(imgFile, label, contentsRootDir string) error

var (
	mkfsHandlers = map[string]MkfsFunc{
		"vfat": MkfsVfat,
		"ext4": MkfsExt4,
	}
)

// FilesystemImageWriter is capable of creating filesystem images described by
// laid out structures.
type FilesystemImageWriter struct {
	contentDir string
	ps         *LaidOutStructure
	workDir    string
}

// PostStageFunc is called after the filesystem contents for the given structure
// have been staged at a temporary location, but before the filesystem image is
// created. The function can be used to manipulate the staged data.
type PostStageFunc func(rootDir string, ps *LaidOutStructure) error

// NewFilesystemImageWriter returns a writer capable of creating filesystem
// images corresponding to the provided structure, with content from the given
// content directory. A staging directory will be created in either, the
// optionally provided work directory, or the default temp location.
func NewFilesystemImageWriter(contentDir string, ps *LaidOutStructure, workDir string) (*FilesystemImageWriter, error) {
	if ps == nil {
		return nil, fmt.Errorf("internal error: *LaidOutStructure is nil")
	}
	if !ps.HasFilesystem() {
		return nil, fmt.Errorf("internal error: structure has no filesystem")
	}
	if contentDir == "" {
		return nil, fmt.Errorf("internal error: gadget content directory cannot be unset")
	}
	if _, ok := mkfsHandlers[ps.Filesystem]; !ok {
		return nil, fmt.Errorf("internal error: filesystem %q has no handler", ps.Filesystem)
	}

	fiw := &FilesystemImageWriter{
		contentDir: contentDir,
		ps:         ps,
		workDir:    workDir,
	}
	return fiw, nil
}

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

	mkfsWithContent := mkfsHandlers[f.ps.Filesystem]
	if mkfsWithContent == nil {
		return fmt.Errorf("internal error: filesystem %q has no handler", f.ps.Filesystem)
	}

	stagingDir := filepath.Join(f.workDir, fmt.Sprintf("snap-stage-content-part-%04d", f.ps.Index))
	if osutil.IsDirectory(stagingDir) {
		return fmt.Errorf("cannot prepare staging directory %s: path exists", stagingDir)
	}

	if err := os.Mkdir(stagingDir, 0755); err != nil {
		return fmt.Errorf("cannot prepare staging directory: %v", err)
	}

	if os.Getenv("SNAP_DEBUG_IMAGE_NO_CLEANUP") == "" {
		defer func() {
			if err := os.RemoveAll(stagingDir); err != nil {
				logger.Noticef("cannot remove filesystem staging directory %q: %v", stagingDir, err)
			}
		}()
	}

	// use a mounted filesystem writer to populate the staging directory
	// with contents of given structure
	mrw, err := NewMountedFilesystemWriter(f.contentDir, f.ps)
	if err != nil {
		return fmt.Errorf("cannot prepare filesystem writer for %v: %v", f.ps, err)
	}
	// drop all contents to the staging directory
	if err := mrw.Write(stagingDir, nil); err != nil {
		return fmt.Errorf("cannot prepare filesystem content: %v", err)
	}

	if postStage != nil {
		if err := postStage(stagingDir, f.ps); err != nil {
			return fmt.Errorf("post stage callback failed: %v", err)
		}
	}

	// create a filesystem with contents of the staging directory
	if err := mkfsWithContent(fname, f.ps.Label, stagingDir); err != nil {
		return fmt.Errorf("cannot create %q filesystem: %v", f.ps.Filesystem, err)
	}

	return nil
}
