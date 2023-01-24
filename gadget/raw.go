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

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
)

// TODO: RawStructureWriter should not be exported

// RawStructureWriter implements support for writing raw (bare) structures.
type RawStructureWriter struct {
	contentDir string
	ps         *LaidOutStructure
}

// NewRawStructureWriter returns a writer for the given structure, that will load
// the structure content data from the provided gadget content directory.
func NewRawStructureWriter(contentDir string, ps *LaidOutStructure) (*RawStructureWriter, error) {
	if ps == nil {
		return nil, fmt.Errorf("internal error: *LaidOutStructure is nil")
	}
	if ps.VolumeStructure.HasFilesystem() {
		return nil, fmt.Errorf("internal error: structure %s has a filesystem", ps)
	}
	if contentDir == "" {
		return nil, fmt.Errorf("internal error: gadget content directory cannot be unset")
	}
	rw := &RawStructureWriter{
		contentDir: contentDir,
		ps:         ps,
	}
	return rw, nil
}

// writeRawStream writes the input stream in that corresponds to provided
// laid out content. The number of bytes read from input stream must match
// exactly the declared size of the content entry.
func writeRawStream(out io.WriteSeeker, pc *LaidOutContent, in io.Reader) error {
	if _, err := out.Seek(int64(pc.StartOffset), io.SeekStart); err != nil {
		return fmt.Errorf("cannot seek to content start offset 0x%x: %v", pc.StartOffset, err)
	}

	_, err := io.CopyN(out, in, int64(pc.Size))
	if err != nil {
		return fmt.Errorf("cannot write image: %v", err)
	}
	return nil
}

// writeRawImage writes a single image described by a laid out content entry.
func (r *RawStructureWriter) writeRawImage(out io.WriteSeeker, pc *LaidOutContent) error {
	if pc.Image == "" {
		return fmt.Errorf("internal error: no image defined")
	}
	img, err := os.Open(filepath.Join(r.contentDir, pc.Image))
	if err != nil {
		return fmt.Errorf("cannot open image file: %v", err)
	}
	defer img.Close()

	return writeRawStream(out, pc, img)
}

// Write will write whole contents of a structure into the output stream.
func (r *RawStructureWriter) Write(out io.WriteSeeker) error {
	for _, pc := range r.ps.LaidOutContent {
		if err := r.writeRawImage(out, &pc); err != nil {
			return fmt.Errorf("failed to write image %v: %v", pc, err)
		}
	}
	return nil
}

// rawStructureUpdater implements support for updating raw (bare) structures.
type rawStructureUpdater struct {
	*RawStructureWriter
	backupDir    string
	deviceLookup deviceLookupFunc
}

type deviceLookupFunc func(ps *LaidOutStructure) (device string, offs quantity.Offset, err error)

// newRawStructureUpdater returns an updater for the given raw (bare) structure.
// Update data will be loaded from the provided gadget content directory.
// Backups of replaced structures are temporarily kept in the rollback
// directory.
func newRawStructureUpdater(contentDir string, ps *LaidOutStructure, backupDir string, deviceLookup deviceLookupFunc) (*rawStructureUpdater, error) {
	if deviceLookup == nil {
		return nil, fmt.Errorf("internal error: device lookup helper must be provided")
	}
	if backupDir == "" {
		return nil, fmt.Errorf("internal error: backup directory cannot be unset")
	}

	rw, err := NewRawStructureWriter(contentDir, ps)
	if err != nil {
		return nil, err
	}
	ru := &rawStructureUpdater{
		RawStructureWriter: rw,
		backupDir:          backupDir,
		deviceLookup:       deviceLookup,
	}
	return ru, nil
}

func rawContentBackupPath(backupDir string, ps *LaidOutStructure, pc *LaidOutContent) string {
	return filepath.Join(backupDir, fmt.Sprintf("struct-%v-%v", ps.YamlIndex, pc.Index))
}

func (r *rawStructureUpdater) backupOrCheckpointContent(disk io.ReadSeeker, pc *LaidOutContent) error {
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
	updateDigest, _, err := osutil.FileDigest(filepath.Join(r.contentDir, pc.Image), crypto.SHA1)
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

// matchDevice identifies the device matching the configured structure, returns
// device path and a shifted structure should any offset adjustments be needed
func (r *rawStructureUpdater) matchDevice() (device string, shifted *LaidOutStructure, err error) {
	device, offs, err := r.deviceLookup(r.ps)
	if err != nil {
		return "", nil, fmt.Errorf("cannot find device matching structure %v: %v", r.ps, err)
	}

	if offs == r.ps.StartOffset {
		return device, r.ps, nil
	}

	// Structure starts at different offset, make the necessary adjustment.
	structForDevice := ShiftStructureTo(*r.ps, offs)
	return device, &structForDevice, nil
}

// Backup attempts to analyze and prepare a backup copy of data that will be
// replaced during subsequent update. Backups are kept in the backup directory
// passed to newRawStructureUpdater(). Each region replaced by new content is
// copied out to a separate file. Only differing regions are backed up. Analysis
// and backup of each region is checkpointed. Regions that have been backed up
// or determined to be identical will not be analyzed on subsequent calls.
func (r *rawStructureUpdater) Backup() error {
	device, structForDevice, err := r.matchDevice()
	if err != nil {
		return err
	}

	disk, err := os.OpenFile(device, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot open device for reading: %v", err)
	}
	defer disk.Close()

	for _, pc := range structForDevice.LaidOutContent {
		if err := r.backupOrCheckpointContent(disk, &pc); err != nil {
			return fmt.Errorf("cannot backup image %v: %v", pc, err)
		}
	}

	return nil
}

func (r *rawStructureUpdater) rollbackDifferent(out io.WriteSeeker, pc *LaidOutContent) error {
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
func (r *rawStructureUpdater) Rollback() error {
	device, structForDevice, err := r.matchDevice()
	if err != nil {
		return err
	}

	disk, err := os.OpenFile(device, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot open device for writing: %v", err)
	}
	defer disk.Close()

	for _, pc := range structForDevice.LaidOutContent {
		if err := r.rollbackDifferent(disk, &pc); err != nil {
			return fmt.Errorf("cannot rollback image %v: %v", pc, err)
		}
	}

	return nil
}

func (r *rawStructureUpdater) updateDifferent(disk io.WriteSeeker, pc *LaidOutContent) error {
	backupPath := rawContentBackupPath(r.backupDir, r.ps, pc)

	if osutil.FileExists(backupPath + ".same") {
		// content the same, no update needed
		return ErrNoUpdate
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
func (r *rawStructureUpdater) Update() error {
	device, structForDevice, err := r.matchDevice()
	if err != nil {
		return err
	}

	disk, err := os.OpenFile(device, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("cannot open device for writing: %v", err)
	}
	defer disk.Close()

	skipped := 0
	for _, pc := range structForDevice.LaidOutContent {
		if err := r.updateDifferent(disk, &pc); err != nil {
			if err == ErrNoUpdate {
				skipped++
				continue
			}
			return fmt.Errorf("cannot update image %v: %v", pc, err)
		}
	}

	if skipped == len(structForDevice.LaidOutContent) {
		// all content is identical, nothing was updated
		return ErrNoUpdate
	}

	return nil
}
