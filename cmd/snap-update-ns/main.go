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

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var opts struct {
	FromSnapConfine bool `long:"from-snap-confine"`
	UserMounts      bool `long:"user-mounts"`
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
			logger.Debugf("no preserved mount namespace, nothing to update")
			return nil
		}
		return err
	}
	// END IMPORTANT

	if err := parseArgs(os.Args[1:]); err != nil {
		return err
	}

	if opts.UserMounts {
		return applyUserFstab(opts.Positionals.SnapName)
	}
	return applySystemFstab(opts.Positionals.SnapName, opts.FromSnapConfine)
}

func applyProfile(snapName string, currentBefore, desired *osutil.MountProfile, as *Assumptions) (*osutil.MountProfile, error) {
	// Compute the needed changes and perform each change if
	// needed, collecting those that we managed to perform or that
	// were performed already.
	changesNeeded := NeededChanges(currentBefore, desired)
	debugShowChanges(changesNeeded, "mount changes needed")

	logger.Debugf("performing mount changes:")
	var changesMade []*Change
	for _, change := range changesNeeded {
		logger.Debugf("\t * %s", change)
		synthesised, err := changePerform(change, as)
		changesMade = append(changesMade, synthesised...)
		if len(synthesised) > 0 {
			logger.Debugf("\tsynthesised additional mount changes:")
			for _, synth := range synthesised {
				logger.Debugf(" * \t\t%s", synth)
			}
		}
		if err != nil {
			// We may have done something even if Perform itself has
			// failed. We need to collect synthesized changes and
			// store them.
			origin := change.Entry.XSnapdOrigin()
			if origin == "layout" || origin == "overname" {
				return nil, err
			} else if err != ErrIgnoredMissingMount {
				logger.Noticef("cannot change mount namespace of snap %q according to change %s: %s", snapName, change, err)
			}
			continue
		}

		changesMade = append(changesMade, change)
	}

	// Compute the new current profile so that it contains only changes that were made
	// and save it back for next runs.
	var currentAfter osutil.MountProfile
	for _, change := range changesMade {
		if change.Action == Mount || change.Action == Keep {
			currentAfter.Entries = append(currentAfter.Entries, change.Entry)
		}
	}
	debugShowProfile(&currentAfter, "current mount profile (after applying changes)")
	return &currentAfter, nil
}
