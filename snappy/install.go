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
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/store"
)

// SetupFlags can be used to pass additional flags to the install of a
// snap
type InstallFlags uint

const (
	// AllowUnauthenticated allows to install a snap even if it cannot be authenticated
	AllowUnauthenticated InstallFlags = 1 << iota
	// InhibitHooks will ensure that the hooks are not run
	InhibitHooks
	// DoInstallGC will ensure that garbage collection is done
	DoInstallGC
	// AllowGadget allows the installation of Gadget packages, this does not affect updates.
	AllowGadget
	// DeveloperMode will install the snap without confinement
	DeveloperMode
)

func installRemote(mStore *store.SnapUbuntuStoreRepository, remoteSnap *snap.Info, flags InstallFlags, meter progress.Meter) (string, error) {
	downloadedSnap, err := mStore.Download(remoteSnap, meter, nil)
	if err != nil {
		return "", fmt.Errorf("cannot download %s: %s", remoteSnap.Name(), err)
	}
	defer os.Remove(downloadedSnap)

	localSnap, err := (&Overlord{}).InstallWithSideInfo(downloadedSnap, &remoteSnap.SideInfo, flags, meter)
	if err != nil {
		return "", err
	}

	return localSnap.Name(), nil
}

func doUpdate(mStore *store.SnapUbuntuStoreRepository, rsnap *snap.Info, flags InstallFlags, meter progress.Meter) error {
	_, err := installRemote(mStore, rsnap, flags, meter)
	if err == ErrSideLoaded {
		logger.Noticef("Skipping sideloaded package: %s", rsnap.Name())
		return nil
	} else if err != nil {
		return err
	}

	if err := GarbageCollect(rsnap.Name(), flags, meter); err != nil {
		return err
	}

	return nil
}

// FIXME: This needs to go (and will go). We will have something
//        like:
//           remoteSnapType = GetUpdatesFromServer()
//           localSnapType = DoUpdate(remoteSnaps)
//           ShowUpdates(localSnaps)
//        By using the different types (instead of the same interface)
//        it will not be possilbe to pass remote snaps into the
//        ShowUpdates() output.
//
//
// convertToInstalledSnaps takes a slice of remote snaps that got
// updated and returns the corresponding local snaps
func convertToInstalledSnaps(remoteUpdates []*snap.Info) ([]*Snap, error) {
	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return nil, err
	}

	installedUpdates := make([]*Snap, 0, len(remoteUpdates))
	for _, snap := range remoteUpdates {
		for _, installed := range installed {
			if snap.Name() == installed.Name() && snap.Version == installed.Version() {
				installedUpdates = append(installedUpdates, installed)
			}
		}
	}

	return installedUpdates, nil
}

// snapUpdates identifies which snaps have updates in the store.
func snapUpdates(repo *store.SnapUbuntuStoreRepository) (snaps []*snap.Info, err error) {
	// TODO: this should eventually be snap-id based
	// NOTE this *will* send .sideload apps to the store.
	installed, err := ActiveSnapIterByType(fullNameWithChannel, snap.TypeApp, snap.TypeGadget, snap.TypeOS, snap.TypeKernel)
	if err != nil || len(installed) == 0 {
		return nil, err
	}

	rsnaps, err := repo.Updates(installed, nil)
	if err != nil {
		return nil, err
	}

	for _, rsnap := range rsnaps {
		current := ActiveSnapByName(rsnap.Name())
		if current == nil || current.Revision() != rsnap.Revision {
			snaps = append(snaps, rsnap)
		}
	}

	return snaps, nil
}

var storeConfig = (*store.SnapUbuntuStoreConfig)(nil)

// TODO: kill this function once fewer places make a store on the fly

// NewConfiguredUbuntuStoreSnapRepository creates a new fully configured store.SnapUbuntuStoreRepository with the store id selected by the gadget.
func NewConfiguredUbuntuStoreSnapRepository() *store.SnapUbuntuStoreRepository {
	storeID := ""
	if cand := os.Getenv("UBUNTU_STORE_ID"); cand != "" {
		storeID = cand
	} else if cand := StoreID(); cand != "" {
		storeID = cand
	}
	return store.NewUbuntuStoreSnapRepository(storeConfig, storeID)
}

// Update updates the selected name
func Update(name string, flags InstallFlags, meter progress.Meter) ([]*Snap, error) {
	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return nil, err
	}
	cur := FindSnapsByName(name, installed)
	if len(cur) != 1 {
		return nil, ErrNotInstalled
	}

	mStore := NewConfiguredUbuntuStoreSnapRepository()
	// zomg :-(
	// TODO: query the store for just this package, instead of this
	updates, err := snapUpdates(mStore)
	if err != nil {
		return nil, fmt.Errorf("cannot get updates: %s", err)
	}
	var update *snap.Info
	for _, upd := range updates {
		if cur[0].Name() == upd.Name() {
			update = upd
			break
		}
	}
	if update == nil {
		return nil, fmt.Errorf("cannot find any update for %q", name)
	}

	if err := doUpdate(mStore, update, flags, meter); err != nil {
		return nil, err
	}

	installedUpdates, err := convertToInstalledSnaps([]*snap.Info{update})
	if err != nil {
		return nil, err
	}

	return installedUpdates, nil
}

// UpdateAll the installed snappy packages, it returns the updated Snaps
// if updates where available and an error and nil if any of the updates
// fail to apply.
func UpdateAll(flags InstallFlags, meter progress.Meter) ([]*Snap, error) {
	mStore := NewConfiguredUbuntuStoreSnapRepository()
	updates, err := snapUpdates(mStore)
	if err != nil {
		return nil, err
	}

	for _, snap := range updates {
		meter.Notify(fmt.Sprintf("Updating %s (%s)", snap.Name(), snap.Version))
		if err := doUpdate(mStore, snap, flags, meter); err != nil {
			return nil, err
		}
	}

	installedUpdates, err := convertToInstalledSnaps(updates)
	if err != nil {
		return nil, err
	}

	return installedUpdates, nil
}

// Install the givens snap names provided via args. This can be local
// files or snaps that are queried from the store
func Install(name, channel string, flags InstallFlags, meter progress.Meter) (string, error) {
	name, err := doInstall(name, channel, flags, meter)
	if err != nil {
		return "", err
	}

	return name, GarbageCollect(name, flags, meter)
}

func doInstall(name, channel string, flags InstallFlags, meter progress.Meter) (snapName string, err error) {
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
			flags |= AllowUnauthenticated
		}

		snap, err := (&Overlord{}).Install(name, flags, meter)
		if err != nil {
			return "", err
		}

		return snap.Name(), nil
	}

	// check repos next
	mStore := NewConfiguredUbuntuStoreSnapRepository()
	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return "", err
	}

	snap, err := mStore.Snap(name, channel, nil)
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
func GarbageCollect(name string, flags InstallFlags, pb progress.Meter) error {
	var snaps BySnapVersion

	if (flags & DoInstallGC) == 0 {
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
		if err := (&Overlord{}).Uninstall(snap, pb); err != nil {
			return ErrGarbageCollectImpossible(err.Error())
		}
	}

	return nil
}
