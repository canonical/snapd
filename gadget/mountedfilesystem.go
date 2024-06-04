// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"io/fs"
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

func checkContent(content *ResolvedContent) error {
	if content.ResolvedSource == "" {
		return fmt.Errorf("internal error: source cannot be unset")
	}
	if content.Target == "" {
		return fmt.Errorf("internal error: target cannot be unset")
	}
	return nil
}

func observe(observer ContentObserver, op ContentOperation, partRole, root, dst string, data *ContentChange) (ContentChangeAction, error) {
	if observer == nil {
		return ChangeApply, nil
	}
	relativeTarget := dst
	if strings.HasPrefix(dst, root) {
		// target path isn't really relative, make it so now
		relative, err := filepath.Rel(root, dst)
		if err != nil {
			return ChangeAbort, err
		}
		relativeTarget = relative
	}
	return observer.Observe(op, partRole, root, relativeTarget, data)
}

// TODO: MountedFilesystemWriter should not be exported

// MountedFilesystemWriter assists in writing contents of a structure to a
// mounted filesystem.
type MountedFilesystemWriter struct {
	fromPs   *LaidOutStructure
	ps       *LaidOutStructure
	observer ContentObserver
}

// NewMountedFilesystemWriter returns a writer capable of writing provided
// structure, with content of the structure stored in the given root directory.
func NewMountedFilesystemWriter(fromPs, ps *LaidOutStructure, observer ContentObserver) (*MountedFilesystemWriter, error) {
	if ps == nil {
		return nil, fmt.Errorf("internal error: *LaidOutStructure is nil")
	}
	if !ps.HasFilesystem() {
		return nil, fmt.Errorf("structure %v has no filesystem", ps)
	}
	fw := &MountedFilesystemWriter{
		fromPs:   fromPs,
		ps:       ps,
		observer: observer,
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

	// TODO:UC20: preserve managed boot assets
	preserveInDst, err := mapPreserve(whereDir, preserve)
	if err != nil {
		return fmt.Errorf("cannot map preserve entries for destination %q: %v", whereDir, err)
	}

	for _, c := range m.ps.ResolvedContent {
		if err := m.writeVolumeContent(whereDir, &c, preserveInDst); err != nil {
			return fmt.Errorf("cannot write filesystem content of %s: %v", c, err)
		}
	}
	return nil
}

// writeDirectory copies the source directory, or its contents under target
// location dst. Follows rsync like semantics, that is:
//
//	/foo/ -> /bar - writes contents of foo under /bar
//	/foo  -> /bar - writes foo and its subtree under /bar
func (m *MountedFilesystemWriter) writeDirectory(volumeRoot, src, dst string, preserveInDst []string) error {
	hasDirSourceSlash := strings.HasSuffix(src, "/")

	if err := checkSourceIsDir(src); err != nil {
		return err
	}

	if !hasDirSourceSlash {
		// /foo -> /bar (write foo and subtree)
		dst = filepath.Join(dst, filepath.Base(src))
	}

	fis, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("cannot list directory entries: %v", err)
	}

	for _, fi := range fis {
		pSrc := filepath.Join(src, fi.Name())
		pDst := filepath.Join(dst, fi.Name())

		write := m.observedWriteFileOrSymlink
		if fi.IsDir() {
			if err := os.MkdirAll(pDst, 0755); err != nil {
				return fmt.Errorf("cannot create directory prefix: %v", err)
			}

			write = m.writeDirectory
			pSrc += "/"
		}
		if err := write(volumeRoot, pSrc, pDst, preserveInDst); err != nil {
			return err
		}
	}

	return nil
}

func (m *MountedFilesystemWriter) observedWriteFileOrSymlink(volumeRoot, src, dst string, preserveInDst []string) error {
	if strings.HasSuffix(dst, "/") {
		// write to directory
		dst = filepath.Join(dst, filepath.Base(src))
	}

	data := &ContentChange{
		// we are writing a new thing
		Before: "",
		// with content in this file
		After: src,
	}
	act, err := observe(m.observer, ContentWrite, m.ps.Role(), volumeRoot, dst, data)
	if err != nil {
		return fmt.Errorf("cannot observe file write: %v", err)
	}
	if act == ChangeIgnore {
		return nil
	}
	return writeFileOrSymlink(src, dst, preserveInDst)
}

