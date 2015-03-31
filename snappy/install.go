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
	"io/ioutil"
	"os"
	"sort"
	"strings"

	"launchpad.net/snappy/logger"
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
)

// check if the image is in developer mode
// FIXME: this is a bit crude right now, but it seems like there is not more
//        meta-data to check right now
// TODO: add feature to ubuntu-device-flash to write better info file when
//       the image is in developer mode
func inDeveloperMode() bool {
	f, err := os.Open(cloudMetaDataFile)
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return false
	}
	needle := "public-keys:\n"
	if strings.Contains(string(data), needle) {
		return true
	}
	return false
}

// Install the givens snap names provided via args. This can be local
// files or snaps that are queried from the store
func Install(name string, flags InstallFlags) (err error) {
	if name, err = doInstall(name, flags); err != nil {
		return logger.LogError(err)
	}
	return logger.LogError(doGarbageCollect(name, flags))
}

func doInstall(name string, flags InstallFlags) (string, error) {
	// consume local parts
	if _, err := os.Stat(name); err == nil {
		// we allow unauthenticated package when in developer
		// mode
		if inDeveloperMode() {
			flags |= AllowUnauthenticated
		}

		pbar := NewTextProgress(name)
		return installClick(name, flags, pbar)
	}

	// check repos next
	m := NewMetaRepository()
	found, _ := m.Details(name)
	for _, part := range found {
		// act only on parts that are downloadable
		if !part.IsInstalled() {
			pbar := NewTextProgress(part.Name())
			return name, part.Install(pbar, flags)
		}
	}

	return "", ErrPackageNotFound
}

func doGarbageCollect(name string, flags InstallFlags) (err error) {
	var parts BySnapVersion

	if (flags & DoInstallGC) == 0 {
		return nil
	}

	m := NewMetaRepository()
	found, _ := m.Details(name)
	for _, part := range found {
		if !part.IsInstalled() {
			continue
		}
		parts = append(parts, part)
	}
	if len(parts) < 3 {
		// not enough things installed to do gc
		return nil
	}
	sort.Sort(parts)
	active := -1
	needsReboot := false
	for i, part := range parts {
		if part.IsActive() {
			if active > -1 {
				// xxx: is this even possible?
				return ErrGarbageCollectImpossible("more than one active??")
			}
			active = i
		}
		if part.NeedsReboot() {
			needsReboot = true
		}
	}
	if needsReboot {
		if len(parts) < 4 {
			return nil
		}
		if active > -1 && active != len(parts)-2 {
			// xxx: this is a scenario that errors that maybe shouldn't:
			//  . boot with v1 (have v0 installed but not active)
			//  . install v2, which asks for reboot (active: v1; v2 not active but needsreboot)
			//  . install v3, which asks for reboot (active: v1; v2, v3 needsreboot [<- would we get two needsreboot? not sure])
			//  . reboots, everything happy
			// garbage collection should probably collect v0 and v2?
			// xxx to the xxx: not sure if this is a supported scenario
			return ErrGarbageCollectImpossible("unimplemented")
		}
		parts = parts[:len(parts)-3]
	} else {
		if active > -1 && active != len(parts)-1 {
			return ErrGarbageCollectImpossible("current not latest")
		}
		parts = parts[:len(parts)-2]
	}

	for _, part := range parts {
		if err = part.Uninstall(); err != nil {
			return ErrGarbageCollectImpossible(err.Error())
		}
	}

	return nil
}
