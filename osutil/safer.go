// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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

package osutil

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/strutil"
)

// not available through syscall
const (
	// unmountNoFollow is the equivalent of UMOUNT_NOFOLLOW
	umountNoFollow = 8
)

// ReadOnlyFsError is an error encapsulating encountered EROFS.
type ReadOnlyFsError struct {
	Path string
}

func (e *ReadOnlyFsError) Error() string {
	return fmt.Sprintf("cannot operate on read-only filesystem at %s", e.Path)
}

// TrespassingError is an error when filesystem operation would affect the host.
type TrespassingError struct {
	ViolatedPath string
	DesiredPath  string
}

// Error returns a formatted error message.
func (e *TrespassingError) Error() string {
	return fmt.Sprintf("cannot write to %q because it would affect the host in %q", e.DesiredPath, e.ViolatedPath)
}

// Assumptions track the assumptions about the state of the filesystem.
//
// Assumptions constitute the global part of the write restriction management.
// Assumptions are global in the sense that they span multiple distinct write
// operations. In contrast, Restrictions track per-operation state.
type Assumptions interface {
	RestrictionsFor(desiredPath string) *Restrictions
	CanWriteToDirectory(dirFd int, dirName string) (bool, error)
}

// Restrictions contains meta-data of a compound write operation.
//
// This structure helps functions that write to the filesystem to keep track of
// the ultimate destination across several calls (e.g. the function that
// creates a file needs to call helpers to create subsequent directories).
// Keeping track of the desired path aids in constructing useful error
// messages.
//
// In addition the structure keeps track of the restricted write mode flag which
// is based on the full path of the desired object being constructed. This allows
// various write helpers to avoid trespassing on host filesystem in places that
// are not expected to be written to by snapd (e.g. outside of $SNAP_DATA).
type Restrictions struct {
	assumptions Assumptions
	desiredPath string
	restricted  bool
}

func MakeRestrictions(as Assumptions, path string) *Restrictions {
	return &Restrictions{
		assumptions: as,
		desiredPath: path,
		restricted:  true,
	}
}

// Check verifies whether writing to a directory would trespass on the host.
//
// The check is only performed in restricted mode. If the check fails a
// TrespassingError is returned.
func (rs *Restrictions) Check(dirFd int, dirName string) error {
	if rs == nil || !rs.restricted {
		return nil
	}
	// In restricted mode check the directory before attempting to write to it.
	ok, err := rs.assumptions.CanWriteToDirectory(dirFd, dirName)
	if ok || err != nil {
		return err
	}
	if dirName == "/" {
		// If writing to / is not allowed then we are in a tough spot because
		// we cannot construct a writable mimic over /. This should never
		// happen in normal circumstances because the root filesystem is some
		// kind of base snap.
		return fmt.Errorf("cannot recover from trespassing over /")
	}
	return &TrespassingError{ViolatedPath: dirName, DesiredPath: rs.desiredPath}
}

// Lift lifts write restrictions for the desired path.
//
// This function should be called when, as subsequent components of a path are
// either discovered or created, the conditions for using restricted mode are
// no longer true.
func (rs *Restrictions) Lift() {
	if rs != nil {
		rs.restricted = false
	}
}

