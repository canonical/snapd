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

	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
)

// not available through syscall
const (
	umountNoFollow = 8
)

// For mocking everything during testing.
var (
	osLstat    = os.Lstat
	osReadlink = os.Readlink

	sysClose   = syscall.Close
	sysMkdirat = syscall.Mkdirat
	sysMount   = syscall.Mount
	sysOpen    = syscall.Open
	sysOpenat  = syscall.Openat
	sysUnmount = syscall.Unmount
	sysFchown  = syscall.Fchown

	ioutilReadDir = ioutil.ReadDir
)

// ReadOnlyFsError is an error encapsulating encountered EROFS.
type ReadOnlyFsError struct {
	Path string
}

func (e *ReadOnlyFsError) Error() string {
	return fmt.Sprintf("cannot operate on read-only filesystem at %s", e.Path)
}

// Create directories for all but the last segments and return the file
// descriptor to the leaf directory. This function is a base for secure
// variants of mkdir, touch and symlink.
func secureMkPrefix(segments []string, perm os.FileMode, uid, gid int) (int, error) {
	logger.Debugf("secure-mk-prefix %q %v %d %d -> ...", segments, perm, uid, gid)

	// Declare var and don't assign-declare below to ensure we don't swallow
	// any errors by mistake.
	var err error
	var fd int

	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_DIRECTORY

	// Open the root directory and start there.
	fd, err = sysOpen("/", openFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("cannot open root directory: %v", err)
	}
	if len(segments) > 1 {
		defer sysClose(fd)
	}

	if len(segments) > 0 {
		// Process all but the last segment.
		for i := range segments[:len(segments)-1] {
			fd, err = secureMkDir(fd, segments, i, perm, uid, gid)
			if err != nil {
				return -1, err
			}
			// Keep the final FD open (caller needs to close it).
			if i < len(segments)-2 {
				defer sysClose(fd)
			}
		}
	}

	logger.Debugf("secure-mk-prefix %q %v %d %d -> %d", segments, perm, uid, gid, fd)
	return fd, nil
}

// secureMkDir creates a directory at i-th entry of absolute path represented
// by segments. This function can be used to construct subsequent elements of
// the constructed path. The return value contains the newly created file
// descriptor or -1 on error.
func secureMkDir(fd int, segments []string, i int, perm os.FileMode, uid, gid int) (int, error) {
	logger.Debugf("secure-mk-dir %d %q %d %v %d %d -> ...", fd, segments, i, perm, uid, gid)

	segment := segments[i]
	made := true
	var err error
	var newFd int

	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_DIRECTORY

	if err = sysMkdirat(fd, segment, uint32(perm.Perm())); err != nil {
		switch err {
		case syscall.EEXIST:
			made = false
		case syscall.EROFS:
			// Treat EROFS specially: this is a hint that we have to poke a
			// hole using tmpfs. The path below is the location where we
			// need to poke the hole.
			p := "/" + strings.Join(segments[:i], "/")
			return -1, &ReadOnlyFsError{Path: p}
		default:
			return -1, fmt.Errorf("cannot mkdir path segment %q: %v", segment, err)
		}
	}
	newFd, err = sysOpenat(fd, segment, openFlags, 0)
	if err != nil {
		return -1, fmt.Errorf("cannot open path segment %q (got up to %q): %v", segment,
			"/"+strings.Join(segments[:i], "/"), err)
	}
	if made {
		// Chown each segment that we made.
		if err := sysFchown(newFd, uid, gid); err != nil {
			// Close the FD we opened if we fail here since the caller will get
			// an error and won't assume responsibility for the FD.
			sysClose(newFd)
			return -1, fmt.Errorf("cannot chown path segment %q to %d.%d (got up to %q): %v", segment, uid, gid,
				"/"+strings.Join(segments[:i], "/"), err)
		}
	}
	logger.Debugf("secure-mk-dir %d %q %d %v %d %d -> %d", fd, segments, i, perm, uid, gid, newFd)
	return newFd, err
}