// writeFileOrSymlink writes the source file or a symlink at given location or
// under given directory. Follows rsync like semantics, that is:
//
//	/foo -> /bar/ - writes foo as /bar/foo
//	/foo  -> /bar - writes foo as /bar
//
// The destination location is overwritten.
func writeFileOrSymlink(src, dst string, preserveInDst []string) error {
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

	if osutil.IsSymlink(src) {
		// recreate the symlinks as they are
		to, err := os.Readlink(src)
		if err != nil {
			return fmt.Errorf("cannot read symlink: %v", err)
		}
		if err := os.Symlink(to, dst); err != nil {
			return fmt.Errorf("cannot write a symlink: %v", err)
		}
	} else {
		// TODO try to preserve ownership and permission bits

		// do not follow sylimks, dst is a reflection of the src which
		// is a file
		if err := osutil.AtomicWriteFileCopy(dst, src, 0); err != nil {
			return fmt.Errorf("cannot copy %s: %v", src, err)
		}
	}
	return nil
}

func (m *MountedFilesystemWriter) writeVolumeContent(volumeRoot string, content *ResolvedContent, preserveInDst []string) error {
	if err := checkContent(content); err != nil {
		return err
	}
	realTarget := filepath.Join(volumeRoot, content.Target)

	// filepath trims the trailing /, restore if needed
	if strings.HasSuffix(content.Target, "/") {
		realTarget += "/"
	}

	if osutil.IsDirectory(content.ResolvedSource) || strings.HasSuffix(content.ResolvedSource, "/") {
		// write a directory
		return m.writeDirectory(volumeRoot, content.ResolvedSource, realTarget, preserveInDst)
	} else {
		// write a file
		return m.observedWriteFileOrSymlink(volumeRoot, content.ResolvedSource, realTarget, preserveInDst)
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

type mountLookupFunc func(ps *LaidOutStructure) (string, error)

// mountedFilesystemUpdater assists in applying updates to a mounted filesystem.
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
type mountedFilesystemUpdater struct {
	*MountedFilesystemWriter
	backupDir      string
	mountPoint     string
	updateObserver ContentObserver
}

// newMountedFilesystemUpdater returns an updater for given filesystem
// structure, with structure content coming from provided root directory. The
// mount is located by calling a mount lookup helper. The backup directory
// contains backup state information for use during rollback.
func newMountedFilesystemUpdater(fromPs, ps *LaidOutStructure, backupDir string, mountLookup mountLookupFunc, observer ContentObserver) (*mountedFilesystemUpdater, error) {
	// avoid passing observer, writes will not be observed
	fw, err := NewMountedFilesystemWriter(fromPs, ps, nil)
	if err != nil {
		return nil, err
	}
	if mountLookup == nil {
		return nil, fmt.Errorf("internal error: mount lookup helper must be provided")
	}
	if backupDir == "" {
		return nil, fmt.Errorf("internal error: backup directory must not be unset")
	}
	mount, err := mountLookup(ps)
	if err != nil {
		return nil, fmt.Errorf("cannot find mount location of structure %v: %v", ps, err)
	}

	fu := &mountedFilesystemUpdater{
		MountedFilesystemWriter: fw,
		backupDir:               backupDir,
		mountPoint:              mount,
		updateObserver:          observer,
	}
	return fu, nil
}

func fsStructBackupPath(backupDir string, ps *LaidOutStructure) string {
	return filepath.Join(backupDir, fmt.Sprintf("struct-%v", ps.VolumeStructure.YamlIndex))
}

// entryDestPaths resolves destination and backup paths for given
// source/target combination. Backup location is within provided
// backup directory or empty if directory was not provided.
func (f *mountedFilesystemUpdater) entryDestPaths(dstRoot, source, target, backupDir string) (dstPath, backupPath string) {
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

func getDestinationPath(content VolumeContent) (dst string) {
	dst = content.Target
	if strings.HasSuffix(dst, "/") {
		dst = filepath.Join(dst, filepath.Base(content.UnresolvedSource))
	}

	return dst
}

func (f *mountedFilesystemUpdater) getKnownContent() (knownContent map[string]bool) {
	knownContent = make(map[string]bool)
	for _, c := range f.ps.VolumeStructure.Content {
		knownContent[getDestinationPath(c)] = true
	}

	return knownContent
}

// Update applies an update to a mounted filesystem. The caller must have
// executed a Backup() before, to prepare a data set for rollback purpose.
func (f *mountedFilesystemUpdater) Update() error {
	preserveInDst, err := mapPreserve(f.mountPoint, f.ps.VolumeStructure.Update.Preserve)
	if err != nil {
		return fmt.Errorf("cannot map preserve entries for mount location %q: %v", f.mountPoint, err)
	}

	backupRoot := fsStructBackupPath(f.backupDir, f.ps)

	skipped := 0

	for _, c := range f.ps.ResolvedContent {
		if err := f.updateVolumeContent(f.mountPoint, &c, preserveInDst, backupRoot); err != nil {
			if err == ErrNoUpdate {
				skipped++
				continue
			}
			return fmt.Errorf("cannot update content: %v", err)
		}
	}

	knownContent := f.getKnownContent()
	deleted := false
	if f.fromPs != nil {
		for _, c := range f.fromPs.VolumeStructure.Content {
			if knownContent[getDestinationPath(c)] {
				continue
			}
			destPath, backupPath := f.entryDestPaths(f.mountPoint, c.UnresolvedSource, c.Target, backupRoot)
			preserveStamp := backupPath + ".preserve"

			// We skip directory because we do not know
			// exactly the content that is supposed to be
			// in there.
			// XXX: it might be possible to recursively compare
			// directories from mounted snaps to detect
			// what files are removed.
			if osutil.IsDirectory(destPath) {
				continue
			}

			if strutil.SortedListContains(preserveInDst, destPath) || osutil.FileExists(preserveStamp) {
				continue
			}

			if err := os.Remove(destPath); err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("cannot remove content: %v", err)
			}
			deleted = true
		}
	}

	if !deleted && skipped == len(f.ps.ResolvedContent) {
		return ErrNoUpdate
	}

	return nil
}

func (f *mountedFilesystemUpdater) sourceDirectoryEntries(srcPath string) ([]fs.DirEntry, error) {
	if err := checkSourceIsDir(srcPath); err != nil {
		return nil, err
	}

	// TODO: enable support for symlinks when needed
	if osutil.IsSymlink(srcPath) {
		return nil, fmt.Errorf("source is a symbolic link")
	}

	return os.ReadDir(srcPath)
}

// targetInSourceDir resolves the actual target for given source directory name
// and target specification.
//
//	source: /foo/bar/ target: /baz => /bar/    (contents of /foo/bar/ under /baz)
//	source: /foo/bar  target: /baz => /bar/bar (directory /foo/bar under /baz, contents under /baz/bar)
func targetForSourceDir(source, target string) string {
	if strings.HasSuffix(source, "/") {
		// contents of source directory land under target
		return target
	}
	// source directory lands under target
	return filepath.Join(target, filepath.Base(source))
}

func (f *mountedFilesystemUpdater) updateDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
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
	skipped := 0
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
			if err == ErrNoUpdate {
				skipped++
				continue
			}
			return err
		}
	}

	if skipped == len(fis) {
		return ErrNoUpdate
	}

	return nil
}

