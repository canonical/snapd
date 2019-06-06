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
	"bytes"
	"crypto"
	_ "crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
)

// RawStructureWriter implements support for writing raw (bare) structures.
type RawStructureWriter struct {
	rootDir string
	ps      *PositionedStructure
}

// NewRawStructureWriter returns a writer for the given structure, that will load
// the structure content data from the provided root directory.
func NewRawStructureWriter(rootDir string, ps *PositionedStructure) (*RawStructureWriter, error) {
	if ps == nil {
		return nil, fmt.Errorf("internal error: missing structure")
	}
	if !ps.IsBare() {
		return nil, fmt.Errorf("internal error: structure %s is not bare", ps)
	}
	if rootDir == "" {
		return nil, fmt.Errorf("internal error: content root directory not provided")
	}
	rw := &RawStructureWriter{
		rootDir: rootDir,
		ps:      ps,
	}
	return rw, nil
}

// writeRawStream writes the input stream in that corresponds to provided
// positioned content. The number of bytes read from input stream must match
// exactly the declared size of positioned content entry.
func writeRawStream(out io.WriteSeeker, pc *PositionedContent, in io.Reader) error {
	if _, err := out.Seek(int64(pc.StartOffset), io.SeekStart); err != nil {
		return fmt.Errorf("cannot seek to content start offset 0x%x: %v", pc.StartOffset, err)
	}

	_, err := io.CopyN(out, in, int64(pc.Size))
	if err != nil {
		return fmt.Errorf("cannot write image: %v", err)
	}
	return nil
}

// writeRawImage writes a single image described by a positioned content entry.
func (r *RawStructureWriter) writeRawImage(out io.WriteSeeker, pc *PositionedContent) error {
	if pc == nil {
		return fmt.Errorf("internal error: content not provided")
	}
	if pc.Image == "" {
		return fmt.Errorf("internal error: no image defined")
	}
	img, err := os.Open(filepath.Join(r.rootDir, pc.Image))
	if err != nil {
		return fmt.Errorf("cannot open image file: %v", err)
	}
	defer img.Close()

	return writeRawStream(out, pc, img)
}

// Write will write whole contents of a structure into the output stream.
func (r *RawStructureWriter) Write(out io.WriteSeeker) error {
	for _, pc := range r.ps.PositionedContent {
		if err := r.writeRawImage(out, &pc); err != nil {
			return fmt.Errorf("failed to write image %v: %v", pc, err)
		}
	}
	return nil
}

// RawStructureUpdater implements support for updating raw (bare) structures.
type RawStructureUpdater struct {
	*RawStructureWriter
	backupDir    string
	deviceLookup locationLookupFunc
}

type locationLookupFunc func(ps *PositionedStructure) (string, error)

// NewRawStructureUpdater returns an updater for the given raw (bare) structure.
// Update data will be loaded from the provided root directory. Backups of replaced
// structures are temporarily kept in the rollback directory.
func NewRawStructureUpdater(rootDir string, ps *PositionedStructure, backupDir string, deviceLookup locationLookupFunc) (*RawStructureUpdater, error) {
	if deviceLookup == nil {
		return nil, fmt.Errorf("internal error: missing device lookup helper")
	}
	if backupDir == "" {
		return nil, fmt.Errorf("internal error: backup directory not provided")
	}

	rw, err := NewRawStructureWriter(rootDir, ps)
	if err != nil {
		return nil, err
	}
	ru := &RawStructureUpdater{
		RawStructureWriter: rw,
		backupDir:          backupDir,
		deviceLookup:       deviceLookup,
	}
	return ru, nil
}

func rawContentBackupPath(backupDir string, ps *PositionedStructure, pc *PositionedContent) string {
	return filepath.Join(backupDir, fmt.Sprintf("struct-%v-%v", ps.Index, pc.Index))
}

