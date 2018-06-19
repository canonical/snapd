// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
)

// not available through syscall
const (
	umountNoFollow = 8
	// StReadOnly is the equivalent of ST_RDONLY
	StReadOnly = 1
	// SquashfsMagic is the equivalent of SQUASHFS_MAGIC
	SquashfsMagic = 0x73717368
	// Ext4Magic is the equivalent of EXT4_SUPER_MAGIC
	Ext4Magic = 0xef53
	// TmpfsMagic is the equivalent of TMPFS_MAGIC
	TmpfsMagic = 0x01021994
)

// For mocking everything during testing.
var (
	osLstat    = os.Lstat
	osReadlink = os.Readlink
	osRemove   = os.Remove

	sysClose      = syscall.Close
	sysMkdirat    = syscall.Mkdirat
	sysMount      = syscall.Mount
	sysOpen       = syscall.Open
	sysOpenat     = syscall.Openat
	sysUnmount    = syscall.Unmount
	sysFchown     = sys.Fchown
	sysFstat      = syscall.Fstat
	sysFstatfs    = syscall.Fstatfs
	sysSymlinkat  = osutil.Symlinkat
	sysReadlinkat = osutil.Readlinkat
	sysFchdir     = syscall.Fchdir
	sysLstat      = syscall.Lstat

	ioutilReadDir = ioutil.ReadDir
)

// IsReadOnly returns true if a directory is ready only.
//
// Directories are read only when they reside on file systems mounted in read
// only mode or when the underlying file system itself is inherently read only.
func IsReadOnly(dirFd int, dirName string) (bool, error) {
	var fsData syscall.Statfs_t
	if err := sysFstatfs(dirFd, &fsData); err != nil {
		return false, fmt.Errorf("cannot fstatfs %q: %s", dirName, err)
	}
	// If something is mounted with f_flags & ST_RDONLY then is read-only.
	if fsData.Flags&StReadOnly == StReadOnly {
		return true, nil
	}
	// If something is a known read-only file-system then it is safe.
	// Older copies of snapd were not mounting squashfs as read only.
	if fsData.Type == SquashfsMagic {
		return true, nil
	}
	return false, nil
}

// IsSnapdCreatedPrivateTmpfs returns true if a directory is a tmpfs mounted by snapd.
//
// The function inspects the directory (represented as both an open file
// descriptor and the absolute path) and a list of changes that were applied to
// the mount namespace. A directory is trusted if it is a tmpfs that was
// mounted by snap-confine or snapd-update-ns. Note that sub-directories of a
// trusted tmpfs are not considered trusted by this function.
func IsSnapdCreatedPrivateTmpfs(dirFd int, dirName string, changes []*Change) (bool, error) {
	var fsData syscall.Statfs_t
	if err := sysFstatfs(dirFd, &fsData); err != nil {
		return false, fmt.Errorf("cannot fstatfs %q: %s", dirName, err)
	}
	// If something is not a tmpfs it cannot be the trusted tmpfs we are looking for.
	if fsData.Type != TmpfsMagic {
		return false, nil
	}
	// Any of the past changes that mounted a tmpfs exactly at the directory we
	// are inspecting is considered as trusted. This is conservative because it
	// doesn't trust sub-directories of a trusted tmpfs. This approach is
	// sufficient for the intended use.
	//
	// The algorithm goes over all the changes in reverse and picks up the
	// first tmpfs mount or unmount action that matches the directory name.
	// The set of constraints in snap-update-ns and snapd prevent from mounting
	// over an existing mount point so we don't need to consider e.g. a bind
	// mount shadowing an active tmpfs.
	for i := len(changes) - 1; i >= 0; i-- {
		change := changes[i]
		if change.Entry.Type == "tmpfs" && change.Entry.Dir == dirName {
			return change.Action == Mount, nil
		}
	}
	// TODO: As a special exception, assume that a tmpfs over /var/lib is
	// trusted. This tmpfs is created by snap-confine as a "quirk" to support
	// a particular behavior of LXD.  Once the quirk is migrated to a mount
	// profile (or removed entirely if no longer necessary) the following code
	// fragment can go away.
	return dirName == "/var/lib", nil
}

// ReadOnlyFsError is an error encapsulating encountered EROFS.
type ReadOnlyFsError struct {
	Path string
}

func (e *ReadOnlyFsError) Error() string {
	return fmt.Sprintf("cannot operate on read-only filesystem at %s", e.Path)
}