func (f *mountedFilesystemUpdater) updateOrSkipFile(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	dstPath, backupPath := f.entryDestPaths(dstRoot, source, target, backupDir)
	backupName := backupPath + ".backup"
	sameStamp := backupPath + ".same"
	preserveStamp := backupPath + ".preserve"
	ignoreStamp := backupPath + ".ignore"

	// TODO: enable support for symlinks when needed
	if osutil.IsSymlink(source) {
		return fmt.Errorf("cannot update file %s: symbolic links are not supported", source)
	}

	if osutil.FileExists(ignoreStamp) {
		// explicitly ignored by request of the observer
		return ErrNoUpdate
	}

	if osutil.FileExists(dstPath) {
		if strutil.SortedListContains(preserveInDst, dstPath) || osutil.FileExists(preserveStamp) {
			// file is to be preserved
			return ErrNoUpdate
		}
		if osutil.FileExists(sameStamp) {
			// file is the same as current copy
			return ErrNoUpdate
		}
		if !osutil.FileExists(backupName) {
			// not preserved & different than the update, error out
			// as there is no backup
			return fmt.Errorf("missing backup file %q for %v", backupName, target)
		}
	}

	return writeFileOrSymlink(source, dstPath, preserveInDst)
}

func (f *mountedFilesystemUpdater) updateVolumeContent(volumeRoot string, content *ResolvedContent, preserveInDst []string, backupDir string) error {
	if err := checkContent(content); err != nil {
		return err
	}

	if osutil.IsDirectory(content.ResolvedSource) || strings.HasSuffix(content.ResolvedSource, "/") {
		// TODO: pass both Unresolved and resolved Source (unresolved for better error reporting)
		return f.updateDirectory(volumeRoot, content.ResolvedSource, content.Target, preserveInDst, backupDir)
	} else {
		// TODO: pass both Unresolved and resolved Source (unresolved for better error reporting)
		return f.updateOrSkipFile(volumeRoot, content.ResolvedSource, content.Target, preserveInDst, backupDir)
	}
}

