// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package snapstatetest

import (
	"fmt"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"gopkg.in/check.v1"
)

func fakeSnapID(name string) string {
	if id := naming.WellKnownSnapID(name); id != "" {
		return id
	}
	return snaptest.AssertedSnapID(name)
}

func InstallSnap(c *check.C, st *state.State, yaml string, required bool, si *snap.SideInfo) *snap.Info {
	info := snaptest.MakeSnapFileAndDir(c, yaml, nil, si)

	t := info.Type()
	if si.RealName == "core" {
		t = snap.TypeOS
	}

	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		SnapType:        string(t),
		Active:          true,
		Sequence:        []*snap.SideInfo{&info.SideInfo},
		Current:         info.Revision,
		Flags:           snapstate.Flags{Required: required},
		TrackingChannel: si.Channel,
	})
	return info
}

func InstallEssentialSnaps(c *check.C, st *state.State, base string, bloader bootloader.Bootloader) {
	const required = true

	InstallSnap(c, st, fmt.Sprintf("name: pc\nversion: 1\ntype: gadget\nbase: %s", base), required, &snap.SideInfo{
		SnapID:   fakeSnapID("pc"),
		Revision: snap.R(1),
		RealName: "pc",
	})

	InstallSnap(c, st, "name: pc-kernel\nversion: 1\ntype: kernel\n", required, &snap.SideInfo{
		SnapID:   fakeSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	})

	InstallSnap(c, st, fmt.Sprintf("name: %s\nversion: 1\ntype: base\n", base), required, &snap.SideInfo{
		SnapID:   fakeSnapID(base),
		Revision: snap.R(1),
		RealName: base,
	})

	if bloader != nil {
		err := bloader.SetBootVars(map[string]string{
			"snap_mode":   boot.DefaultStatus,
			"snap_core":   fmt.Sprintf("%s_1.snap", base),
			"snap_kernel": "pc-kernel_1.snap",
		})
		c.Assert(err, check.IsNil)
	}
}
