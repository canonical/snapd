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
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
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

func applySystemFstab(instanceName string, fromSnapConfine bool) error {
	// Lock the mount namespace so that any concurrently attempted invocations
	// of snap-confine are synchronized and will see consistent state.
	lock, err := mount.OpenLock(instanceName)
	if err != nil {
		return fmt.Errorf("cannot open lock file for mount namespace of snap %q: %s", instanceName, err)
	}
	defer func() {
		logger.Debugf("unlocking mount namespace of snap %q", instanceName)
		lock.Close()
	}()

	logger.Debugf("locking mount namespace of snap %q", instanceName)
	if fromSnapConfine {
		// When --from-snap-confine is passed then we just ensure that the
		// namespace is locked. This is used by snap-confine to use
		// snap-update-ns to apply mount profiles.
		if err := lock.TryLock(); err != osutil.ErrAlreadyLocked {
			return fmt.Errorf("mount namespace of snap %q is not locked but --from-snap-confine was used", instanceName)
		}
	} else {
		if err := lock.Lock(); err != nil {
			return fmt.Errorf("cannot lock mount namespace of snap %q: %s", instanceName, err)
		}
	}

	// Freeze the mount namespace and unfreeze it later. This lets us perform
	// modifications without snap processes attempting to construct
	// symlinks or perform other malicious activity (such as attempting to
	// introduce a symlink that would cause us to mount something other
	// than what we expected).
	logger.Debugf("freezing processes of snap %q", instanceName)
	if err := freezeSnapProcesses(instanceName); err != nil {
		return err
	}
	defer func() {
		logger.Debugf("thawing processes of snap %q", instanceName)
		thawSnapProcesses(instanceName)
	}()

	// Allow creating directories related to this snap name.
	//
	// Note that we allow /var/snap instead of /var/snap/$SNAP_NAME because
	// content interface connections can readily create missing mount points on
	// both sides of the interface connection.
	//
	// We scope /snap/$SNAP_NAME because only one side of the connection can be
	// created, as snaps are read-only, the mimic construction will kick-in and
	// create the missing directory but this directory is only visible from the
	// snap that we are operating on (either plug or slot side, the point is,
	// the mount point is not universally visible).
	//
	// /snap/$SNAP_NAME needs to be there as the code that creates such mount
	// points must traverse writable host filesystem that contains /snap/*/ and
	// normally such access is off-limits. This approach allows /snap/foo
	// without allowing /snap/bin, for example.
	//
	// /snap/$SNAP_INSTANCE_NAME and /snap/$SNAP_NAME are added to allow
	// remapping for parallel installs only when the snap has an instance key
	//
	// TODO: Handle /home/*/snap/* when we do per-user mount namespaces and
	// allow defining layout items that refer to SNAP_USER_DATA and
	// SNAP_USER_COMMON.
	as := &Assumptions{}
	as.AddUnrestrictedPaths("/tmp", "/var/snap", "/snap/"+instanceName)
	if snapName := snap.InstanceSnap(instanceName); snapName != instanceName {
		as.AddUnrestrictedPaths("/snap/" + snapName)
	}
	return computeAndSaveChanges(instanceName, as)
}

func applyUserFstab(snapName string) error {
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.user-fstab", dirs.SnapMountPolicyDir, snapName)
	desired, err := osutil.LoadMountProfile(desiredProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load desired user mount profile of snap %q: %s", snapName, err)
	}

	// Replace XDG_RUNTIME_DIR in mount profile
	xdgRuntimeDir := fmt.Sprintf("%s/%d", dirs.XdgRuntimeDirBase, os.Getuid())
	for i := range desired.Entries {
		if strings.HasPrefix(desired.Entries[i].Name, "$XDG_RUNTIME_DIR/") {
			desired.Entries[i].Name = strings.Replace(desired.Entries[i].Name, "$XDG_RUNTIME_DIR", xdgRuntimeDir, 1)
		}
		if strings.HasPrefix(desired.Entries[i].Dir, "$XDG_RUNTIME_DIR/") {
			desired.Entries[i].Dir = strings.Replace(desired.Entries[i].Dir, "$XDG_RUNTIME_DIR", xdgRuntimeDir, 1)
		}
	}

	debugShowProfile(desired, "desired mount profile")

	// TODO: configure the secure helper and inform it about directories that
	// can be created without trespassing.
	as := &Assumptions{}
	_, err = applyProfile(snapName, &osutil.MountProfile{}, desired, as)
	return err
}

func computeAndSaveChanges(snapName string, as *Assumptions) error {
	// Read the desired and current mount profiles. Note that missing files
	// count as empty profiles so that we can gracefully handle a mount
	// interface connection/disconnection.
	desiredProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapMountPolicyDir, snapName)
	desired, err := osutil.LoadMountProfile(desiredProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load desired mount profile of snap %q: %s", snapName, err)
	}
	debugShowProfile(desired, "desired mount profile")

	currentProfilePath := fmt.Sprintf("%s/snap.%s.fstab", dirs.SnapRunNsDir, snapName)
	currentBefore, err := osutil.LoadMountProfile(currentProfilePath)
	if err != nil {
		return fmt.Errorf("cannot load current mount profile of snap %q: %s", snapName, err)
	}
	debugShowProfile(currentBefore, "current mount profile (before applying changes)")
	// Synthesize mount changes that were applied before for the purpose of the tmpfs detector.
	for _, entry := range currentBefore.Entries {
		as.AddChange(&Change{Action: Mount, Entry: entry})
	}

	currentAfter, err := applyProfile(snapName, currentBefore, desired, as)
	if err != nil {
		return err
	}

	logger.Debugf("saving current mount profile of snap %q", snapName)
	if err := currentAfter.Save(currentProfilePath); err != nil {
		return fmt.Errorf("cannot save current mount profile of snap %q: %s", snapName, err)
	}
	return nil
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
