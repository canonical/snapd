// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package snapstate_test

import (
	"errors"
	"strings"

	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/store"
)

type fakeOp struct {
	op string

	name    string
	revno   int
	channel string
	flags   int
	active  bool
	sinfo   snap.SideInfo

	old string
}

type fakeSnappyBackend struct {
	ops []fakeOp

	fakeCurrentProgress int
	fakeTotalProgress   int

	linkSnapFailTrigger string
}

func (f *fakeSnappyBackend) InstallLocal(path string, flags int, p progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "install-local",
		name: path,
	})
	return nil
}

func (f *fakeSnappyBackend) Download(name, channel string, checker func(*snap.Info) error, p progress.Meter, auther store.Authenticator) (*snap.Info, string, error) {
	f.ops = append(f.ops, fakeOp{
		op:      "download",
		name:    name,
		channel: channel,
	})
	p.SetTotal(float64(f.fakeTotalProgress))
	p.Set(float64(f.fakeCurrentProgress))

	revno := 11
	if channel == "channel-for-7" {
		revno = 7
	}

	info := &snap.Info{
		SideInfo: snap.SideInfo{
			OfficialName: strings.Split(name, ".")[0],
			Channel:      channel,
			SnapID:       "snapIDsnapidsnapidsnapidsnapidsn",
			Revision:     revno,
		},
		Version: name,
	}

	err := checker(info)
	if err != nil {
		return nil, "", err
	}

	return info, "downloaded-snap-path", nil
}

func (f *fakeSnappyBackend) CheckSnap(snapFilePath string, curInfo *snap.Info, flags int) error {
	cur := "<no-current>"
	if curInfo != nil {
		cur = curInfo.MountDir()
	}
	f.ops = append(f.ops, fakeOp{
		op:    "check-snap",
		name:  snapFilePath,
		old:   cur,
		flags: flags,
	})
	return nil
}

func (f *fakeSnappyBackend) SetupSnap(snapFilePath string, si *snap.SideInfo, flags int) error {
	revno := 0
	if si != nil {
		revno = si.Revision
	}
	f.ops = append(f.ops, fakeOp{
		op:    "setup-snap",
		name:  snapFilePath,
		flags: flags,
		revno: revno,
	})
	return nil
}

func (f *fakeSnappyBackend) ReadInfo(name string, si *snap.SideInfo) (*snap.Info, error) {
	// naive emulation for now, always works
	return &snap.Info{SuggestedName: name, SideInfo: *si}, nil
}

func (f *fakeSnappyBackend) CopySnapData(newInfo, oldInfo *snap.Info, flags int) error {
	old := "<no-old>"
	if oldInfo != nil {
		old = oldInfo.MountDir()
	}
	f.ops = append(f.ops, fakeOp{
		op:    "copy-data",
		name:  newInfo.MountDir(),
		flags: flags,
		old:   old,
	})
	return nil
}

func (f *fakeSnappyBackend) LinkSnap(info *snap.Info) error {
	if info.MountDir() == f.linkSnapFailTrigger {
		f.ops = append(f.ops, fakeOp{
			op:   "link-snap.failed",
			name: info.MountDir(),
		})
		return errors.New("fail")
	}

	f.ops = append(f.ops, fakeOp{
		op:   "link-snap",
		name: info.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) UndoSetupSnap(s snap.PlaceInfo) error {
	f.ops = append(f.ops, fakeOp{
		op:   "undo-setup-snap",
		name: s.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) UndoCopySnapData(newInfo *snap.Info, flags int) error {
	f.ops = append(f.ops, fakeOp{
		op:   "undo-copy-snap-data",
		name: newInfo.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) CanRemove(info *snap.Info, active bool) bool {
	f.ops = append(f.ops, fakeOp{
		op:     "can-remove",
		name:   info.MountDir(),
		active: active,
	})
	return true
}

func (f *fakeSnappyBackend) UnlinkSnap(info *snap.Info, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "unlink-snap",
		name: info.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapFiles(s snap.PlaceInfo, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-files",
		name: s.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapData(info *snap.Info) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-data",
		name: info.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) GarbageCollect(name string, flags int, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:    "garbage-collect",
		name:  name,
		flags: flags,
	})
	return nil
}

func (f *fakeSnappyBackend) Candidate(sideInfo *snap.SideInfo) {
	var sinfo snap.SideInfo
	if sideInfo != nil {
		sinfo = *sideInfo
	}
	f.ops = append(f.ops, fakeOp{
		op:    "candidate",
		sinfo: sinfo,
	})
}
