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
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
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

type InstallSnapOptions struct {
	Required         bool
	PreserveSequence bool
}

func InstallSnap(c *check.C, st *state.State, yaml string, files [][]string, si *snap.SideInfo, opts InstallSnapOptions) *snap.Info {
	info := snaptest.MakeSnapFileAndDir(c, yaml, files, si)

	t := info.Type()
	if si.RealName == "core" {
		t = snap.TypeOS
	}

	var seq sequence.SnapSequence
	if opts.PreserveSequence {
		var ss snapstate.SnapState
		err := snapstate.Get(st, info.InstanceName(), &ss)
		c.Assert(err, check.IsNil)
		seq.Revisions = append(seq.Revisions, ss.Sequence.Revisions...)
	}

	seq.Revisions = append(seq.Revisions, sequence.NewRevisionSideState(si, nil))

	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		SnapType:        string(t),
		Active:          true,
		Sequence:        seq,
		Current:         info.Revision,
		Flags:           snapstate.Flags{Required: opts.Required},
		TrackingChannel: si.Channel,
	})
	return info
}

func InstallEssentialSnaps(c *check.C, st *state.State, base string, gadgetFiles [][]string, bloader bootloader.Bootloader) {
	InstallSnap(c, st, fmt.Sprintf("name: pc\nversion: 1\ntype: gadget\nbase: %s", base), gadgetFiles, &snap.SideInfo{
		SnapID:   fakeSnapID("pc"),
		Revision: snap.R(1),
		RealName: "pc",
	}, InstallSnapOptions{Required: true})

	InstallSnap(c, st, "name: pc-kernel\nversion: 1\ntype: kernel\n", nil, &snap.SideInfo{
		SnapID:   fakeSnapID("pc-kernel"),
		Revision: snap.R(1),
		RealName: "pc-kernel",
	}, InstallSnapOptions{Required: true})

	InstallSnap(c, st, fmt.Sprintf("name: %s\nversion: 1\ntype: base\n", base), nil, &snap.SideInfo{
		SnapID:   fakeSnapID(base),
		Revision: snap.R(1),
		RealName: base,
	}, InstallSnapOptions{Required: true})

	if bloader != nil {
		err := bloader.SetBootVars(map[string]string{
			"snap_mode":   boot.DefaultStatus,
			"snap_core":   fmt.Sprintf("%s_1.snap", base),
			"snap_kernel": "pc-kernel_1.snap",
		})
		c.Assert(err, check.IsNil)
	}
}

func NewSequenceFromSnapSideInfos(snapSideInfo []*snap.SideInfo) sequence.SnapSequence {
	revSis := make([]*sequence.RevisionSideState, len(snapSideInfo))
	for i, si := range snapSideInfo {
		revSis[i] = sequence.NewRevisionSideState(si, nil)
	}
	return sequence.SnapSequence{Revisions: revSis}
}

func NewSequenceFromRevisionSideInfos(revsSideInfo []*sequence.RevisionSideState) sequence.SnapSequence {
	return sequence.SnapSequence{Revisions: revsSideInfo}
}
