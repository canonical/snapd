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

package snapstate

import (
	"fmt"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
)

// UpdateBootRevisions synchronizes the active kernel and OS snap versions
// with the versions that actually booted. This is needed because a
// system may install "os=v2" but that fails to boot. The bootloader
// fallback logic will revert to "os=v1" but on the filesystem snappy
// still has the "active" version set to "v2" which is
// misleading. This code will check what kernel/os booted and set
// those versions active.To do this it creates a Change and kicks
// start it directly.
func UpdateBootRevisions(st *state.State) error {
	const errorPrefix = "cannot update revisions after boot changes: "

	if release.OnClassic {
		return nil
	}

	// nothing to check if there's no kernel
	ok, err := HasSnapOfType(st, snap.TypeKernel)
	if err != nil {
		return fmt.Errorf(errorPrefix+"%s", err)
	}
	if !ok {
		return nil
	}

	kernel, core, err := boot.GetCurrentBoot()
	if err != nil {
		return fmt.Errorf(errorPrefix+"%s", err)
	}

	var tsAll []*state.TaskSet
	for _, actual := range []*snap.Info{kernel, core} {
		name := actual.InstanceName()
		rev := actual.Revision
		info, err := CurrentInfo(st, name)
		if err != nil {
			logger.Noticef("cannot get info for %q: %s", name, err)
			continue
		}
		if rev != info.SideInfo.Revision {
			// FIXME: check that there is no task
			//        for this already in progress
			ts, err := RevertToRevision(st, name, rev, Flags{})
			if err != nil {
				return err
			}
			tsAll = append(tsAll, ts)
		}
	}

	if len(tsAll) == 0 {
		return nil
	}

	msg := fmt.Sprintf("Update kernel and core snap revisions")
	chg := st.NewChange("update-revisions", msg)
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)

	return nil
}

// CurrentBootNameAndRevision returns the currently set name and
// revision for boot for the given type of snap, which can be core or
// kernel. Returns ErrBootNameAndRevisionAgain if the values are
// temporarily not established.
func CurrentBootNameAndRevision(typ snap.Type) (name string, revision snap.Revision, err error) {

	kernel, core, err := boot.GetCurrentBoot()
	if err != nil {
		return "", snap.Revision{}, err
	}

	switch typ {
	case snap.TypeKernel:
		return kernel.InstanceName(), kernel.Revision, nil
	case snap.TypeOS, snap.TypeBase:
		return core.InstanceName(), core.Revision, nil
	default:
		return "", snap.Revision{}, fmt.Errorf("cannot find boot revision for anything but core and kernel")
	}
}