// secureMkFile creates a file at i-th entry of absolute path represented by
// segments. This function is meant to be used to create the leaf file as a
// preparation for a mount point. Existing files are reused without errors.
// Newly created files have the specified mode and ownership.
func secureMkFile(fd int, segments []string, i int, perm os.FileMode, uid, gid int) error {
	logger.Debugf("secure-mk-file %d %q %d %v %d %d", fd, segments, i, perm, uid, gid)
	segment := segments[i]
	made := true
	var newFd int
	var err error

	// NOTE: Tests don't show O_RDONLY as has a value of 0 and is not
	// translated to textual form. It is added here for explicitness.
	const openFlags = syscall.O_NOFOLLOW | syscall.O_CLOEXEC | syscall.O_RDONLY

	// Open the final path segment as a file. Try to create the file (so that
	// we know if we need to chown it) but fall back to just opening an
	// existing one.

	newFd, err = sysOpenat(fd, segment, openFlags|syscall.O_CREAT|syscall.O_EXCL, uint32(perm.Perm()))
	if err != nil {
		switch err {
		case syscall.EEXIST:
			// If the file exists then just open it without O_CREAT and O_EXCL
			newFd, err = sysOpenat(fd, segment, openFlags, 0)
			if err != nil {
				return fmt.Errorf("cannot open file %q: %v", segment, err)
			}
			made = false
		case syscall.EROFS:
			// Treat EROFS specially: this is a hint that we have to poke a
			// hole using tmpfs. The path below is the location where we
			// need to poke the hole.
			p := "/" + strings.Join(segments[:i], "/")
			return &ReadOnlyFsError{Path: p}
		default:
			return fmt.Errorf("cannot open file %q: %v", segment, err)
		}
	}
	defer sysClose(newFd)

	if made {
		// Chown the file if we made it.
		if err := sysFchown(newFd, uid, gid); err != nil {
			return fmt.Errorf("cannot chown file %q to %d.%d: %v", segment, uid, gid, err)
		}
	}

	return nil
}

func splitIntoSegments(name string) ([]string, error) {
	if name != filepath.Clean(name) {
		return nil, fmt.Errorf("cannot split unclean path %q", name)
	}
	segments := strings.FieldsFunc(filepath.Clean(name), func(c rune) bool { return c == '/' })
	return segments, nil
}

// SecureMkdirAll is the secure variant of os.MkdirAll.
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
func secureMkdirAll(name string, perm os.FileMode, uid, gid int) error {
	logger.Debugf("secure-mkdir-all %q %v %d %d", name, perm, uid, gid)

	// Only support absolute paths to avoid bugs in snap-confine when
	// called from anywhere.
	if !filepath.IsAbs(name) {
		return fmt.Errorf("cannot create directory with relative path: %q", name)
	}

	// Split the path into segments.
	segments, err := splitIntoSegments(name)
	if err != nil {
		return err
	}

	// Create the prefix.
	fd, err := secureMkPrefix(segments, perm, uid, gid)
	if err != nil {
		return err
	}
	defer sysClose(fd)

	if len(segments) > 0 {
		// Create the final segment as a directory.
		fd, err = secureMkDir(fd, segments, len(segments)-1, perm, uid, gid)
		if err != nil {
			return err
		}
		defer sysClose(fd)
	}

	return nil
}

