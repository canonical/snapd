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
	"strings"

	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

func nameAndRevnoFromSnap(sn string) (string, snap.Revision) {
	name := strings.Split(sn, "_")[0]
	revnoNSuffix := strings.Split(sn, "_")[1]
	rev, err := snap.ParseRevision(strings.Split(revnoNSuffix, ".snap")[0])
	if err != nil {
		return "", snap.Revision{}
	}
	return name, rev
}

// SyncBoot synchronizes the active kernel and OS snap versions with
// the versions that actually booted. This is needed because a
// system may install "os=v2" but that fails to boot. The bootloader
// fallback logic will revert to "os=v1" but on the filesystem snappy
// still has the "active" version set to "v2" which is
// misleading. This code will check what kernel/os booted and set
// those versions active.
func SyncBoot() error {
	if release.OnClassic {
		return nil
	}
	bootloader, err := partition.FindBootloader()
	if err != nil {
		return fmt.Errorf("cannot run SyncBoot: %s", err)
	}

	kernelSnap, _ := bootloader.GetBootVar("snappy_kernel")
	osSnap, _ := bootloader.GetBootVar("snappy_os")

	installed, err := (&Overlord{}).Installed()
	if err != nil {
		return fmt.Errorf("cannot run SyncBoot: %s", err)
	}

	overlord := &Overlord{}
	for _, snap := range []string{kernelSnap, osSnap} {
		name, revno := nameAndRevnoFromSnap(snap)
		found := FindSnapsByNameAndRevision(name, revno, installed)
		if len(found) != 1 {
			return fmt.Errorf("cannot SyncBoot, expected 1 snap %q (revision %s) found %d", snap, revno, len(found))
		}
		if err := overlord.SetActive(found[0], true, nil); err != nil {
			return fmt.Errorf("cannot SyncBoot, cannot make %s active: %s", found[0].Name(), err)
		}
	}

	return nil
}
