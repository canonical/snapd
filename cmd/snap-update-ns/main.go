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
		return err
	}
	snapName := opts.Positionals.SnapName

	// Lock the mount namespace so that any concurrently attempted invocations
	// of snap-confine are synchronized and will see consistent state.
	lock, err := mount.OpenLock(snapName)
	if err != nil {
		return fmt.Errorf("cannot open mount namespace lock file: %s", err)
	}
	if err := lock.Lock(); err != nil {
		return fmt.Errorf("cannot lock mount namespace: %s", err)
	}
	defer lock.Close()

	// Read the desired and current mount profiles. Note that missing files
	// count as empty profiles so that we can gracefully handle a mount
	// interface connection/disconnection.
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)
	desired, err := mount.LoadProfile(desiredProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load desired mount profile: %s", err)
	}
	fmt.Fprintf(os.Stderr, "DESIRED:\n")
	for _, entry := range desired.Entries {
		fmt.Fprintf(os.Stderr, " - %s\n", entry)
	}

	currentProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
	current, err := mount.LoadProfile(currentProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load current mount profile: %s", err)
	}
	fmt.Fprintf(os.Stderr, "CURRENT (before):\n")
	for _, entry := range current.Entries {
		fmt.Fprintf(os.Stderr, " - %s\n", entry)
	}

	// Compute the needed changes and perform each change if needed, collecting
	// those that we managed to perform or that were performed already.
	changesNeeded := mount.NeededChanges(current, desired)
	fmt.Fprintf(os.Stderr, "CHANGES NEEDED:\n")
	for _, change := range changesNeeded {
		fmt.Fprintf(os.Stderr, " - %s\n", change)
	}
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
		fmt.Printf("%s\n", change)
		if err := change.Perform(); err != nil {
			logger.Noticef("cannot perform mount change %s: %s", change, err)
			continue
		}
		changesMade = append(changesMade, change)
	}
	fmt.Fprintf(os.Stderr, "CHANGES MADE:\n")
	for _, change := range changesMade {
		fmt.Fprintf(os.Stderr, " - %s\n", change)
	}

	// Compute the new current profile so that it contains only changes that were made
	// and save it back for next runs.
	current = &mount.Profile{}
	for _, change := range changesMade {
		if change.Action == mount.Mount || change.Action == mount.Keep {
			current.Entries = append(current.Entries, change.Entry)
		}
	}
	fmt.Fprintf(os.Stderr, "CURRENT (after):\n")
	for _, entry := range current.Entries {
		fmt.Fprintf(os.Stderr, " - %s\n", entry)
	}
	if err := current.Save(currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile: %s", err)
	}
	return nil
}