// Secure is a helper for making filesystem operations free from certain kinds of attacks.
type Secure struct{}

// CheckTrespassing inspects if a filesystem operation on the given path
// segment would trespass on the host.
//
// The idea of trespassing is so that we do not attempt to create a
// directory, a file or a symlink in a directory that is usually beyond
// control of snap applications. We are perfectly comfortable with creating
// things in $SNAP_DATA or in /tmp but we don't want to do so in /etc or in
// other sensitive places.
func (sec *Secure) CheckTrespassing(dirFd int, dirName string, name string) error {
	return nil
}

// OpenPath creates a path file descriptor for the given
// path, making sure no components are symbolic links.
//
// The file descriptor is opened using the O_PATH, O_NOFOLLOW,
// and O_CLOEXEC flags.
func (sec *Secure) OpenPath(path string) (int, error) {
	iter, err := strutil.NewPathIterator(path)
	if err != nil {
		return -1, fmt.Errorf("cannot open path: %s", err)
	}
	if !filepath.IsAbs(iter.Path()) {
		return -1, fmt.Errorf("path %v is not absolute", iter.Path())
	}
	iter.Next() // Advance iterator to '/'
	// We use the following flags to open:
	//  O_PATH: we don't intend to use the fd for IO
	//  O_NOFOLLOW: don't follow symlinks
	//  O_DIRECTORY: we expect to find directories (except for the leaf)
	//  O_CLOEXEC: don't leak file descriptors over exec() boundaries
	openFlags := sys.O_PATH | syscall.O_NOFOLLOW | syscall.O_DIRECTORY | syscall.O_CLOEXEC
	fd, err := sysOpen("/", openFlags, 0)
	if err != nil {
		return -1, err
	}
	for iter.Next() {
		// Ensure the parent file descriptor is closed
		defer sysClose(fd)
		if !strings.HasSuffix(iter.CurrentName(), "/") {
			openFlags &^= syscall.O_DIRECTORY
		}
		fd, err = sysOpenat(fd, iter.CurrentCleanName(), openFlags, 0)
		if err != nil {
			return -1, err
		}
	}

	var statBuf syscall.Stat_t
	err = sysFstat(fd, &statBuf)
	if err != nil {
		sysClose(fd)
		return -1, err
	}
	if statBuf.Mode&syscall.S_IFMT == syscall.S_IFLNK {
		sysClose(fd)
		return -1, fmt.Errorf("%q is a symbolic link", path)
	}
	return fd, nil
}

// MkPrefix creates all the missing directories in a given base path and
// returns the file descriptor to the leaf directory. This function is a base
// for secure variants of mkdir, touch and symlink. None of the traversed
// directories can be symbolic links.
func (sec *Secure) MkPrefix(base string, perm os.FileMode, uid sys.UserID, gid sys.GroupID) (int, error) {
	iter, err := strutil.NewPathIterator(base)
	if err != nil {
		// TODO: Reword the error and adjust the tests.
		return -1, fmt.Errorf("cannot split unclean path %q", base)
	}
	if !filepath.IsAbs(iter.Path()) {
		return -1, fmt.Errorf("path %v is not absolute", iter.Path())
	}
	iter.Next() // Advance iterator to '/'

	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_DIRECTORY
	// Open the root directory and start there.
	fd, err := sysOpen("/", openFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("cannot open root directory: %v", err)
	}
	for iter.Next() {
		// Keep closing the previous descriptor as we go, so that we have the
		// last one handy from the MkDir below.
		defer sysClose(fd)
		fd, err = sec.MkDir(fd, iter.CurrentBase(), iter.CurrentCleanName(), perm, uid, gid)
		if err != nil {
			return -1, err
		}
	}

	return fd, nil
}

// MkDir creates a directory with a given name.
//
// The directory is represented with a file descriptor and its name (for
// convenience). This function is meant to be used to construct subsequent
// elements of some path. The return value contains the newly created file
// descriptor for the new directory or -1 on error.
func (sec *Secure) MkDir(dirFd int, dirName string, name string, perm os.FileMode, uid sys.UserID, gid sys.GroupID) (int, error) {
	made := true

	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_DIRECTORY

	if err := sysMkdirat(dirFd, name, uint32(perm.Perm())); err != nil {
		switch err {
		case syscall.EEXIST:
			made = false
		case syscall.EROFS:
			// Treat EROFS specially: this is a hint that we have to poke a
			// hole using tmpfs. The path below is the location where we
			// need to poke the hole.
			return -1, &ReadOnlyFsError{Path: dirName}
		default:
			return -1, fmt.Errorf("cannot create directory %q: %v", filepath.Join(dirName, name), err)
		}
	}
	newFd, err := sysOpenat(dirFd, name, openFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("cannot open directory %q: %v", filepath.Join(dirName, name), err)
	}
	if made {
		// Chown each segment that we made.
		if err := sysFchown(newFd, uid, gid); err != nil {
			// Close the FD we opened if we fail here since the caller will get
			// an error and won't assume responsibility for the FD.
			sysClose(newFd)
			return -1, fmt.Errorf("cannot chown directory %q to %d.%d: %v", filepath.Join(dirName, name), uid, gid, err)
		}
	}
	return newFd, err
}

