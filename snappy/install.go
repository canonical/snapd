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

	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/provisioning"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

// SetupFlags can be used to pass additional flags to the install of a
// snap
type LegacyInstallFlags uint

const (
	// AllowUnauthenticated allows to install a snap even if it cannot be authenticated
	LegacyAllowUnauthenticated LegacyInstallFlags = 1 << iota
	// InhibitHooks will ensure that the hooks are not run
	LegacyInhibitHooks
	// DoInstallGC will ensure that garbage collection is done
	LegacyDoInstallGC
	// AllowGadget allows the installation of Gadget packages, this does not affect updates.
	LegacyAllowGadget

	// Do not add new flags here! this is all going away soon! just kept alive as long as we may need to quickly patch up u-d-f.
	DO_NOT_ADD_NEW_FLAGS_HERE
)

func installRemote(mStore *store.SnapUbuntuStoreRepository, remoteSnap *snap.Info, flags LegacyInstallFlags, meter progress.Meter) (string, error) {
	downloadedSnap, err := mStore.Download(remoteSnap.Name(), &remoteSnap.DownloadInfo, meter, nil)
	if err != nil {
		return "", fmt.Errorf("cannot download %s: %s", remoteSnap.Name(), err)
	}
	defer os.Remove(downloadedSnap)

	localSnap, err := (&Overlord{}).installWithSideInfo(downloadedSnap, &remoteSnap.SideInfo, flags, meter)
	if err != nil {
		return "", err
	}

	return localSnap.Name(), nil
}

var storeConfig = (*store.SnapUbuntuStoreConfig)(nil)

// TODO: kill this function once fewer places make a store on the fly

// newConfiguredUbuntuStoreSnapRepository creates a new fully configured store.SnapUbuntuStoreRepository with the store id selected by the gadget.
func newConfiguredUbuntuStoreSnapRepository() *store.SnapUbuntuStoreRepository {
	storeID := ""
	// TODO: set the store-id here from the model information
	if cand := os.Getenv("UBUNTU_STORE_ID"); cand != "" {
		storeID = cand
	}

	return store.NewUbuntuStoreSnapRepository(storeConfig, storeID, nil)
}

// Install the givens snap names provided via args. This can be local
// files or snaps that are queried from the store
func install(name, channel string, flags LegacyInstallFlags, meter progress.Meter) (string, error) {
	name, err := doInstall(name, channel, flags, meter)
	if err != nil {
		return "", err
	}

	return name, GarbageCollect(name, flags, meter)
}

func doInstall(name, channel string, flags LegacyInstallFlags, meter progress.Meter) (snapName string, err error) {
	defer func() {
		if err != nil {
			err = &ErrInstallFailed{Snap: name, OrigErr: err}
		}
	}()

	// consume local snaps
	if fi, err := os.Stat(name); err == nil && fi.Mode().IsRegular() {
		// we allow unauthenticated package when in developer
		// mode
		if provisioning.InDeveloperMode() {
			flags |= LegacyAllowUnauthenticated
		}

		snap, err := (&Overlord{}).install(name, flags, meter)
		if err != nil {
			return "", err
		}

		return snap.Name(), nil
	}

	// check repos next
	mStore := newConfiguredUbuntuStoreSnapRepository()
	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return "", err
	}

	// devmode false preserves the old behaviour but we might want
	// it to be set from flags instead.
	snap, err := mStore.Snap(name, channel, false, nil)
	if err != nil {
		return "", err
	}

	cur := FindSnapsByNameAndVersion(snap.Name(), snap.Version, installed)
	if len(cur) != 0 {
		return "", ErrAlreadyInstalled
	}
	if PackageNameActive(snap.Name()) {
		return "", ErrPackageNameAlreadyInstalled
	}

	return installRemote(mStore, snap, flags, meter)
}

// GarbageCollect removes all versions two older than the current active
// version, as long as NeedsReboot() is false on all the versions found, and
// DoInstallGC is set.
func GarbageCollect(name string, flags LegacyInstallFlags, pb progress.Meter) error {
	var snaps BySnapVersion

	if (flags & LegacyDoInstallGC) == 0 {
		return nil
	}

	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return err
	}

	snaps = FindSnapsByName(name, installed)
	if len(snaps) < 3 {
		// not enough things installed to do gc
		return nil
	}

	// FIXME: sort by revision sequence
	sort.Sort(snaps)
	active := -1 // active is the index of the active snap in snaps (-1 if no active snap)

	for i, snap := range snaps {
		if snap.IsActive() {
			if active > -1 {
				return ErrGarbageCollectImpossible("more than one active (should not happen).")
			}
			active = i
		}
		if snap.NeedsReboot() {
			return nil // don't do gc on snaps that need reboot.
		}
	}

	if active < 1 {
		// how was this an install?
		return nil
	}

	for _, snap := range snaps[:active-1] {
		if err := (&Overlord{}).uninstall(snap, pb); err != nil {
			return ErrGarbageCollectImpossible(err.Error())
		}
	}

	return nil
}