// Backup analyzes a mounted filesystem and prepares a rollback state should the
// update be applied. The content of the filesystem is processed, files and
// directories that would be modified by the update are backed up, while
// identical/preserved files may be stamped to improve the later step of update
// process.
//
// The backup directory structure mirrors the structure of destination
// location. Given the following destination structure:
//
// foo
// ├── a
// ├── b
// ├── bar
// │   ├── baz
// │   │   └── d
// │   └── z
// ├── c
// └── d
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
// ├── c.ignore           <-- stamp indicating change to ./c was requested to be ignored
// └── d.preserve         <-- stamp indicating ./d is to be preserved
func (f *mountedFilesystemUpdater) Backup() error {
	backupRoot := fsStructBackupPath(f.backupDir, f.ps)

	if err := os.MkdirAll(backupRoot, 0755); err != nil {
		return fmt.Errorf("cannot create backup directory: %v", err)
	}

	preserveInDst, err := mapPreserve(f.mountPoint, f.ps.VolumeStructure.Update.Preserve)
	if err != nil {
		return fmt.Errorf("cannot map preserve entries for mount location %q: %v", f.mountPoint, err)
	}

	for _, c := range f.ps.ResolvedContent {
		if err := f.backupVolumeContent(f.mountPoint, &c, preserveInDst, backupRoot); err != nil {
			return fmt.Errorf("cannot backup content: %v", err)
		}
	}

	knownContent := f.getKnownContent()
	if f.fromPs != nil {
		for _, c := range f.fromPs.VolumeStructure.Content {
			if knownContent[getDestinationPath(c)] {
				continue
			}

			destPath, backupPath := f.entryDestPaths(f.mountPoint, c.UnresolvedSource, c.Target, backupRoot)
			// We skip directory because we do not know
			// exactly the content that is supposed to be
			// in there.
			// XXX: it might be possible to recursively compare
			// directories from mounted snaps to detect
			// what files are removed.
			if osutil.IsDirectory(destPath) {
				continue
			}
			backupName := backupPath + ".backup"

			if !osutil.FileExists(destPath) {
				continue
			}

			if err := writeFileOrSymlink(destPath, backupName, nil); err != nil {
				return fmt.Errorf("cannot create backup file: %v", err)
			}
		}
	}

	return nil
}

