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
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

func makeStamp(stamp string) error {
	if err := os.MkdirAll(filepath.Dir(stamp), 0755); err != nil {
		return fmt.Errorf("cannot create stamp file prefix: %v", err)
	}

	return osutil.AtomicWriteFile(stamp, nil, 0644, 0)
}

func makeStampFile(stamp string) (*osutil.AtomicFile, error) {
	if err := os.MkdirAll(filepath.Dir(stamp), 0755); err != nil {
		return nil, fmt.Errorf("cannot create stamp file prefix: %v", err)
	}
	return osutil.NewAtomicFile(stamp, 0644, 0, osutil.NoChown, osutil.NoChown)
}

type MountedFilesystemWriter struct {
	rootDir string
}

func NewMountedFilesystemWriter(rootDir string) *MountedFilesystemWriter {
	return &MountedFilesystemWriter{
		rootDir: rootDir,
	}
}

func remapPreserve(dstDir string, preserve []string) []string {
	preserveInDst := make([]string, len(preserve))
	for i, p := range preserve {
		preserveInDst[i] = filepath.Join(dstDir, p)
	}
	sort.Strings(preserveInDst)

	return preserveInDst
}

func (m *MountedFilesystemWriter) Deploy(whereDir string, ps *PositionedStructure, preserve []string) error {
	preserveInDst := remapPreserve(whereDir, preserve)
	for _, c := range ps.Content {
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

func (m *MountedFilesystemWriter) DeployContent(whereDir string, content *VolumeContent, preserve []string) error {
	return m.deployOneContent(whereDir, content, remapPreserve(whereDir, preserve))
}

type MountedFilesystemUpdater struct {
	*MountedFilesystemWriter
	backupDir   string
	mountLookup locationLookupFunc
}

func NewMountedFilesystemUpdater(rootDir, backupDir string, mountLookup locationLookupFunc) *MountedFilesystemUpdater {
	return &MountedFilesystemUpdater{
		MountedFilesystemWriter: NewMountedFilesystemWriter(rootDir),
		backupDir:               backupDir,
		mountLookup:             mountLookup,
	}
}

func (r *MountedFilesystemUpdater) structBackupPath(ps *PositionedStructure) string {
	return filepath.Join(r.backupDir, fmt.Sprintf("struct-%v", ps.Index))
}

// entryNames resolves the source, destination and backup locations for given
// source/target combination. The source location is always within the root
// directory passed during initialization. Backup location is within provided
// backup directory or empty if directory was not provided.
func (f *MountedFilesystemUpdater) entryNames(dstRoot, source, target, backupDir string) (srcName, dstName, backupName string) {
	srcName = f.entrySource(source)

	dstBase := target
	if strings.HasSuffix(target, "/") {
		// deploying to a directory
		dstBase = filepath.Join(dstBase, filepath.Base(source))
	}
	dstName = filepath.Join(dstRoot, dstBase)

	if backupDir != "" {
		backupName = filepath.Join(backupDir, dstBase)
	}

	return srcName, dstName, backupName
}

// entrySource returns the location of given source within the root directory
// provided during initialization.
func (f *MountedFilesystemUpdater) entrySource(source string) string {
	srcName := filepath.Join(f.rootDir, source)

	if strings.HasSuffix(source, "/") {
		// restore trailing / if one was there
		srcName += "/"
	}
	return srcName
}

func (f *MountedFilesystemUpdater) deployDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	srcName := f.entrySource(source)

	fis, err := ioutil.ReadDir(srcName)
	if err != nil {
		return fmt.Errorf("cannot list directory entries: %v", err)
	}

	if !strings.HasSuffix(source, "/") {
		// /foo -> /bar/ => /bar/foo
		target = filepath.Join(target, filepath.Base(source))
	}

	for _, fi := range fis {
		fpSrc := filepath.Join(source, fi.Name())
		fpDst := filepath.Join(target, fi.Name())

		deploy := f.deployOrSkipFile
		if fi.IsDir() {
			_, dstName, _ := f.entryNames(dstRoot, source, fpDst, "")
			if err := os.MkdirAll(dstName, 0755); err != nil {
				return fmt.Errorf("cannot deploy directory: %v", err)
			}

			deploy = f.deployDirectory
			fpSrc += "/"
		}
		if err := deploy(dstRoot, fpSrc, fpDst, preserveInDst, backupDir); err != nil {
			return err
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) deployOrSkipFile(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	srcName, dstName, backupName := f.entryNames(dstRoot, source, target, backupDir)

	if osutil.FileExists(dstName) {
		if strutil.SortedListContains(preserveInDst, dstName) {
			// file is to be preserved
			return nil
		}
		if osutil.FileExists(backupName + ".same") {
			// file is the same as current copy
			return nil
		}
		if !osutil.FileExists(backupName + ".backup") {
			// not preserver & different than the update, double
			// check we have a backup
			return fmt.Errorf("missing backup file for %v", dstName)
		}
	}

	return deployFile(srcName, dstName, preserveInDst)
}

func (f *MountedFilesystemUpdater) updateContent(whereDir string, content *VolumeContent, preserveInDst []string, backupDir string) error {
	srcName, _, _ := f.entryNames(whereDir, content.Source, content.Target, backupDir)

	if strings.HasSuffix(content.Source, "/") || osutil.IsDirectory(srcName) {
		// deploy a directory
		return f.deployDirectory(whereDir, content.Source, content.Target, preserveInDst, backupDir)
	} else {
		// deploy a file
		return f.deployOrSkipFile(whereDir, content.Source, content.Target, preserveInDst, backupDir)
	}
}

// Update applies an update to a mounted filesystem representing given
// structure. The caller must have executed a Backup() before, to prepare a data
// set for rollback purpose.
func (f *MountedFilesystemUpdater) Update(from *PositionedStructure, to *PositionedStructure) error {
	mount, err := f.mountLookup(to)
	if err != nil {
		return fmt.Errorf("cannot find mount location of structure %v: %v", to, err)
	}

	preserveInDst := remapPreserve(mount, to.Update.Preserve)
	backupRoot := f.structBackupPath(to)

	for _, c := range to.Content {
		if err := f.updateContent(mount, &c, preserveInDst, backupRoot); err != nil {
			return err
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) backupOrCheckpointDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	srcName := f.entrySource(source)

	fis, err := ioutil.ReadDir(srcName)
	if err != nil {
		return fmt.Errorf("cannot list directory entries: %v", err)
	}

	if !strings.HasSuffix(source, "/") {
		// /foo -> /bar/ => /bar/foo
		target = filepath.Join(target, filepath.Base(source))
	}

	for _, fi := range fis {
		fpSrc := filepath.Join(source, fi.Name())
		fpDst := filepath.Join(target, fi.Name())

		backup := f.backupOrCheckpointFile
		if fi.IsDir() {
			if err := f.backupOrCheckpointPrefix(dstRoot, fpDst+"/", backupDir); err != nil {
				return fmt.Errorf("cannot backup prefix directory: %v", err)
			}

			backup = f.backupOrCheckpointDirectory
			fpSrc += "/"
		}
		if err := backup(dstRoot, fpSrc, fpDst, preserveInDst, backupDir); err != nil {
			return err
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) backupOrCheckpointPrefix(dstRoot, target string, backupDir string) error {

	// file is different and backed up, check how much of the prefix needs to be created
	for prefix := filepath.Dir(target); prefix != "." && prefix != "/"; prefix = filepath.Dir(prefix) {
		_, prefixDst, prefixBackupBase := f.entryNames(dstRoot, "", prefix, backupDir)

		prefixBackupName := prefixBackupBase + ".backup"
		if osutil.FileExists(prefixBackupName) {
			continue
		}
		if !osutil.IsDirectory(prefixDst) {
			// does not exist now, will be created on the fly and
			// removed during rollback
			continue
		}
		if err := os.MkdirAll(filepath.Dir(prefixBackupName), 0755); err != nil {
			return fmt.Errorf("cannot create backup prefix: %v", err)
		}
		if err := makeStamp(prefixBackupName); err != nil {
			return fmt.Errorf("cannot create a checkpoint for directory: %v", err)
		}
	}
	return nil
}

func (f *MountedFilesystemUpdater) backupOrCheckpointFile(dstRoot, source, target string, preserveInDst []string, backupDir string) error {

	srcName, dstName, backupBase := f.entryNames(dstRoot, source, target, backupDir)

	backupName := backupBase + ".backup"
	sameStamp := backupBase + ".same"
	preserveStamp := backupBase + ".preserve"

	if osutil.FileExists(backupName) || osutil.FileExists(sameStamp) {
		// file already checked, move on
		return nil
	}

	if strutil.SortedListContains(preserveInDst, dstName) {
		if !osutil.FileExists(dstName) {
			// preserve, but does not exist, will be deployed anyway
			return nil
		}
		if osutil.FileExists(preserveStamp) {
			// already stamped
			return nil
		}
		// make a stamp
		if err := makeStamp(preserveStamp); err != nil {
			return fmt.Errorf("cannot create preserve stamp: %v", err)
		}
		return nil
	}

	orig, err := os.Open(dstName)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("cannot open destination file: %v", err)
		}

		// destination does not exist, no need for backup
		return f.backupOrCheckpointPrefix(dstRoot, target, backupDir)
	}

	// backup of the original content
	backup, err := makeStampFile(backupName)
	if err != nil {
		return fmt.Errorf("cannot create backup file: %v", err)
	}
	// becomes a noop if canceled
	defer backup.Commit()

	// checksum the original data while it's being copied
	origHash := crypto.SHA1.New()
	htr := io.TeeReader(orig, origHash)

	_, err = io.Copy(backup, htr)
	if err != nil {
		backup.Cancel()
		return fmt.Errorf("cannot backup original file: %v", err)
	}

	// digest of the update
	updateDigest, _, err := osutil.FileDigest(srcName, crypto.SHA1)
	if err != nil {
		backup.Cancel()
		return fmt.Errorf("cannot checksum update file: %v", err)
	}
	// digest of the currently present data
	origDigest := origHash.Sum(nil)

	if bytes.Equal(origDigest, updateDigest) {
		// files are identical, no backup needed
		if err := makeStamp(sameStamp); err != nil {
			return fmt.Errorf("cannot create a checkpoint file: %v", err)
		}

		// makes the previous commit a noop
		backup.Cancel()
		return nil
	}

	return f.backupOrCheckpointPrefix(dstRoot, target, backupDir)
}

func (f *MountedFilesystemUpdater) backupOrCheckpoint(whereDir string, content *VolumeContent, preserveInDst []string, backupDir string) error {
	srcName := f.entrySource(content.Source)

	if strings.HasSuffix(content.Source, "/") || osutil.IsDirectory(srcName) {
		// backup directory contents
		return f.backupOrCheckpointDirectory(whereDir, content.Source, content.Target, preserveInDst, backupDir)
	} else {
		// deploy a file
		return f.backupOrCheckpointFile(whereDir, content.Source, content.Target, preserveInDst, backupDir)
	}
}

// Backup analyzes a mounted filesystem and prepares a rollback state
// information should the update be applied. The content of the filesystem is
// processed, files that would be modified by the update are backed up, while
// other files may be stamped to improve the later step of update process.
func (f *MountedFilesystemUpdater) Backup(from *PositionedStructure, to *PositionedStructure) error {
	mount, err := f.mountLookup(to)
	if err != nil {
		return fmt.Errorf("cannot find mount location of structure %v: %v", to, err)
	}

	backupRoot := f.structBackupPath(to)

	if err := os.MkdirAll(backupRoot, 0755); err != nil {
		return fmt.Errorf("cannot create backup directory: %v", err)
	}

	for _, c := range to.Content {
		if err := f.backupOrCheckpoint(mount, &c, remapPreserve(mount, to.Update.Preserve), backupRoot); err != nil {
			return fmt.Errorf("cannot create a backup structure: %v", err)
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) rollbackPrefix(dstRoot, target string, backupDir string) error {
	for prefix := filepath.Dir(target); prefix != "/" && prefix != "."; prefix = filepath.Dir(prefix) {
		_, prefixDst, prefixBackupBase := f.entryNames(dstRoot, "", prefix, backupDir)
		if !osutil.FileExists(prefixBackupBase + ".backup") {
			// try remove
			if err := os.Remove(prefixDst); err != nil {
				logger.Noticef("cannot remove gadget directory %v: %v", prefix, err)
			}
		}
	}
	return nil
}

func (f *MountedFilesystemUpdater) rollbackDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	srcName := f.entrySource(source)

	fis, err := ioutil.ReadDir(srcName)
	if err != nil {
		return fmt.Errorf("cannot list directory entries: %v", err)
	}

	if !strings.HasSuffix(source, "/") {
		// /foo -> /bar/ => /bar/foo
		target = filepath.Join(target, filepath.Base(source))
	}

	for _, fi := range fis {
		fpSrc := filepath.Join(source, fi.Name())
		fpDst := filepath.Join(target, fi.Name())

		rollback := f.rollbackFile
		if fi.IsDir() {
			rollback = f.rollbackDirectory
			fpSrc += "/"
		}
		if err := rollback(dstRoot, fpSrc, fpDst, preserveInDst, backupDir); err != nil {
			return err
		}
		if fi.IsDir() {
			if err := f.rollbackPrefix(dstRoot, fpDst+"/", backupDir); err != nil {
				return err
			}
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) rollbackFile(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	_, dstName, backupBase := f.entryNames(dstRoot, source, target, backupDir)

	backupName := backupBase + ".backup"
	sameStamp := backupBase + ".same"
	preserveStamp := backupBase + ".preserve"

	if strutil.SortedListContains(preserveInDst, dstName) && osutil.FileExists(preserveStamp) {
		// file was preserved at original location, do nothing
		return nil
	}
	if osutil.FileExists(sameStamp) {
		// contents are the same as original, do nothing
		return nil
	}

	if osutil.FileExists(backupName) {
		// restore backup -> destination
		return deployFile(backupName, dstName, nil)
	}

	// none of the markers exists, file is not preserved, meaning, it has
	// been added by the update

	if err := os.Remove(dstName); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove deployed update: %v", err)
	}

	return f.rollbackPrefix(dstRoot, target, backupDir)
}

func (f *MountedFilesystemUpdater) rollbackContent(whereDir string, content *VolumeContent, preserveInDst []string, backupDir string) error {
	srcName := f.entrySource(content.Source)

	if strings.HasSuffix(content.Source, "/") || osutil.IsDirectory(srcName) {
		// rollback directory
		return f.rollbackDirectory(whereDir, content.Source, content.Target, preserveInDst, backupDir)
	} else {
		// rollback file
		return f.rollbackFile(whereDir, content.Source, content.Target, preserveInDst, backupDir)
	}
}

// Rollback attempst to revert changes done by the update step, using state
// information collected during backup phase. Files that were modified by the
// update are stored from their backup copies, newly added directories are
// removed.
func (f *MountedFilesystemUpdater) Rollback(from *PositionedStructure, to *PositionedStructure) error {
	mount, err := f.mountLookup(to)
	if err != nil {
		return fmt.Errorf("cannot find mount location of structure %v: %v", to, err)
	}

	backupRoot := f.structBackupPath(to)

	preserveInDst := remapPreserve(mount, to.Update.Preserve)

	for _, c := range to.Content {
		if err := f.rollbackContent(mount, &c, preserveInDst, backupRoot); err != nil {
			return err
		}
	}

	return nil
}