// secureMkfileAll is a secure implementation of "mkdir -p $(dirname $1) && touch $1".
//
// This function is like secureMkdirAll but it creates an empty file instead of
// a directory for the final path component. Each created directory component
// is chowned to the desired user and group.
func secureMkfileAll(name string, perm os.FileMode, uid, gid int) error {
	logger.Debugf("secure-mkfile-all %q %q %d %d", name, perm, uid, gid)

	// Only support absolute paths to avoid bugs in snap-confine when
	// called from anywhere.
	if !filepath.IsAbs(name) {
		return fmt.Errorf("cannot create file with relative path: %q", name)
	}
	// Only support file names, not directory names.
	if strings.HasSuffix(name, "/") {
		return fmt.Errorf("cannot create non-file path: %q", name)
	}

	// Split the path into segments.
	segments, err := splitIntoSegments(name)
	if err != nil {
		return err
	}

	// Create the prefix.
	fd, err := secureMkPrefix(segments, perm, uid, gid)
	if err != nil {
		return err
	}
	defer sysClose(fd)

	if len(segments) > 0 {
		// Create the final segment as a file.
		err = secureMkFile(fd, segments, len(segments)-1, perm, uid, gid)
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
func planWritableMimic(dir string) ([]*Change, error) {
	// We need a place for "safe keeping" of what is present in the original
	// directory as we are about to attach a tmpfs there, which will hide
	// everything inside.
	logger.Debugf("create-writable-mimic %q", dir)
	safeKeepingDir := filepath.Join("/tmp/.snap/", dir)

	var changes []*Change

	// Bind mount the original directory elsewhere for safe-keeping.
	changes = append(changes, &Change{
		Action: Mount, Entry: mount.Entry{
			// NOTE: Here we bind instead of recursively binding
			// because recursive binds cannot be undone without
			// parsing the mount table and exploring what is really
			// there and this is not how the undo logic is
			// designed.
			Name: dir, Dir: safeKeepingDir, Options: []string{"bind"}},
	})
	// Mount tmpfs over the original directory, hiding its contents.
	changes = append(changes, &Change{
		Action: Mount, Entry: mount.Entry{Name: "none", Dir: dir, Type: "tmpfs"},
	})
	// Iterate over the items in the original directory (nothing is mounted _yet_).
	entries, err := ioutilReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, fi := range entries {
		ch := &Change{Action: Mount, Entry: mount.Entry{
			Name:    filepath.Join(safeKeepingDir, fi.Name()),
			Dir:     filepath.Join(dir, fi.Name()),
			Options: []string{"bind"},
		}}
		// Bind mount each element from the safe-keeping directory into the
		// tmpfs. Our Change.Perform() engine can create the missing
		// directories automatically so we don't bother creating those.
		m := fi.Mode()
		switch {
		case m.IsDir():
			changes = append(changes, ch)
		case m.IsRegular():
			ch.Entry.Options = append(ch.Entry.Options, "x-snapd.kind=file")
			changes = append(changes, ch)
		case m&os.ModeSymlink != 0:
			if target, err := osReadlink(filepath.Join(dir, fi.Name())); err == nil {
				ch.Entry.Options = append(ch.Entry.Options, "x-snapd.kind=symlink", fmt.Sprintf("x-snapd.symlink=%s", target))
				changes = append(changes, ch)
			}
		default:
			logger.Noticef("skipping unsupported file %s", fi)
		}
	}
	// Finally unbind the safe-keeping directory as we don't need it anymore.
	changes = append(changes, &Change{
		Action: Unmount, Entry: mount.Entry{Name: "none", Dir: safeKeepingDir},
	})
	return changes, nil
}

func ensureMountPoint(path string, mode os.FileMode, uid int, gid int) error {
	// If the mount point is not present then create a directory in its
	// place.  This is very naive, doesn't handle read-only file systems
	// but it is a good starting point for people working with things like
	// $SNAP_DATA/subdirectory.
	//
	// We use lstat to ensure that we don't follow the symlink in case one
	// was set up by the snap. Note that at the time this is run, all the
	// snap's processes are frozen.
	fi, err := osLstat(path)
	switch {
	case err != nil && os.IsNotExist(err):
		return secureMkdirAll(path, mode, uid, gid)
	case err != nil:
		return fmt.Errorf("cannot inspect %q: %v", path, err)
	case err == nil:
		// Ensure that mount point is a directory.
		if !fi.IsDir() {
			return fmt.Errorf("cannot use %q for mounting, not a directory", path)
		}
	}
	return nil
}
