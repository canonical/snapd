// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"fmt"
	"os"
	"sort"

	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/provisioning"
)

// InstallFlags can be used to pass additional flags to the install of a
// snap
type InstallFlags uint

const (
	// AllowUnauthenticated allows to install a snap even if it can not be authenticated
	AllowUnauthenticated InstallFlags = 1 << iota
	// InhibitHooks will ensure that the hooks are not run
	InhibitHooks
	// DoInstallGC will ensure that garbage collection is done
	DoInstallGC
	// AllowGadget allows the installation of Gadget packages, this does not affect updates.
	AllowGadget
)

func findSnapPartByName(spl []*SnapPart, name string) *SnapPart {
	for _, sp := range spl {
		if sp.Name() == name {
			return sp
		}
	}
	return nil
}

// Update updates a single snap with the given name
func Update(name string, flags InstallFlags, meter progress.Meter) (*SnapPart, error) {
	overlord := &Overlord{}
	cur := findSnapPartByName(overlord.Installed(), name)
	if cur == nil {
		return nil, ErrNotInstalled
	}

	mStore := NewUbuntuStoreSnapRepository()
	remoteSnap, err := mStore.Snap(QualifiedName(cur))
	if err != nil {
		return nil, fmt.Errorf("no update found for %s", name)
	}

	localSnap, err := installRemote(remoteSnap, meter, flags)
	if err == ErrSideLoaded {
		logger.Noticef("Skipping sideloaded package: %s", cur.Name())
		return localSnap, nil
	} else if err != nil {
		return nil, err
	}

	if err := GarbageCollect(localSnap.Name(), flags, meter); err != nil {
		return nil, err
	}

	return localSnap, nil
}

// UpdateAll the installed snappy packages, it returns the updated Parts
// if updates where available and an error and nil if any of the updates
// fail to apply.
func UpdateAll(flags InstallFlags, meter progress.Meter) ([]*SnapPart, error) {
	mStore := NewUbuntuStoreSnapRepository()
	updates, err := mStore.Updates()
	if err != nil {
		return nil, err
	}

	allUpdates := []*SnapPart{}
	for _, part := range updates {
		meter.Notify(fmt.Sprintf("Updating %s (%s)", part.Name(), part.Version()))
		localSnap, err := Update(part.Name(), flags, meter)
		if err != nil {
			return nil, err
		}
		allUpdates = append(allUpdates, localSnap)
	}

	return allUpdates, nil
}

// Install the givens snap names provided via args. This can be local
// files or snaps that are queried from the store
func Install(name string, flags InstallFlags, meter progress.Meter) (string, error) {
	sp, err := doInstall(name, flags, meter)
	if err != nil {
		return "", err
	}

	return sp.Name(), GarbageCollect(sp.Name(), flags, meter)
}

func doInstall(name string, flags InstallFlags, meter progress.Meter) (sp *SnapPart, err error) {
	defer func() {
		if err != nil {
			err = &ErrInstallFailed{Snap: name, OrigErr: err}
		}
	}()

	// consume local parts
	if fi, err := os.Stat(name); err == nil && fi.Mode().IsRegular() {
		// we allow unauthenticated package when in developer
		// mode
		if provisioning.InDeveloperMode() {
			flags |= AllowUnauthenticated
		}

		overlord := &Overlord{}
		return overlord.Install(name, SideloadedOrigin, meter, flags)
	}

	// check repos next
	mStore := NewUbuntuStoreSnapRepository()
	installed, err := NewMetaLocalRepository().Installed()
	if err != nil {
		return nil, err
	}

	name, origin := SplitOrigin(name)
	found, err := mStore.Details(name, origin)
	if err != nil {
		return nil, err
	}

	if len(found) == 0 {
		return nil, ErrPackageNotFound
	} else if len(found) > 1 {
		return nil, fmt.Errorf("found %d results for %s. please report this as a bug", len(found), name)
	}

	// FIXME: move into the overlord
	part := found[0]
	cur := FindSnapsByNameAndVersion(QualifiedName(part), part.Version(), installed)
	if len(cur) != 0 {
		return nil, ErrAlreadyInstalled
	}

	if PackageNameActive(part.Name()) {
		return nil, ErrPackageNameAlreadyInstalled
	}

	return installRemote(part.(*RemoteSnapPart), meter, flags)
}

func installRemote(snap *RemoteSnapPart, meter progress.Meter, flags InstallFlags) (*SnapPart, error) {
	mStore := NewUbuntuStoreSnapRepository()
	downloadedSnap, err := mStore.Download(snap, meter)
	if err != nil {
		return nil, fmt.Errorf("failed to download %s: %v", snap.Name(), err)
	}
	defer os.Remove(downloadedSnap)

	if err := snap.saveStoreManifest(); err != nil {
		return nil, err
	}

	overlord := &Overlord{}
	return overlord.Install(downloadedSnap, snap.Origin(), meter, flags)
}

// GarbageCollect removes all versions two older than the current active
// version, as long as NeedsReboot() is false on all the versions found, and
// DoInstallGC is set.
func GarbageCollect(name string, flags InstallFlags, pb progress.Meter) error {
	var parts BySnapVersion

	if (flags & DoInstallGC) == 0 {
		return nil
	}

	m := NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	parts = FindSnapsByName(name, installed)
	if len(parts) < 3 {
		// not enough things installed to do gc
		return nil
	}

	sort.Sort(parts)
	active := -1 // active is the index of the active part in parts (-1 if no active part)

	for i, part := range parts {
		if part.IsActive() {
			if active > -1 {
				return ErrGarbageCollectImpossible("more than one active (should not happen).")
			}
			active = i
		}
		if part.NeedsReboot() {
			return nil // don't do gc on parts that need reboot.
		}
	}

	if active < 1 {
		// how was this an install?
		return nil
	}

	for _, part := range parts[:active-1] {
		if err := (&Overlord{}).Uninstall(part.(*SnapPart), pb); err != nil {
			return ErrGarbageCollectImpossible(err.Error())
		}
	}

	return nil
}
