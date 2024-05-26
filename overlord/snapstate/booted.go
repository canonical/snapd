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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

// UpdateBootRevisions synchronizes the active kernel and OS snap versions
// with the versions that actually booted. This is needed because a
// system may install "os=v2" but that fails to boot. The bootloader
// fallback logic will revert to "os=v1" but on the filesystem snappy
// still has the "active" version set to "v2" which is
// misleading. This code will check what kernel/os booted and set
// those versions active. To do this it creates a Change and kicks
// start it directly.
func UpdateBootRevisions(st *state.State) error {
	const errorPrefix = "cannot update revisions after boot changes: "

	// nothing to check if there's no kernel
	ok := mylog.Check2(HasSnapOfType(st, snap.TypeKernel))

	if !ok {
		return nil
	}

	deviceCtx := mylog.Check2(DeviceCtx(st, nil, nil))

	// if we have a kernel, we should have a model

	var tsAll []*state.TaskSet
	for _, typ := range []snap.Type{snap.TypeKernel, snap.TypeBase} {
		if !boot.SnapTypeParticipatesInBoot(typ, deviceCtx) {
			continue
		}

		actual := mylog.Check2(boot.GetCurrentBoot(typ, deviceCtx))

		info := mylog.Check2(CurrentInfo(st, actual.SnapName()))

		if actual.SnapRevision() != info.SideInfo.Revision {
			// FIXME: check that there is no task
			//        for this already in progress
			ts := mylog.Check2(RevertToRevision(st, actual.SnapName(), actual.SnapRevision(), Flags{}, ""))

			tsAll = append(tsAll, ts)
		}
	}

	if len(tsAll) == 0 {
		return nil
	}

	msg := "Update kernel and core snap revisions"
	chg := st.NewChange("update-revisions", msg)
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	st.EnsureBefore(0)

	return nil
}
