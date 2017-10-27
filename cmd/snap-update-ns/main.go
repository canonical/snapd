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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var opts struct {
	FromSnapConfine bool `long:"from-snap-confine"`
	Positionals     struct {
		SnapName string `positional-arg-name:"SNAP_NAME" required:"yes"`
	} `positional-args:"true"`
}

// IMPORTANT: all the code in main() until bootstrap is finished may be run
// with elevated privileges when invoking snap-update-ns from the setuid
// snap-confine.

func main() {
	logger.SimpleSetup()
	if err := run(); err != nil {
		fmt.Printf("cannot update snap namespace: %s\n", err)
		os.Exit(1)
	}
	// END IMPORTANT
}

func parseArgs(args []string) error {
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	_, err := parser.ParseArgs(args)
	return err
}

// IMPORTANT: all the code in run() until BootStrapError() is finished may
// be run with elevated privileges when invoking snap-update-ns from
// the setuid snap-confine.

func run() error {
	// There is some C code that runs before main() is started.
	// That code always runs and sets an error condition if it fails.
	// Here we just check for the error.
	if err := BootstrapError(); err != nil {
		// If there is no mount namespace to transition to let's just quit
		// instantly without any errors as there is nothing to do anymore.
		if err == ErrNoNamespace {
			return nil
		}
		return err
	}
	// END IMPORTANT

	if err := parseArgs(os.Args[1:]); err != nil {
		return err
	}

	snapName := opts.Positionals.SnapName

	// Lock the mount namespace so that any concurrently attempted invocations
	// of snap-confine are synchronized and will see consistent state.
	lock, err := mount.OpenLock(snapName)
	if err != nil {
		return fmt.Errorf("cannot open lock file for mount namespace of snap %q: %s", snapName, err)
	}
	defer lock.Close()

	if opts.FromSnapConfine {
		// When --from-snap-confine is passed then we just ensure that the
		// namespace is locked. This is used by snap-confine to use
		// snap-update-ns to apply mount profiles.
		if err := lock.TryLock(); err != osutil.ErrAlreadyLocked {
			return fmt.Errorf("mount namespace of snap %q is not locked but --from-snap-confine was used", snapName)
		}
	} else {
		if err := lock.Lock(); err != nil {
			return fmt.Errorf("cannot lock mount namespace of snap %q: %s", snapName, err)
		}
	}

	// Freeze the mount namespace and unfreeze it later. This lets us perform
	// modifications without snap processes attempting to construct
	// symlinks or perform other malicious activity (such as attempting to
	// introduce a symlink that would cause us to mount something other
	// than what we expected).
	if err := freezeSnapProcesses(opts.Positionals.SnapName); err != nil {
		return err
	}
	defer thawSnapProcesses(opts.Positionals.SnapName)

	// Read the desired and current mount profiles. Note that missing files
	// count as empty profiles so that we can gracefully handle a mount
	// interface connection/disconnection.
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)
	desired, err := mount.LoadProfile(desiredProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load desired mount profile of snap %q: %s", snapName, err)
	}

	currentProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
	currentBefore, err := mount.LoadProfile(currentProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load current mount profile of snap %q: %s", snapName, err)
	}

	// Compute the needed changes and perform each change if needed, collecting
	// those that we managed to perform or that were performed already.
	changesNeeded := NeededChanges(currentBefore, desired)
	var changesMade []Change
	for _, change := range changesNeeded {
		if change.Action == Keep {
			changesMade = append(changesMade, change)
			continue
		}
		if err := change.Perform(); err != nil {
			logger.Noticef("cannot change mount namespace of snap %q according to change %s: %s", snapName, change, err)
			continue
		}
		changesMade = append(changesMade, change)
	}

	// Compute the new current profile so that it contains only changes that were made
	// and save it back for next runs.
	var currentAfter mount.Profile
	for _, change := range changesMade {
		if change.Action == Mount || change.Action == Keep {
			currentAfter.Entries = append(currentAfter.Entries, change.Entry)
		}
	}
	if err := currentAfter.Save(currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", snapName, err)
	}
	return nil
}