// MkFile creates a file with a given name.
//
// The directory is represented with a file descriptor and its name (for
// convenience). This function is meant to be used to create the leaf file as a
// preparation for a mount point. Existing files are reused without errors.
// Newly created files have the specified mode and ownership.
func (sec *Secure) MkFile(dirFd int, dirName string, name string, perm os.FileMode, uid sys.UserID, gid sys.GroupID) error {
	made := true

	// NOTE: Tests don't show O_RDONLY as has a value of 0 and is not
	// translated to textual form. It is added here for explicitness.
	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_RDONLY

	// Open the final path segment as a file. Try to create the file (so that
	// we know if we need to chown it) but fall back to just opening an
	// existing one.

	newFd, err := sysOpenat(dirFd, name, openFlags|syscall.O_CREAT|syscall.O_EXCL, uint32(perm.Perm()))
	if err != nil {
		switch err {
		case syscall.EEXIST:
			// If the file exists then just open it without O_CREAT and O_EXCL
			newFd, err = sysOpenat(dirFd, name, openFlags, 0)
			if err != nil {
				return fmt.Errorf("cannot open file %q: %v", filepath.Join(dirName, name), err)
			}
			made = false
		case syscall.EROFS:
			// Treat EROFS specially: this is a hint that we have to poke a
			// hole using tmpfs. The path below is the location where we
			// need to poke the hole.
			return &ReadOnlyFsError{Path: dirName}
		default:
			return fmt.Errorf("cannot open file %q: %v", filepath.Join(dirName, name), err)
		}
	}
	defer sysClose(newFd)

	if made {
		// Chown the file if we made it.
		if err := sysFchown(newFd, uid, gid); err != nil {
			return fmt.Errorf("cannot chown file %q to %d.%d: %v", filepath.Join(dirName, name), uid, gid, err)
		}
	}

	return nil
}

// MkSymlink creates a symlink with a given name.
//
// The directory is represented with a file descriptor and its name (for
// convenience). This function is meant to be used to create the leaf symlink.
// Existing and identical symlinks are reused without errors.
func (sec *Secure) MkSymlink(dirFd int, dirName string, name string, oldname string) error {
	// Create the final path segment as a symlink.
	// TODO: don't write links outside of tmpfs or $SNAP_{,USER_}{DATA,COMMON}
	if err := sysSymlinkat(oldname, dirFd, name); err != nil {
		switch err {
		case syscall.EEXIST:
			var objFd int
			// If the file exists then just open it for examination.
			// Maybe it's the symlink we were hoping to create.
			objFd, err = sysOpenat(dirFd, name, syscall.O_CLOEXEC|sys.O_PATH|syscall.O_NOFOLLOW, 0)
			if err != nil {
				return fmt.Errorf("cannot open existing file %q: %v", filepath.Join(dirName, name), err)
			}
			defer sysClose(objFd)
			var statBuf syscall.Stat_t
			err = sysFstat(objFd, &statBuf)
			if err != nil {
				return fmt.Errorf("cannot inspect existing file %q: %v", filepath.Join(dirName, name), err)
			}
			if statBuf.Mode&syscall.S_IFMT != syscall.S_IFLNK {
				return fmt.Errorf("cannot create symbolic link %q: existing file in the way", filepath.Join(dirName, name))
			}
			var n int
			buf := make([]byte, len(oldname)+2)
			n, err = sysReadlinkat(objFd, "", buf)
			if err != nil {
				return fmt.Errorf("cannot read symbolic link %q: %v", filepath.Join(dirName, name), err)
			}
			if string(buf[:n]) != oldname {
				return fmt.Errorf("cannot create symbolic link %q: existing symbolic link in the way", filepath.Join(dirName, name))
			}
			return nil
		case syscall.EROFS:
			// Treat EROFS specially: this is a hint that we have to poke a
			// hole using tmpfs. The path below is the location where we
			// need to poke the hole.
			return &ReadOnlyFsError{Path: dirName}
		default:
			return fmt.Errorf("cannot create symlink %q: %v", filepath.Join(dirName, name), err)
		}
	}

	return nil
}