func (f *mountedFilesystemUpdater) backupOrCheckpointDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	fis, err := f.sourceDirectoryEntries(source)
	if err != nil {
		return fmt.Errorf("cannot backup directory %q: %v", source, err)
	}

	target = targetForSourceDir(source, target)

	for _, fi := range fis {
		pSrc := filepath.Join(source, fi.Name())
		pDst := filepath.Join(target, fi.Name())

		backup := f.observedBackupOrCheckpointFile
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
func (f *mountedFilesystemUpdater) checkpointPrefix(dstRoot, target string, backupDir string) error {
	// check how much of the prefix needs to be created
	for prefix := filepath.Dir(target); prefix != "." && prefix != "/"; prefix = filepath.Dir(prefix) {
		prefixDst, prefixBackupBase := f.entryDestPaths(dstRoot, "", prefix, backupDir)

		// TODO: enable support for symlinks when needed
		if osutil.IsSymlink(prefixDst) {
			return fmt.Errorf("cannot create a checkpoint for directory %v: symbolic links are not supported", prefix)
		}

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

func (f *mountedFilesystemUpdater) ignoreChange(backupPath string) error {
	preserveStamp := backupPath + ".preserve"
	backupName := backupPath + ".backup"
	sameStamp := backupPath + ".same"
	ignoreStamp := backupPath + ".ignore"
	if err := makeStamp(ignoreStamp); err != nil {
		return fmt.Errorf("cannot create a checkpoint file: %v", err)
	}
	for _, name := range []string{backupName, sameStamp, preserveStamp} {
		if err := os.Remove(name); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove existing stamp file: %v", err)
		}
	}
	return nil
}

func (f *mountedFilesystemUpdater) observedBackupOrCheckpointFile(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	change, err := f.backupOrCheckpointFile(dstRoot, source, target, preserveInDst, backupDir)
	if err != nil {
		return err
	}
	if change != nil {
		dstPath, backupPath := f.entryDestPaths(dstRoot, source, target, backupDir)
		act, err := observe(f.updateObserver, ContentUpdate, f.ps.Role(), f.mountPoint, dstPath, change)
		if err != nil {
			return fmt.Errorf("cannot observe pending file write %v\n", err)
		}
		if act == ChangeIgnore {
			// observer asked for the change to be ignored
			if err := f.ignoreChange(backupPath); err != nil {
				return fmt.Errorf("cannot ignore content change: %v", err)
			}
		}
	}
	return nil
}

// backupOrCheckpointFile analyzes a given source file from the gadget and a
// target location under the provided destination root directory. When both
// files are identical, creates a stamp that allows the update to skip the file.
// When content of the new file is different, a backup of the original file is
// created. Returns a content change if a file will be written by the update
// pass.
func (f *mountedFilesystemUpdater) backupOrCheckpointFile(dstRoot, source, target string, preserveInDst []string, backupDir string) (change *ContentChange, err error) {
	dstPath, backupPath := f.entryDestPaths(dstRoot, source, target, backupDir)

	backupName := backupPath + ".backup"
	sameStamp := backupPath + ".same"
	preserveStamp := backupPath + ".preserve"
	ignoreStamp := backupPath + ".ignore"

	changeWithBackup := &ContentChange{
		// content of the new data
		After: source,
		// this is the original data that was present before the
		// update
		Before: backupName,
	}
	changeNewFile := &ContentChange{
		// content of the new data
		After: source,
	}

	if osutil.FileExists(ignoreStamp) {
		// observer already requested the change to the target location
		// to be ignored
		return nil, nil
	}

	// TODO: enable support for symlinks when needed
	if osutil.IsSymlink(dstPath) {
		return nil, fmt.Errorf("cannot backup file %s: symbolic links are not supported", target)
	}

	if !osutil.FileExists(dstPath) {
		// destination does not exist and will be created when writing
		// the udpate, no need for backup
		return changeNewFile, nil
	}
	// destination file exists beyond this point

	if osutil.FileExists(backupName) {
		// file already checked and backed up
		return changeWithBackup, nil
	}
	if osutil.FileExists(sameStamp) {
		// file already checked, same as the update, move on
		return nil, nil
	}
	// TODO: correctly identify new files that were written by a partially
	// executed update pass

	if strutil.SortedListContains(preserveInDst, dstPath) {
		if osutil.FileExists(preserveStamp) {
			// already stamped
			return nil, nil
		}
		// make a stamp
		if err := makeStamp(preserveStamp); err != nil {
			return nil, fmt.Errorf("cannot create preserve stamp: %v", err)
		}
		return nil, nil
	}

	// try to find out whether the update and the existing file are
	// identical

	orig, err := os.Open(dstPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open destination file: %v", err)
	}

	// backup of the original content
	backup, err := newStampFile(backupName)
	if err != nil {
		return nil, fmt.Errorf("cannot create backup file: %v", err)
	}
	// becomes a backup copy or a noop if canceled
	defer backup.Commit()

	// checksum the original data while it's being copied
	origHash := crypto.SHA1.New()
	htr := io.TeeReader(orig, origHash)

	_, err = io.Copy(backup, htr)
	if err != nil {
		backup.Cancel()
		return nil, fmt.Errorf("cannot backup original file: %v", err)
	}

	// digest of the update
	updateDigest, _, err := osutil.FileDigest(source, crypto.SHA1)
	if err != nil {
		backup.Cancel()
		return nil, fmt.Errorf("cannot checksum update file: %v", err)
	}
	// digest of the currently present data
	origDigest := origHash.Sum(nil)

	// TODO: look into comparing the streams directly
	if bytes.Equal(origDigest, updateDigest) {
		// mark that files are identical and update can be skipped, no
		// backup is needed
		if err := makeStamp(sameStamp); err != nil {
			return nil, fmt.Errorf("cannot create a checkpoint file: %v", err)
		}

		// makes the deferred commit a noop
		backup.Cancel()
		return nil, nil
	}

	// update will overwrite existing file, a backup copy is created on
	// Commit()
	return changeWithBackup, nil
}

func (f *mountedFilesystemUpdater) backupVolumeContent(volumeRoot string, content *ResolvedContent, preserveInDst []string, backupDir string) error {
	if err := checkContent(content); err != nil {
		return err
	}

	if err := f.checkpointPrefix(volumeRoot, content.Target, backupDir); err != nil {
		return err
	}
	if osutil.IsDirectory(content.ResolvedSource) || strings.HasSuffix(content.ResolvedSource, "/") {
		// backup directory contents
		// TODO: pass both Unresolved and resolved Source (unresolved for better error reporting)
		return f.backupOrCheckpointDirectory(volumeRoot, content.ResolvedSource, content.Target, preserveInDst, backupDir)
	} else {
		// backup a file
		return f.observedBackupOrCheckpointFile(volumeRoot, content.ResolvedSource, content.Target, preserveInDst, backupDir)
	}
}

// Rollback attempts to revert changes done by the update step, using state
// information collected during backup phase. Files that were modified by the
// update are stored from their backup copies, newly added directories are
// removed.
func (f *mountedFilesystemUpdater) Rollback() error {
	backupRoot := fsStructBackupPath(f.backupDir, f.ps)

	preserveInDst, err := mapPreserve(f.mountPoint, f.ps.VolumeStructure.Update.Preserve)
	if err != nil {
		return fmt.Errorf("cannot map preserve entries for mount location %q: %v", f.mountPoint, err)
	}

	knownContent := f.getKnownContent()
	if f.fromPs != nil {
		for _, c := range f.fromPs.VolumeStructure.Content {
			if knownContent[getDestinationPath(c)] {
				continue
			}

			destPath, backupPath := f.entryDestPaths(f.mountPoint, c.UnresolvedSource, c.Target, backupRoot)

			if osutil.IsDirectory(destPath) {
				continue
			}

			if err := os.Remove(destPath); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("cannot rollback %s: %v", destPath, err)
				}
			}

			backupName := backupPath + ".backup"

			if err := writeFileOrSymlink(backupName, destPath, nil); err != nil {
				return fmt.Errorf("cannot rollback %s: %v", destPath, err)
			}
		}
	}

	for _, c := range f.ps.ResolvedContent {
		if err := f.rollbackVolumeContent(f.mountPoint, &c, preserveInDst, backupRoot); err != nil {
			return fmt.Errorf("cannot rollback content: %v", err)
		}
	}
	return nil
}

