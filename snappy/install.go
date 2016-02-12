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
	// AllowUnauthenticated allows to install a snap even if it cannot be authenticated
	AllowUnauthenticated InstallFlags = 1 << iota
	// InhibitHooks will ensure that the hooks are not run
	InhibitHooks
	// DoInstallGC will ensure that garbage collection is done
	DoInstallGC
	// AllowGadget allows the installation of Gadget packages, this does not affect updates.
	AllowGadget
)

func installRemote(mStore *SnapUbuntuStoreRepository, remoteSnap *RemoteSnapPart, flags InstallFlags, meter progress.Meter) (string, error) {
	downloadedSnap, err := mStore.Download(remoteSnap, meter)
	if err != nil {
		return "", fmt.Errorf("cannot download %s: %s", remoteSnap.Name(), err)
	}
	defer os.Remove(downloadedSnap)

	if err := remoteSnap.saveStoreManifest(); err != nil {
		return "", err
	}

	localSnap, err := (&Overlord{}).Install(downloadedSnap, remoteSnap.Origin(), flags, meter)
	if err != nil {
		return "", err
	}

	return localSnap.Name(), nil
}

func doUpdate(mStore *SnapUbuntuStoreRepository, part Part, flags InstallFlags, meter progress.Meter) error {
	_, err := installRemote(mStore, part.(*RemoteSnapPart), flags, meter)
	if err == ErrSideLoaded {
		logger.Noticef("Skipping sideloaded package: %s", part.Name())
		return nil
	} else if err != nil {
		return err
	}

	if err := GarbageCollect(part.Name(), flags, meter); err != nil {
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
// updated and returns the corresponding local snap parts.
func convertToInstalledSnaps(remoteUpdates []Part) ([]Part, error) {
	installed, err := NewLocalSnapRepository().Installed()
	if err != nil {
		return nil, err
	}

	installedUpdates := make([]Part, 0, len(remoteUpdates))
	for _, part := range remoteUpdates {
		localPart := FindSnapsByNameAndVersion(part.Name(), part.Version(), installed)
		if len(localPart) != 1 {
			return nil, fmt.Errorf("expected one local part for the update %v, got %v", part, len(localPart))
		}
		installedUpdates = append(installedUpdates, localPart[0])
	}

	return installedUpdates, nil
}

// Update updates the selected name
func Update(name string, flags InstallFlags, meter progress.Meter) ([]Part, error) {
	installed, err := NewLocalSnapRepository().Installed()
	if err != nil {
		return nil, err
	}
	cur := FindSnapsByName(name, installed)
	if len(cur) != 1 {
		return nil, ErrNotInstalled
	}

	mStore := NewUbuntuStoreSnapRepository()
	// zomg :-(
	// TODO: query the store for just this package, instead of this
	updates, err := mStore.Updates()
	if err != nil {
		return nil, fmt.Errorf("cannot get updates: %s", err)
	}
	upd := FindSnapsByName(QualifiedName(cur[0]), updates)
	if len(upd) < 1 {
		return nil, fmt.Errorf("cannot find any update for %q", name)
	}

	if err := doUpdate(mStore, upd[0], flags, meter); err != nil {
		return nil, err
	}

	installedUpdates, err := convertToInstalledSnaps(upd)
	if err != nil {
		return nil, err
	}

	return installedUpdates, nil
}

// UpdateAll the installed snappy packages, it returns the updated Parts
// if updates where available and an error and nil if any of the updates
// fail to apply.
func UpdateAll(flags InstallFlags, meter progress.Meter) ([]Part, error) {
	mStore := NewUbuntuStoreSnapRepository()
	updates, err := mStore.Updates()
	if err != nil {
		return nil, err
	}

	for _, part := range updates {
		meter.Notify(fmt.Sprintf("Updating %s (%s)", part.Name(), part.Version()))
		if err := doUpdate(mStore, part, flags, meter); err != nil {
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
func Install(name string, flags InstallFlags, meter progress.Meter) (string, error) {
	name, err := doInstall(name, flags, meter)
	if err != nil {
		return "", err
	}

	return name, GarbageCollect(name, flags, meter)
}

func doInstall(name string, flags InstallFlags, meter progress.Meter) (snapName string, err error) {
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

		return installClick(name, flags, meter, SideloadedOrigin)
	}

	// check repos next
	mStore := NewUbuntuStoreSnapRepository()
	installed, err := NewLocalSnapRepository().Installed()
	if err != nil {
		return "", err
	}

	part, err := mStore.Snap(name)
	if err != nil {
		return "", err
	}

	cur := FindSnapsByNameAndVersion(QualifiedName(part), part.Version(), installed)
	if len(cur) != 0 {
		return "", ErrAlreadyInstalled
	}
	if PackageNameActive(part.Name()) {
		return "", ErrPackageNameAlreadyInstalled
	}

	return installRemote(mStore, part, flags, meter)
}

// GarbageCollect removes all versions two older than the current active
// version, as long as NeedsReboot() is false on all the versions found, and
// DoInstallGC is set.
func GarbageCollect(name string, flags InstallFlags, pb progress.Meter) error {
	var parts BySnapVersion

	if (flags & DoInstallGC) == 0 {
		return nil
	}

	installed, err := NewLocalSnapRepository().Installed()
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
