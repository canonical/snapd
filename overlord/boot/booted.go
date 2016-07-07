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

package boot

import (
	"fmt"
	"strings"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

func nameAndRevnoFromSnap(sn string) (string, snap.Revision, error) {
	l := strings.Split(sn, "_")
	if len(l) < 2 {
		return "", snap.Revision{}, fmt.Errorf("input %q has invalid format (not enough '_')", sn)
	}
	name := l[0]
	revnoNSuffix := l[1]
	rev, err := snap.ParseRevision(strings.Split(revnoNSuffix, ".snap")[0])
	if err != nil {
		return "", snap.Revision{}, err
	}
	return name, rev, nil
}

// UpdateRevisions synchronizes the active kernel and OS snap versions with
// the versions that actually booted. This is needed because a
// system may install "os=v2" but that fails to boot. The bootloader
// fallback logic will revert to "os=v1" but on the filesystem snappy
// still has the "active" version set to "v2" which is
// misleading. This code will check what kernel/os booted and set
// those versions active.
func UpdateRevisions(ovld *overlord.Overlord) error {
	if release.OnClassic {
		return nil
	}

	bootloader, err := partition.FindBootloader()
	if err != nil {
		return fmt.Errorf("cannot run UpdateRevisions: %s", err)
	}

	bv := "snappy_kernel"
	kernelSnap, err := bootloader.GetBootVar(bv)
	if err != nil {
		return fmt.Errorf("cannot get bootvar %q: %s", bv, err)
	}
	bv = "snappy_os"
	osSnap, err := bootloader.GetBootVar(bv)
	if err != nil {
		return fmt.Errorf("cannot get bootvar %q: %s", bv, err)
	}

	st := ovld.State()
	st.Lock()
	installed, err := snapstate.All(st)
	if err != nil {
		return fmt.Errorf("cannot run UpdateRevisions: %s", err)
	}

	var tsAll []*state.TaskSet
	for _, snapNameAndRevno := range []string{kernelSnap, osSnap} {
		name, rev, err := nameAndRevnoFromSnap(snapNameAndRevno)
		if err != nil {
			logger.Noticef("cannot parse %q: %s", snapNameAndRevno, err)
			continue
		}
		for snapName, snapState := range installed {
			if name == snapName {
				if rev != snapState.Current {
					ts, err := snapstate.RevertToRevision(st, name, rev)
					if err != nil {
						return err
					}
					tsAll = append(tsAll, ts)
				}
			}
		}
	}
	st.Unlock()

	if len(tsAll) == 0 {
		return nil
	}

	st.Lock()
	msg := fmt.Sprintf("Update snap revisions after boot changes")
	chg := st.NewChange("update-revisions", msg)
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	st.Unlock()

	// do it and wait for ready
	ovld.Loop()

	st.EnsureBefore(0)
	<-chg.Ready()

	st.Lock()
	status := chg.Status()
	err = chg.Err()
	st.Unlock()
	if status != state.DoneStatus {
		ovld.Stop()
		return fmt.Errorf("cannot update revisions after boot changes: %s", err)
	}

	return ovld.Stop()
}
