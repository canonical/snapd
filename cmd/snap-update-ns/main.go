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
	"github.com/snapcore/snapd/snap"
)

var opts struct {
	Positionals struct {
		SnapName string `positional-arg-name:"SNAP_NAME" required:"yes"`
	} `positional-args:"true"`
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("cannot update snap namespace: %s\n", err)
		os.Exit(1)
	}
}

func parseArgs(args []string) error {
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	if _, err := parser.ParseArgs(args); err != nil {
		return err
	}
	return snap.ValidateName(opts.Positionals.SnapName)
}

func run() error {
	if err := parseArgs(os.Args[1:]); err != nil {
		return err
	}

	// There is some C code that runs before main() is started.
	// That code always runs and sets an error condition if it fails.
	// Here we just check for the error.
	if err := BootstrapError(); err != nil {
		// If there is no mount namespace to transition to let's just quit
		// instantly without any errors as there is nothing to do anymore.
		if err == ErrNoNS {
			return nil
		}
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
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("cannot lock mount namespace of snap %q: %s", snapName, err)
	}

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
	changesNeeded := mount.NeededChanges(currentBefore, desired)
	var changesMade []mount.Change
	for _, change := range changesNeeded {
		if change.Action == mount.Keep {
			changesMade = append(changesMade, change)
			continue
		}
		// Read mount info each time as our operations may have unexpected
		// consequences and we want to know the real state of what is mounted
		// at each iteration.
		mounted, err := mount.LoadMountInfo(mount.ProcSelfMountInfo)
		if err != nil {
			return fmt.Errorf("cannot read mount-info table: %s", err)
		}
		if !change.Needed(mounted) {
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
		if change.Action == mount.Mount || change.Action == mount.Keep {
			currentAfter.Entries = append(currentAfter.Entries, change.Entry)
		}
	}
	if err := currentAfter.Save(currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", snapName, err)
	}
	return nil
}
