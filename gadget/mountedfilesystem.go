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

func checkSourceIsDir(src string) error {
	if !osutil.IsDirectory(src) {
		if strings.HasSuffix(src, "/") {
			return fmt.Errorf("cannot specify trailing / for a source which is not a directory")
		}
		return fmt.Errorf("source is not a directory")
	}
	return nil
}

func checkContent(content *VolumeContent) error {
	if content.Source == "" {
		return fmt.Errorf("internal error: source cannot be unset")
	}
	if content.Target == "" {
		return fmt.Errorf("internal error: target cannot be unset")
	}
	return nil
}

// MountedFilesystemWriter assists in writing contents of a structure to a
// mounted filesystem.
type MountedFilesystemWriter struct {
	contentDir string
	ps         *PositionedStructure
}

// NewMountedFilesystemWriter returns a writer capable of writing provided
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

func mapPreserve(dstDir string, preserve []string) ([]string, error) {
	preserveInDst := make([]string, len(preserve))
	for i, p := range preserve {
		inDst := filepath.Join(dstDir, p)

		if osutil.IsDirectory(inDst) {
			return nil, fmt.Errorf("preserved entry %q cannot be a directory", p)
		}

		preserveInDst[i] = inDst
	}
	sort.Strings(preserveInDst)

	return preserveInDst, nil
}

// Write writes structure data into provided directory. All existing files are
// overwritten, unless their paths, relative to target directory, are listed in
// the preserve list. Permission bits and ownership of updated entries is not
// preserved.
func (m *MountedFilesystemWriter) Write(whereDir string, preserve []string) error {
	if whereDir == "" {
		return fmt.Errorf("internal error: destination directory cannot be unset")
	}

	preserveInDst, err := mapPreserve(whereDir, preserve)
	if err != nil {
		return fmt.Errorf("cannot map preserve entries for destination %q: %v", whereDir, err)
	}

	for _, c := range m.ps.Content {
		if err := m.writeVolumeContent(whereDir, &c, preserveInDst); err != nil {
			return fmt.Errorf("cannot write filesystem content of %s: %v", c, err)
		}
	}
	return nil
}