// OpenPath creates a path file descriptor for the given
// path, making sure no components are symbolic links.
//
// The file descriptor is opened using the O_PATH, O_NOFOLLOW,
// and O_CLOEXEC flags.
func OpenPath(path string) (int, error) {
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
// returns the file descriptor to the leaf directory as well as the restricted
// flag. This function is a base for secure variants of mkdir, touch and
// symlink. None of the traversed directories can be symbolic links.
func MkPrefix(base string, perm os.FileMode, uid sys.UserID, gid sys.GroupID, rs *Restrictions) (int, error) {
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
	//
	// We don't have to check for possible trespassing on / here because we are
	// going to check for it in sec.MkDir call below which verifies that
	// trespassing restrictions are not violated.
	fd, err := sysOpen("/", openFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("cannot open root directory: %v", err)
	}
	for iter.Next() {
		// Keep closing the previous descriptor as we go, so that we have the
		// last one handy from the MkDir below.
		defer sysClose(fd)
		fd, err = MkDir(fd, iter.CurrentBase(), iter.CurrentCleanName(), perm, uid, gid, rs)
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
func MkDir(dirFd int, dirName string, name string, perm os.FileMode, uid sys.UserID, gid sys.GroupID, rs *Restrictions) (int, error) {
	if err := rs.Check(dirFd, dirName); err != nil {
		return -1, err
	}

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
		// As soon as we find a place that is safe to write we can switch off
		// the restricted mode (and thus any subsequent checks). This is
		// because we only allow "writing" to read-only filesystems where
		// writes fail with EROFS or to a tmpfs that snapd has privately
		// mounted inside the per-snap mount namespace. As soon as we start
		// walking over such tmpfs any subsequent children are either read-
		// only bind mounts from $SNAP, other tmpfs'es  (e.g. one explicitly
		// constructed for a layout) or writable places that are bind-mounted
		// from $SNAP_DATA or similar.
		rs.Lift()
	}
	return newFd, err
}

// MkFile creates a file with a given name.
//
// The directory is represented with a file descriptor and its name (for
// convenience). This function is meant to be used to create the leaf file as
// a preparation for a mount point. Existing files are reused without errors.
// Newly created files have the specified mode and ownership.
func MkFile(dirFd int, dirName string, name string, perm os.FileMode, uid sys.UserID, gid sys.GroupID, rs *Restrictions) error {
	if err := rs.Check(dirFd, dirName); err != nil {
		return err
	}

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
func MkSymlink(dirFd int, dirName string, name string, oldname string, rs *Restrictions) error {
	if err := rs.Check(dirFd, dirName); err != nil {
		return err
	}

	// Create the final path segment as a symlink.
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
func MkdirAll(path string, perm os.FileMode, uid sys.UserID, gid sys.GroupID, rs *Restrictions) error {
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
	dirFd, err := MkPrefix(base, perm, uid, gid, rs)
	if err != nil {
		return err
	}
	defer sysClose(dirFd)

	if name != "" {
		// Create the leaf as a directory.
		leafFd, err := MkDir(dirFd, base, name, perm, uid, gid, rs)
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
func MkfileAll(path string, perm os.FileMode, uid sys.UserID, gid sys.GroupID, rs *Restrictions) error {
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
	dirFd, err := MkPrefix(base, perm, uid, gid, rs)
	if err != nil {
		return err
	}
	defer sysClose(dirFd)

	if name != "" {
		// Create the leaf as a file.
		err = MkFile(dirFd, base, name, perm, uid, gid, rs)
	}
	return err
}

// MksymlinkAll is a secure implementation of "ln -s".
func MksymlinkAll(path string, perm os.FileMode, uid sys.UserID, gid sys.GroupID, oldname string, rs *Restrictions) error {
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
	dirFd, err := MkPrefix(base, perm, uid, gid, rs)
	if err != nil {
		return err
	}
	defer sysClose(dirFd)

	if name != "" {
		// Create the leaf as a symlink.
		err = MkSymlink(dirFd, base, name, oldname, rs)
	}
	return err
}

// BindMount bind mounts two absolute paths containing no symlinks.
//
// The way the bind mount is performed is non-traditional. Both paths are
// opened carefully, ensuring that no symlinks are ever traversed. The
// resulting pair of file descriptors are used to perform the bind mount. As
// such the approach is immune to attack based on racing replacement of a path
// element with a symbolic link.
func BindMount(sourceDir, targetDir string, flags uint) error {
	// This function only attempts to handle bind mounts. Expanding to other
	// mounts will require examining do_mount() from fs/namespace.c of the
	// kernel that called functions (eventually) verify `DCACHE_CANT_MOUNT` is
	// not set (eg, by calling lock_mount()).
	if flags&syscall.MS_BIND == 0 {
		return fmt.Errorf("cannot perform non-bind mount operation")
	}

	// The kernel doesn't support recursively switching a tree of bind mounts
	// to read only, and we haven't written a work around.
	if flags&syscall.MS_RDONLY != 0 && flags&syscall.MS_REC != 0 {
		return fmt.Errorf("cannot use MS_RDONLY and MS_REC together")
	}

	// Step 1: acquire file descriptors representing the source and destination
	// directories, ensuring no symlinks are followed.
	sourceFd, err := OpenPath(sourceDir)
	if err != nil {
		return err
	}
	defer sysClose(sourceFd)
	targetFd, err := OpenPath(targetDir)
	if err != nil {
		return err
	}
	defer sysClose(targetFd)

	// Step 2: perform a bind mount between the paths identified by the two
	// file descriptors. We primarily care about privilege escalation here and
	// trying to race the sysMount() by removing any part of the dir (sourceDir
	// or targetDir) after we have an open file descriptor to it (sourceFd or
	// targetFd) to then replace an element of the dir's path with a symlink
	// will cause the fd path (ie, sourceFdPath or targetFdPath) to be marked
	// as unmountable within the kernel (this path is also changed to show as
	// '(deleted)'). Alternatively, simply renaming the dir (sourceDir or
	// targetDir) after we have an open file descriptor to it (sourceFd or
	// targetFd) causes the mount to happen with the newly renamed path, but
	// this rename is controlled by DAC so while the user could race the mount
	// source or target, this rename can't be used to gain privileged access to
	// files. For systems with AppArmor enabled, this raced rename would be
	// denied by the per-snap snap-update-ns AppArmor profle.
	sourceFdPath := fmt.Sprintf("/proc/self/fd/%d", sourceFd)
	targetFdPath := fmt.Sprintf("/proc/self/fd/%d", targetFd)
	bindFlags := syscall.MS_BIND | (flags & syscall.MS_REC)
	if err := sysMount(sourceFdPath, targetFdPath, "", uintptr(bindFlags), ""); err != nil {
		return err
	}

	// Step 3: optionally change to readonly
	if flags&syscall.MS_RDONLY != 0 {
		// We need to look up the target directory a second time, because
		// targetFd refers to the path shadowed by the mount point.
		mountFd, err := OpenPath(targetDir)
		if err != nil {
			// FIXME: the mount occurred, but the user moved the target
			// somewhere
			return err
		}
		defer sysClose(mountFd)
		mountFdPath := fmt.Sprintf("/proc/self/fd/%d", mountFd)
		remountFlags := syscall.MS_REMOUNT | syscall.MS_BIND | syscall.MS_RDONLY
		if err := sysMount("none", mountFdPath, "", uintptr(remountFlags), ""); err != nil {
			sysUnmount(mountFdPath, syscall.MNT_DETACH|umountNoFollow)
			return err
		}
	}
	return nil
}