func (f *mountedFilesystemUpdater) rollbackPrefix(dstRoot, target string, backupDir string) error {
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

func (f *mountedFilesystemUpdater) rollbackDirectory(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
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

func (f *mountedFilesystemUpdater) rollbackFile(dstRoot, source, target string, preserveInDst []string, backupDir string) error {
	dstPath, backupPath := f.entryDestPaths(dstRoot, source, target, backupDir)

	backupName := backupPath + ".backup"
	sameStamp := backupPath + ".same"
	preserveStamp := backupPath + ".preserve"
	ignoreStamp := backupPath + ".ignore"

	if strutil.SortedListContains(preserveInDst, dstPath) && osutil.FileExists(preserveStamp) {
		// file was preserved at original location by being
		// explicitly listed
		return nil
	}
	if osutil.FileExists(sameStamp) {
		// contents are the same as original, do nothing
		return nil
	}
	if osutil.FileExists(ignoreStamp) {
		// observer requested the changes to the target to be ignored
		// previously
		return nil
	}

	data := &ContentChange{
		After: source,
		// original content was in the backup file
		Before: backupName,
	}

	if osutil.FileExists(backupName) {
		// restore backup -> destination
		if err := writeFileOrSymlink(backupName, dstPath, nil); err != nil {
			return err
		}
	} else {
		if err := os.Remove(dstPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cannot remove written update: %v", err)
		}
		// since it's a new file, there was no original content
		data.Before = ""
	}
	// avoid passing source path during rollback, the file has been restored
	// to the disk already
	_, err := observe(f.updateObserver, ContentRollback, f.ps.Role(), f.mountPoint, dstPath, data)
	if err != nil {
		return fmt.Errorf("cannot observe pending file rollback %v\n", err)
	}

	return nil
}

func (f *mountedFilesystemUpdater) rollbackVolumeContent(volumeRoot string, content *ResolvedContent, preserveInDst []string, backupDir string) error {
	if err := checkContent(content); err != nil {
		return err
	}

	var err error
	if osutil.IsDirectory(content.ResolvedSource) || strings.HasSuffix(content.ResolvedSource, "/") {
		// rollback directory
		err = f.rollbackDirectory(volumeRoot, content.ResolvedSource, content.Target, preserveInDst, backupDir)
	} else {
		// rollback file
		err = f.rollbackFile(volumeRoot, content.ResolvedSource, content.Target, preserveInDst, backupDir)
	}
	if err != nil {
		return err
	}

	return f.rollbackPrefix(volumeRoot, content.Target, backupDir)
}