// writeDirectory copies the source directory, or its contents under target
// location dst. Follows rsync like semantics, that is:
//   /foo/ -> /bar - writes contents of foo under /bar
//   /foo  -> /bar - writes foo and its subtree under /bar
func writeDirectory(src, dst string, preserveInDst []string) error {
	hasDirSourceSlash := strings.HasSuffix(src, "/")

	if err := checkSourceIsDir(src); err != nil {
		return err
	}

	if !hasDirSourceSlash {
		// /foo -> /bar (write foo and subtree)
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
//   /foo -> /bar/ - writes foo as /bar/foo
//   /foo  -> /bar - writes foo as /bar
// The destination location is overwritten.
func writeFile(src, dst string, preserveInDst []string) error {
	if strings.HasSuffix(dst, "/") {
		// write to directory
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
	// TODO try to preserve ownership and permission bits
	if err := osutil.CopyFile(src, dst, copyFlags); err != nil {
		return fmt.Errorf("cannot copy %s: %v", src, err)
	}
	return nil
}

func (m *MountedFilesystemWriter) writeVolumeContent(volumeRoot string, content *VolumeContent, preserveInDst []string) error {
	if err := checkContent(content); err != nil {
		return err
	}
	realSource := filepath.Join(m.contentDir, content.Source)
	realTarget := filepath.Join(volumeRoot, content.Target)

	// filepath trims the trailing /, restore if needed
	if strings.HasSuffix(content.Target, "/") {
		realTarget += "/"
	}
	if strings.HasSuffix(content.Source, "/") {
		realSource += "/"
	}

	if osutil.IsDirectory(realSource) || strings.HasSuffix(content.Source, "/") {
		// write a directory
		return writeDirectory(realSource, realTarget, preserveInDst)
	} else {
		// write a file
		return writeFile(realSource, realTarget, preserveInDst)
	}
}

func newStampFile(stamp string) (*osutil.AtomicFile, error) {
	if err := os.MkdirAll(filepath.Dir(stamp), 0755); err != nil {
		return nil, fmt.Errorf("cannot create stamp file prefix: %v", err)
	}
	return osutil.NewAtomicFile(stamp, 0644, 0, osutil.NoChown, osutil.NoChown)
}

func makeStamp(stamp string) error {
	f, err := newStampFile(stamp)
	if err != nil {
		return err
	}
	return f.Commit()
}

type mountLookupFunc func(ps *PositionedStructure) (string, error)

// MountedFilesystemUpdater assits in applying updates to a mounted filesystem.
//
// The update process is composed of 2 main passes, and an optional rollback:
//
// 1) backup, where update data and current data is analyzed to identify
// identical content, stamp files are created for entries that are to be
// preserved, modified or otherwise touched by the update, that is, for existing
// files that would be created/overwritten, ones that are explicitly listed as
// preserved, or directories to be written to
//
// 2) update, where update data is written to the target location
//
// 3) rollback (optional), where update data is rolled back and replaced with
// backup copies of files, newly created directories are removed
type MountedFilesystemUpdater struct {
	*MountedFilesystemWriter
	backupDir   string
	mountLookup mountLookupFunc
}

// NewMountedFilesystemUpdater returns an updater for given filesystem
// structure, with structure content coming from provided root directory. The
// mount is located by calling a mount lookup helper. The backup directory
// contains backup state information for use during rollback.
func NewMountedFilesystemUpdater(rootDir string, ps *PositionedStructure, backupDir string, mountLookup mountLookupFunc) (*MountedFilesystemUpdater, error) {
	fw, err := NewMountedFilesystemWriter(rootDir, ps)
	if err != nil {
		return nil, err
	}
	if mountLookup == nil {
		return nil, fmt.Errorf("internal error: mount lookup helper must be provided")
	}
	if backupDir == "" {
		return nil, fmt.Errorf("internal error: backup directory must not be unset")
	}
	fu := &MountedFilesystemUpdater{
		MountedFilesystemWriter: fw,
		backupDir:               backupDir,
		mountLookup:             mountLookup,
	}
	return fu, nil
}

func fsStructBackupPath(backupDir string, ps *PositionedStructure) string {
	return filepath.Join(backupDir, fmt.Sprintf("struct-%v", ps.Index))
}

// entryDestPaths resolves destination and backup paths for given
// source/target combination. Backup location is within provided
// backup directory or empty if directory was not provided.
func (f *MountedFilesystemUpdater) entryDestPaths(dstRoot, source, target, backupDir string) (dstPath, backupPath string) {
	dstBasePath := target
	if strings.HasSuffix(target, "/") {
		// write to a directory
		dstBasePath = filepath.Join(dstBasePath, filepath.Base(source))
	}
	dstPath = filepath.Join(dstRoot, dstBasePath)

	if backupDir != "" {
		backupPath = filepath.Join(backupDir, dstBasePath)
	}

	return dstPath, backupPath
}

// entrySourcePath returns the path of given source entry within the root
// directory provided during initialization.
func (f *MountedFilesystemUpdater) entrySourcePath(source string) string {
	srcPath := filepath.Join(f.contentDir, source)

	if strings.HasSuffix(source, "/") {
		// restore trailing / if one was there
		srcPath += "/"
	}
	return srcPath
}

// Update applies an update to a mounted filesystem. The caller must have
// executed a Backup() before, to prepare a data set for rollback purpose.
func (f *MountedFilesystemUpdater) Update() error {
	mount, err := f.mountLookup(f.ps)
	if err != nil {
		return fmt.Errorf("cannot find mount location of structure %v: %v", f.ps, err)
	}

	preserveInDst, err := mapPreserve(mount, f.ps.Update.Preserve)
	if err != nil {
		return fmt.Errorf("cannot map preserve entries for mount location %q: %v", mount, err)
	}

	backupRoot := fsStructBackupPath(f.backupDir, f.ps)

	for _, c := range f.ps.Content {
		if err := f.updateVolumeContent(mount, &c, preserveInDst, backupRoot); err != nil {
			return fmt.Errorf("cannot update content: %v", err)
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) sourceDirectoryEntries(source string) ([]os.FileInfo, error) {
	srcPath := f.entrySourcePath(source)

	if err := checkSourceIsDir(srcPath); err != nil {
		return nil, err
	}

	return ioutil.ReadDir(srcPath)
}

// targetInSourceDir resolves the actual target for given source directory name
// and target specification.
//  source: /foo/bar/ target: /baz => /bar/    (contents of /foo/bar/ under /baz)
//  source: /foo/bar  target: /baz => /bar/bar (directory /foo/bar under /baz, contents under /baz/bar)
func targetForSourceDir(source, target string) string {
	if strings.HasSuffix(source, "/") {
		// contents of source directory land under target
		return target
	}
	// source directory lands under target
	return filepath.Join(target, filepath.Base(source))
}

func (f *MountedFilesystemUpdater) updateDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	fis, err := f.sourceDirectoryEntries(source)
	if err != nil {
		return fmt.Errorf("cannot list source directory %q: %v", source, err)
	}

	target = targetForSourceDir(source, target)

	// create current target directory if needed
	if err := os.MkdirAll(filepath.Join(dstRoot, target), 0755); err != nil {
		return fmt.Errorf("cannot write directory: %v", err)
	}
	// and write the content of source to target
	for _, fi := range fis {
		pSrc := filepath.Join(source, fi.Name())
		pDst := filepath.Join(target, fi.Name())

		update := f.updateOrSkipFile
		if fi.IsDir() {
			// continue updating contents of the directory rather
			// than the directory itself
			pSrc += "/"
			pDst += "/"
			update = f.updateDirectory
		}
		if err := update(dstRoot, pSrc, pDst, preserveInDst, backupDir); err != nil {
			return err
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) updateOrSkipFile(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	srcPath := f.entrySourcePath(source)
	dstPath, backupPath := f.entryDestPaths(dstRoot, source, target, backupDir)

	if osutil.FileExists(dstPath) {
		if strutil.SortedListContains(preserveInDst, dstPath) {
			// file is to be preserved
			return nil
		}
		if osutil.FileExists(backupPath + ".same") {
			// file is the same as current copy
			return nil
		}
		if !osutil.FileExists(backupPath + ".backup") {
			// not preserved & different than the update, error out
			// as there is no backup
			return fmt.Errorf("missing backup file %q for %v", backupPath+".backup", target)
		}
	}

	return writeFile(srcPath, dstPath, preserveInDst)
}

func (f *MountedFilesystemUpdater) updateVolumeContent(volumeRoot string, content *VolumeContent, preserveInDst []string, backupDir string) error {
	if err := checkContent(content); err != nil {
		return err
	}

	srcPath := f.entrySourcePath(content.Source)

	if osutil.IsDirectory(srcPath) || strings.HasSuffix(content.Source, "/") {
		return f.updateDirectory(volumeRoot, content.Source, content.Target, preserveInDst, backupDir)
	} else {
		return f.updateOrSkipFile(volumeRoot, content.Source, content.Target, preserveInDst, backupDir)
	}
}

// Backup analyzes a mounted filesystem and prepares a rollback state should the
// update be applied. The content of the filesystem is processed, files and
// directories that would be modified by the update are backed up, while
// identical/preserved files may be stamped to improve the later step of update
// process.
//
// The backup directory structure mirrors the the structure of destination
// location. Given the following destination structure:
//
// foo
// ├── a
// ├── b
// ├── bar
// │   ├── baz
// │   │   └── d
// │   └── z
// └── c
//
// The structure of backup looks like this:
//
// foo-backup
// ├── a.backup           <-- backup copy of ./a
// ├── bar
// │   ├── baz
// │   │   └── d.backup   <-- backup copy of ./bar/baz/d
// │   └── baz.backup     <-- stamp indicating ./bar/baz existed before the update
// ├── bar.backup         <-- stamp indicating ./bar existed before the update
// ├── b.same             <-- stamp indicating ./b is identical to the update data
// └── c.preserve         <-- stamp indicating ./c is to be preserved
//
func (f *MountedFilesystemUpdater) Backup() error {
	mount, err := f.mountLookup(f.ps)
	if err != nil {
		return fmt.Errorf("cannot find mount location of structure %v: %v", f.ps, err)
	}

	backupRoot := fsStructBackupPath(f.backupDir, f.ps)

	if err := os.MkdirAll(backupRoot, 0755); err != nil {
		return fmt.Errorf("cannot create backup directory: %v", err)
	}

	preserveInDst, err := mapPreserve(mount, f.ps.Update.Preserve)
	if err != nil {
		return fmt.Errorf("cannot map preserve entries for mount location %q: %v", mount, err)
	}

	for _, c := range f.ps.Content {
		if err := f.backupVolumeContent(mount, &c, preserveInDst, backupRoot); err != nil {
			return fmt.Errorf("cannot backup content: %v", err)
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) backupOrCheckpointDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	fis, err := f.sourceDirectoryEntries(source)
	if err != nil {
		return fmt.Errorf("cannot backup directory %q: %v", source, err)
	}

	target = targetForSourceDir(source, target)

	for _, fi := range fis {
		pSrc := filepath.Join(source, fi.Name())
		pDst := filepath.Join(target, fi.Name())

		backup := f.backupOrCheckpointFile
		if fi.IsDir() {
			// continue backing up the contents of the directory
			// rather than the directory itself
			pSrc += "/"
			pDst += "/"
			backup = f.backupOrCheckpointDirectory
		}
		if err := f.checkpointPrefix(dstRoot, pDst, backupDir); err != nil {
			return err
		}
		if err := backup(dstRoot, pSrc, pDst, preserveInDst, backupDir); err != nil {
			return err
		}
	}

	return nil
}

// checkpointPrefix creates stamps for each part of the destination prefix that exists
func (f *MountedFilesystemUpdater) checkpointPrefix(dstRoot, target string, backupDir string) error {
	// check how much of the prefix needs to be created
	for prefix := filepath.Dir(target); prefix != "." && prefix != "/"; prefix = filepath.Dir(prefix) {
		prefixDst, prefixBackupBase := f.entryDestPaths(dstRoot, "", prefix, backupDir)

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
	srcPath := f.entrySourcePath(source)
	dstPath, backupPath := f.entryDestPaths(dstRoot, source, target, backupDir)

	backupName := backupPath + ".backup"
	sameStamp := backupPath + ".same"
	preserveStamp := backupPath + ".preserve"

	if !osutil.FileExists(dstPath) {
		// destination does not exist and will be created when writing
		// the udpate, no need for backup
		return nil
	}

	if osutil.FileExists(backupName) || osutil.FileExists(sameStamp) {
		// file already checked, either has a backup or is the same as
		// the update, move on
		return nil
	}

	if strutil.SortedListContains(preserveInDst, dstPath) {
		// file is to be preserved, create a relevant stamp

		if !osutil.FileExists(dstPath) {
			// preserve, but does not exist, will be written anyway
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

	// try to find out whether the update and the existing file are
	// identical

	orig, err := os.Open(dstPath)
	if err != nil {
		return fmt.Errorf("cannot open destination file: %v", err)
	}

	// backup of the original content
	backup, err := newStampFile(backupName)
	if err != nil {
		return fmt.Errorf("cannot create backup file: %v", err)
	}
	// becomes a backup copy or a noop if canceled
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
	updateDigest, _, err := osutil.FileDigest(srcPath, crypto.SHA1)
	if err != nil {
		backup.Cancel()
		return fmt.Errorf("cannot checksum update file: %v", err)
	}
	// digest of the currently present data
	origDigest := origHash.Sum(nil)

	// TODO: look into comparing the streams directly
	if bytes.Equal(origDigest, updateDigest) {
		// mark that files are identical and update can be skipped, no
		// backup is needed
		if err := makeStamp(sameStamp); err != nil {
			return fmt.Errorf("cannot create a checkpoint file: %v", err)
		}

		// makes the deferred commit a noop
		backup.Cancel()
		return nil
	}

	// update will overwrite existing file, a backup copy is created on
	// Commit()
	return nil
}

func (f *MountedFilesystemUpdater) backupVolumeContent(volumeRoot string, content *VolumeContent, preserveInDst []string, backupDir string) error {
	if err := checkContent(content); err != nil {
		return err
	}

	srcPath := f.entrySourcePath(content.Source)

	if err := f.checkpointPrefix(volumeRoot, content.Target, backupDir); err != nil {
		return err
	}
	if osutil.IsDirectory(srcPath) || strings.HasSuffix(content.Source, "/") {
		// backup directory contents
		return f.backupOrCheckpointDirectory(volumeRoot, content.Source, content.Target, preserveInDst, backupDir)
	} else {
		// backup a file
		return f.backupOrCheckpointFile(volumeRoot, content.Source, content.Target, preserveInDst, backupDir)
	}
}

// Rollback attempts to revert changes done by the update step, using state
// information collected during backup phase. Files that were modified by the
// update are stored from their backup copies, newly added directories are
// removed.
func (f *MountedFilesystemUpdater) Rollback() error {
	mount, err := f.mountLookup(f.ps)
	if err != nil {
		return fmt.Errorf("cannot find mount location of structure %v: %v", f.ps, err)
	}

	backupRoot := fsStructBackupPath(f.backupDir, f.ps)

	preserveInDst, err := mapPreserve(mount, f.ps.Update.Preserve)
	if err != nil {
		return fmt.Errorf("cannot map preserve entries for mount location %q: %v", mount, err)
	}

	for _, c := range f.ps.Content {
		if err := f.rollbackVolumeContent(mount, &c, preserveInDst, backupRoot); err != nil {
			return fmt.Errorf("cannot rollback content: %v", err)
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) rollbackPrefix(dstRoot, target string, backupDir string) error {
	for prefix := filepath.Dir(target); prefix != "/" && prefix != "."; prefix = filepath.Dir(prefix) {
		prefixDstPath, prefixBackupPath := f.entryDestPaths(dstRoot, "", prefix, backupDir)
		if !osutil.FileExists(prefixBackupPath + ".backup") {
			// try remove
			if err := os.Remove(prefixDstPath); err != nil {
				logger.Noticef("cannot remove gadget directory %q: %v", prefix, err)
			}
		}
	}
	return nil
}

func (f *MountedFilesystemUpdater) rollbackDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	fis, err := f.sourceDirectoryEntries(source)
	if err != nil {
		return fmt.Errorf("cannot rollback directory %q: %v", source, err)
	}

	target = targetForSourceDir(source, target)

	for _, fi := range fis {
		pSrc := filepath.Join(source, fi.Name())
		pDst := filepath.Join(target, fi.Name())

		rollback := f.rollbackFile
		if fi.IsDir() {
			// continue rolling back the contents of the directory
			// rather than the directory itself
			rollback = f.rollbackDirectory
			pSrc += "/"
			pDst += "/"
		}
		if err := rollback(dstRoot, pSrc, pDst, preserveInDst, backupDir); err != nil {
			return err
		}
		if err := f.rollbackPrefix(dstRoot, pDst, backupDir); err != nil {
			return err
		}
	}

	return nil
}

func (f *MountedFilesystemUpdater) rollbackFile(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	dstPath, backupPath := f.entryDestPaths(dstRoot, source, target, backupDir)

	backupName := backupPath + ".backup"
	sameStamp := backupPath + ".same"
	preserveStamp := backupPath + ".preserve"

	if strutil.SortedListContains(preserveInDst, dstPath) && osutil.FileExists(preserveStamp) {
		// file was preserved at original location, do nothing
		return nil
	}
	if osutil.FileExists(sameStamp) {
		// contents are the same as original, do nothing
		return nil
	}

	if osutil.FileExists(backupName) {
		// restore backup -> destination
		return writeFile(backupName, dstPath, nil)
	}

	// none of the markers exists, file is not preserved, meaning, it has
	// been added by the update

	if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot remove written update: %v", err)
	}

	return nil
}

func (f *MountedFilesystemUpdater) rollbackVolumeContent(volumeRoot string, content *VolumeContent, preserveInDst []string, backupDir string) error {
	if err := checkContent(content); err != nil {
		return err
	}

	srcPath := f.entrySourcePath(content.Source)

	var err error
	if osutil.IsDirectory(srcPath) || strings.HasSuffix(content.Source, "/") {
		// rollback directory
		err = f.rollbackDirectory(volumeRoot, content.Source, content.Target, preserveInDst, backupDir)
	} else {
		// rollback file
		err = f.rollbackFile(volumeRoot, content.Source, content.Target, preserveInDst, backupDir)
	}
	if err != nil {
		return err
	}

	return f.rollbackPrefix(volumeRoot, content.Target, backupDir)
}
