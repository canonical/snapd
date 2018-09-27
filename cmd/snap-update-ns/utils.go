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
	"os"
	"path/filepath"
	"syscall"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
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
	if err := osutil.SysLstat(dir, &sb); err != nil {
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
	entries, err := osutil.IoutilReadDir(dir)
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
			if target, err := osutil.OsReadlink(filepath.Join(dir, fi.Name())); err == nil {
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
func execWritableMimic(plan []*Change, as *Assumptions) ([]*Change, error) {
	undoChanges := make([]*Change, 0, len(plan)-2)
	for i, change := range plan {
		if _, err := changePerform(change, as); err != nil {
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
				if _, err2 := changePerform(recoveryUndoChange, as); err2 != nil {
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

func createWritableMimic(dir, neededBy string, as *Assumptions) ([]*Change, error) {
	plan, err := planWritableMimic(dir, neededBy)
	if err != nil {
		return nil, err
	}
	changes, err := execWritableMimic(plan, as)
	if err != nil {
		return nil, err
	}
	return changes, nil
}