func (r *RawStructureUpdater) backupOrCheckpointContent(disk io.ReadSeeker, pc *PositionedContent) error {
	backupPath := rawContentBackupPath(r.backupDir, r.ps, pc)
	backupName := backupPath + ".backup"
	sameName := backupPath + ".same"

	if osutil.FileExists(backupName) || osutil.FileExists(sameName) {
		// already have a backup or the image was found to be identical
		// before
		return nil
	}

	if _, err := disk.Seek(int64(pc.StartOffset), io.SeekStart); err != nil {
		return fmt.Errorf("cannot seek to structure's start offset: %v", err)
	}

	// copy out at most the size of updated content
	lr := io.LimitReader(disk, int64(pc.Size))

	// backup the original content
	backup, err := osutil.NewAtomicFile(backupName, 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return fmt.Errorf("cannot create backup file: %v", err)
	}
	// becomes a noop if canceled
	defer backup.Commit()

	// checksum the original data while it's being copied
	origHash := crypto.SHA1.New()
	htr := io.TeeReader(lr, origHash)

	_, err = io.CopyN(backup, htr, int64(pc.Size))
	if err != nil {
		defer backup.Cancel()
		return fmt.Errorf("cannot backup original image: %v", err)
	}

	// digest of the update
	updateDigest, _, err := osutil.FileDigest(filepath.Join(r.rootDir, pc.Image), crypto.SHA1)
	if err != nil {
		defer backup.Cancel()
		return fmt.Errorf("cannot checksum update image: %v", err)
	}
	// digest of the currently present data
	origDigest := origHash.Sum(nil)

	if bytes.Equal(origDigest, updateDigest) {
		// files are identical, no update needed
		if err := osutil.AtomicWriteFile(sameName, nil, 0644, 0); err != nil {
			return fmt.Errorf("cannot create a checkpoint file: %v", err)
		}

		// makes the previous commit a noop
		backup.Cancel()
	}

	return nil
}

// Backup attempts to analyze and prepare a backup copy of data that will be
// replaced during subsequent update. Backups are kept in the backup directory
// passed to NewRawStructureUpdater(). Each region replaced by new content is
// copied out to a separate file. Only differing regions are backed up. Analysis
// and backup of each region is checkpointed. Regions that have been backed up
// or determined to be identical will not be analyzed on subsequent calls.
func (r *RawStructureUpdater) Backup() error {
	device, err := r.deviceLookup(r.ps)
	if err != nil {
		return fmt.Errorf("cannot find device matching structure %v: %v", r.ps, err)
	}
	disk, err := os.OpenFile(device, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot open device for reading: %v", err)
	}
	defer disk.Close()

	for _, pc := range r.ps.PositionedContent {
		if err := r.backupOrCheckpointContent(disk, &pc); err != nil {
			return fmt.Errorf("cannot backup image %v: %v", pc, err)
		}
	}

	return nil
}

func (r *RawStructureUpdater) rollbackDifferent(out io.WriteSeeker, pc *PositionedContent) error {
	backupPath := rawContentBackupPath(r.backupDir, r.ps, pc)

	if osutil.FileExists(backupPath + ".same") {
		// content the same, no update needed
		return nil
	}

	backup, err := os.Open(backupPath + ".backup")
	if err != nil {
		return fmt.Errorf("cannot open backup image: %v", err)
	}

	if err := writeRawStream(out, pc, backup); err != nil {
		return fmt.Errorf("cannot restore backup: %v", err)
	}

	return nil
}

// Rollback attempts to restore original content from the backup copies prepared during Backup().
func (r *RawStructureUpdater) Rollback() error {
	device, err := r.deviceLookup(r.ps)
	if err != nil {
		return fmt.Errorf("cannot find device matching structure %v: %v", r.ps, err)
	}

	disk, err := os.OpenFile(device, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot open device for writing: %v", err)
	}
	defer disk.Close()

	for _, pc := range r.ps.PositionedContent {
		if err := r.rollbackDifferent(disk, &pc); err != nil {
			return fmt.Errorf("cannot rollback image %v: %v", pc, err)
		}
	}

	return nil
}

func (r *RawStructureUpdater) updateDifferent(disk io.WriteSeeker, pc *PositionedContent) error {
	backupPath := rawContentBackupPath(r.backupDir, r.ps, pc)

	if osutil.FileExists(backupPath + ".same") {
		// content the same, no update needed
		return nil
	}

	if !osutil.FileExists(backupPath + ".backup") {
		// not the same, but a backup file is missing, error out just in
		// case
		return fmt.Errorf("missing backup file")
	}

	if err := r.writeRawImage(disk, pc); err != nil {
		return err
	}

	return nil
}

// Update attempts to update the structure. The structure must have been
// analyzed and backed up by a prior Backup() call.
func (r *RawStructureUpdater) Update() error {
	device, err := r.deviceLookup(r.ps)
	if err != nil {
		return fmt.Errorf("cannot find device matching structure %v: %v", r.ps, err)
	}

	disk, err := os.OpenFile(device, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot open device for writing: %v", err)
	}
	defer disk.Close()

	for _, pc := range r.ps.PositionedContent {
		if err := r.updateDifferent(disk, &pc); err != nil {
			return fmt.Errorf("cannot update image %v: %v", pc, err)
		}
	}

	return nil
}