// MkdirAll is the secure variant of os.MkdirAll.
//
// Unlike the regular version this implementation does not follow any symbolic
// links. At all times the new directory segment is created using mkdirat(2)
// while holding an open file descriptor to the parent directory.
//
// The only handled error is mkdirat(2) that fails with EEXIST. All other
// errors are fatal but there is no attempt to undo anything that was created.
//
// The uid and gid are used for the fchown(2) system call which is performed
// after each segment is created and opened. The special value -1 may be used
// to request that ownership is not changed.
func (sec *Secure) MkdirAll(path string, perm os.FileMode, uid sys.UserID, gid sys.GroupID) error {
	if path != filepath.Clean(path) {
		// TODO: Reword the error and adjust the tests.
		return fmt.Errorf("cannot split unclean path %q", path)
	}
	// Only support absolute paths to avoid bugs in snap-confine when
	// called from anywhere.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("cannot create directory with relative path: %q", path)
	}
	base, name := filepath.Split(path)
	base = filepath.Clean(base) // Needed to chomp the trailing slash.

	// Create the prefix.
	dirFd, err := sec.MkPrefix(base, perm, uid, gid)
	if err != nil {
		return err
	}
	defer sysClose(dirFd)

	if name != "" {
		// Create the leaf as a directory.
		leafFd, err := sec.MkDir(dirFd, base, name, perm, uid, gid)
		if err != nil {
			return err
		}
		defer sysClose(leafFd)
	}

	return nil
}

// MkfileAll is a secure implementation of "mkdir -p $(dirname $1) && touch $1".
//
// This function is like MkdirAll but it creates an empty file instead of
// a directory for the final path component. Each created directory component
// is chowned to the desired user and group.
func (sec *Secure) MkfileAll(path string, perm os.FileMode, uid sys.UserID, gid sys.GroupID) error {
	if path != filepath.Clean(path) {
		// TODO: Reword the error and adjust the tests.
		return fmt.Errorf("cannot split unclean path %q", path)
	}
	// Only support absolute paths to avoid bugs in snap-confine when
	// called from anywhere.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("cannot create file with relative path: %q", path)
	}
	// Only support file names, not directory names.
	if strings.HasSuffix(path, "/") {
		return fmt.Errorf("cannot create non-file path: %q", path)
	}
	base, name := filepath.Split(path)
	base = filepath.Clean(base) // Needed to chomp the trailing slash.

	// Create the prefix.
	dirFd, err := sec.MkPrefix(base, perm, uid, gid)
	if err != nil {
		return err
	}
	defer sysClose(dirFd)

	if name != "" {
		// Create the leaf as a file.
		err = sec.MkFile(dirFd, base, name, perm, uid, gid)
	}
	return err
}

// MksymlinkAll is a secure implementation of "ln -s".
func (sec *Secure) MksymlinkAll(path string, perm os.FileMode, uid sys.UserID, gid sys.GroupID, oldname string) error {
	if path != filepath.Clean(path) {
		// TODO: Reword the error and adjust the tests.
		return fmt.Errorf("cannot split unclean path %q", path)
	}
	// Only support absolute paths to avoid bugs in snap-confine when
	// called from anywhere.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("cannot create symlink with relative path: %q", path)
	}
	// Only support file names, not directory names.
	if strings.HasSuffix(path, "/") {
		return fmt.Errorf("cannot create non-file path: %q", path)
	}
	if oldname == "" {
		return fmt.Errorf("cannot create symlink with empty target: %q", path)
	}

	base, name := filepath.Split(path)
	base = filepath.Clean(base) // Needed to chomp the trailing slash.

	// Create the prefix.
	dirFd, err := sec.MkPrefix(base, perm, uid, gid)
	if err != nil {
		return err
	}
	defer sysClose(dirFd)

	if name != "" {
		// Create the leaf as a symlink.
		err = sec.MkSymlink(dirFd, base, name, oldname)
	}
	return err
}

