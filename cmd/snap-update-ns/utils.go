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
)

// not available through syscall
const (
	umountNoFollow = 8
)

// For mocking everything during testing.
var (
	osLstat    = os.Lstat
	osReadlink = os.Readlink
	osRemove   = os.Remove

	sysClose   = syscall.Close
	sysMkdirat = syscall.Mkdirat
	sysMount   = syscall.Mount
	sysOpen    = syscall.Open
	sysOpenat  = syscall.Openat
	sysUnmount = syscall.Unmount
	sysFchown  = sys.Fchown
	sysSymlink = syscall.Symlink

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
func secureMkPrefix(segments []string, perm os.FileMode, uid sys.UserID, gid sys.GroupID) (int, error) {
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
func secureMkDir(fd int, segments []string, i int, perm os.FileMode, uid sys.UserID, gid sys.GroupID) (int, error) {
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
func secureMkFile(fd int, segments []string, i int, perm os.FileMode, uid sys.UserID, gid sys.GroupID) error {
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

// secureMkdirAll is the secure variant of os.MkdirAll.
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
func secureMkdirAll(name string, perm os.FileMode, uid sys.UserID, gid sys.GroupID) error {
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
func secureMkfileAll(name string, perm os.FileMode, uid sys.UserID, gid sys.GroupID) error {
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

func secureMklinkAll(name string, perm os.FileMode, uid sys.UserID, gid sys.GroupID, oldname string) error {
	parent := filepath.Dir(name)
	err := secureMkdirAll(parent, perm, uid, gid)
	if err != nil {
		return err
	}
	// TODO: roll this uber securely like the code above does using linkat(2).
	err = sysSymlink(oldname, name)
	if err == syscall.EROFS {
		return &ReadOnlyFsError{Path: parent}
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
		Action: Mount, Entry: osutil.MountEntry{
			// NOTE: Here we recursively bind because we realized that not
			// doing so doesn't work work on core devices which use bind
			// mounts extensively to construct writable spaces in /etc and
			// /var and elsewhere.
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
	changes = append(changes, &Change{
		Action: Mount, Entry: osutil.MountEntry{Name: "tmpfs", Dir: dir, Type: "tmpfs"},
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
			changes = append(changes, ch)
		case m.IsRegular():
			ch.Entry.Options = []string{"bind", "x-snapd.kind=file"}
			changes = append(changes, ch)
		case m&os.ModeSymlink != 0:
			if target, err := osReadlink(filepath.Join(dir, fi.Name())); err == nil {
				ch.Entry.Options = []string{"x-snapd.kind=symlink", fmt.Sprintf("x-snapd.symlink=%s", target)}
				changes = append(changes, ch)
			}
		default:
			logger.Noticef("skipping unsupported file %s", fi)
		}
	}
	// Finally unbind the safe-keeping directory as we don't need it anymore.
	changes = append(changes, &Change{
		Action: Unmount, Entry: osutil.MountEntry{Name: "none", Dir: safeKeepingDir, Options: []string{"x-snapd.detach"}},
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
func execWritableMimic(plan []*Change) ([]*Change, error) {
	undoChanges := make([]*Change, 0, len(plan)-2)
	for i, change := range plan {
		if _, err := changePerform(change); err != nil {
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
					recoveryUndoChange.Entry.Options = append(recoveryUndoChange.Entry.Options, "x-snapd.detach")
				}
				if _, err2 := changePerform(recoveryUndoChange); err2 != nil {
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
		if kind, _ := change.Entry.OptStr("x-snapd.kind"); kind == "symlink" {
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

func createWritableMimic(dir string) ([]*Change, error) {
	plan, err := planWritableMimic(dir)
	if err != nil {
		return nil, err
	}
	changes, err := execWritableMimic(plan)
	if err != nil {
		return nil, err
	}
	return changes, nil
}