// planWritableMimic plans how to transform a given directory from read-only to writable.
//
// The algorithm is designed to be universally reversible so that it can be
// always de-constructed back to the original directory. The original directory
// is hidden by tmpfs and a subset of things that were present there originally
// is bind mounted back on top of empty directories or empty files. Symlinks
// are re-created directly. Devices and all other elements are not supported
// because they are forbidden in snaps for which this function is designed to
// be used with. Since the original directory is hidden the algorithm relies on
// a temporary directory where the original is bind-mounted during the
// progression of the algorithm.
func planWritableMimic(dir, neededBy string) ([]*Change, error) {
	// We need a place for "safe keeping" of what is present in the original
	// directory as we are about to attach a tmpfs there, which will hide
	// everything inside.
	logger.Debugf("create-writable-mimic %q", dir)
	safeKeepingDir := filepath.Join("/tmp/.snap/", dir)

	var changes []*Change

	// Stat the original directory to know which mode and ownership to
	// replicate on top of the tmpfs we are about to create below.
	var sb syscall.Stat_t
	if err := sysLstat(dir, &sb); err != nil {
		return nil, err
	}

	// Bind mount the original directory elsewhere for safe-keeping.
	changes = append(changes, &Change{
		Action: Mount, Entry: osutil.MountEntry{
			// NOTE: Here we recursively bind because we realized that not
			// doing so doesn't work on core devices which use bind mounts
			// extensively to construct writable spaces in /etc and /var and
			// elsewhere.
			//
			// All directories present in the original are also recursively
			// bind mounted back to their original location. To unmount this
			// contraption we use MNT_DETACH which frees us from having to
			// enumerate the mount table, unmount all the things (starting
			// with most nested).
			//
			// The undo logic handles rbind mounts and adds x-snapd.unbind
			// flag to them, which in turns translates to MNT_DETACH on
			// umount2(2) system call.
			Name: dir, Dir: safeKeepingDir, Options: []string{"rbind"}},
	})

	// Mount tmpfs over the original directory, hiding its contents.
	// The mounted tmpfs will mimic the mode and ownership of the original
	// directory.
	changes = append(changes, &Change{
		Action: Mount, Entry: osutil.MountEntry{
			Name: "tmpfs", Dir: dir, Type: "tmpfs",
			Options: []string{
				osutil.XSnapdSynthetic(),
				osutil.XSnapdNeededBy(neededBy),
				fmt.Sprintf("mode=%#o", sb.Mode&07777),
				fmt.Sprintf("uid=%d", sb.Uid),
				fmt.Sprintf("gid=%d", sb.Gid),
			},
		},
	})
	// Iterate over the items in the original directory (nothing is mounted _yet_).
	entries, err := ioutilReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, fi := range entries {
		ch := &Change{Action: Mount, Entry: osutil.MountEntry{
			Name: filepath.Join(safeKeepingDir, fi.Name()),
			Dir:  filepath.Join(dir, fi.Name()),
		}}
		// Bind mount each element from the safe-keeping directory into the
		// tmpfs. Our Change.Perform() engine can create the missing
		// directories automatically so we don't bother creating those.
		m := fi.Mode()
		switch {
		case m.IsDir():
			ch.Entry.Options = []string{"rbind"}
		case m.IsRegular():
			ch.Entry.Options = []string{"bind", osutil.XSnapdKindFile()}
		case m&os.ModeSymlink != 0:
			if target, err := osReadlink(filepath.Join(dir, fi.Name())); err == nil {
				ch.Entry.Options = []string{osutil.XSnapdKindSymlink(), osutil.XSnapdSymlink(target)}
			} else {
				continue
			}
		default:
			logger.Noticef("skipping unsupported file %s", fi)
			continue
		}
		ch.Entry.Options = append(ch.Entry.Options, osutil.XSnapdSynthetic())
		ch.Entry.Options = append(ch.Entry.Options, osutil.XSnapdNeededBy(neededBy))
		changes = append(changes, ch)
	}
	// Finally unbind the safe-keeping directory as we don't need it anymore.
	changes = append(changes, &Change{
		Action: Unmount, Entry: osutil.MountEntry{Name: "none", Dir: safeKeepingDir, Options: []string{osutil.XSnapdDetach()}},
	})
	return changes, nil
}

// FatalError is an error that we cannot correct.
type FatalError struct {
	error
}

// execWritableMimic executes the plan for a writable mimic.
// The result is a transformed mount namespace and a set of fake mount changes
// that only exist in order to undo the plan.
//
// Certain assumptions are made about the plan, it must closely resemble that
// created by planWritableMimic, in particular the sequence must look like this:
//
// - bind a directory aside into safekeeping location
// - cover the original with tmpfs
// - bind mount something from safekeeping location to an empty file or
//   directory in the tmpfs; this step can repeat any number of times
// - unbind the safekeeping location
//
// Apart from merely executing the plan a fake plan is returned for undo. The
// undo plan skips the following elements as compared to the original plan:
//
// - the initial bind mount that constructs the safekeeping directory is gone
// - the final unmount that removes the safekeeping directory
// - the source of each of the bind mounts that re-populate tmpfs.
//
// In the event of a failure the undo plan is executed and an error is
// returned. If the undo plan fails the function returns a FatalError as it
// cannot fix the system from an inconsistent state.
func execWritableMimic(plan []*Change, sec *Secure) ([]*Change, error) {
	undoChanges := make([]*Change, 0, len(plan)-2)
	for i, change := range plan {
		if _, err := changePerform(change, sec); err != nil {
			// Drat, we failed! Let's undo everything according to our own undo
			// plan, by following it in reverse order.

			recoveryUndoChanges := make([]*Change, 0, len(undoChanges)+1)
			if i > 0 {
				// The undo plan doesn't contain the entry for the initial bind
				// mount of the safe keeping directory but we have already
				// performed it. For this recovery phase we need to insert that
				// in front of the undo plan manually.
				recoveryUndoChanges = append(recoveryUndoChanges, plan[0])
			}
			recoveryUndoChanges = append(recoveryUndoChanges, undoChanges...)

			for j := len(recoveryUndoChanges) - 1; j >= 0; j-- {
				recoveryUndoChange := recoveryUndoChanges[j]
				// All the changes mount something, we need to reverse that.
				// The "undo plan" is "a plan that can be undone" not "the plan
				// for how to undo" so we need to flip the actions.
				recoveryUndoChange.Action = Unmount
				if recoveryUndoChange.Entry.OptBool("rbind") {
					recoveryUndoChange.Entry.Options = append(recoveryUndoChange.Entry.Options, osutil.XSnapdDetach())
				}
				if _, err2 := changePerform(recoveryUndoChange, sec); err2 != nil {
					// Drat, we failed when trying to recover from an error.
					// We cannot do anything at this stage.
					return nil, &FatalError{error: fmt.Errorf("cannot undo change %q while recovering from earlier error %v: %v", recoveryUndoChange, err, err2)}
				}
			}
			return nil, err
		}
		if i == 0 || i == len(plan)-1 {
			// Don't represent the initial and final changes in the undo plan.
			// The initial change is the safe-keeping bind mount, the final
			// change is the safe-keeping unmount.
			continue
		}
		if change.Entry.XSnapdKind() == "symlink" {
			// Don't represent symlinks in the undo plan. They are removed when
			// the tmpfs is unmounted.
			continue

		}
		// Store an undo change for the change we just performed.
		undoOpts := change.Entry.Options
		if change.Entry.OptBool("rbind") {
			undoOpts = make([]string, 0, len(change.Entry.Options)+1)
			undoOpts = append(undoOpts, change.Entry.Options...)
			undoOpts = append(undoOpts, "x-snapd.detach")
		}
		undoChange := &Change{
			Action: Mount,
			Entry:  osutil.MountEntry{Dir: change.Entry.Dir, Name: change.Entry.Name, Type: change.Entry.Type, Options: undoOpts},
		}
		// Because of the use of a temporary bind mount (aka the safe-keeping
		// directory) we cannot represent bind mounts fully (the temporary bind
		// mount is unmounted as the last stage of this process). For that
		// reason let's hide the original location and overwrite it so to
		// appear as if the directory was a bind mount over itself. This is not
		// fully true (it is a bind mount from the old self to the new empty
		// directory or file in the same path, with the tmpfs in place already)
		// but this is closer to the truth and more in line with the idea that
		// this is just a plan for undoing the operation.
		if undoChange.Entry.OptBool("bind") || undoChange.Entry.OptBool("rbind") {
			undoChange.Entry.Name = undoChange.Entry.Dir
		}
		undoChanges = append(undoChanges, undoChange)
	}
	return undoChanges, nil
}

func createWritableMimic(dir, neededBy string, sec *Secure) ([]*Change, error) {
	plan, err := planWritableMimic(dir, neededBy)
	if err != nil {
		return nil, err
	}
	changes, err := execWritableMimic(plan, sec)
	if err != nil {
		return nil, err
	}
	return changes, nil
}
