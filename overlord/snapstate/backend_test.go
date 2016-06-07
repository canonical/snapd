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

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type fakeOp struct {
	op string

	macaroon string
	name     string
	revno    snap.Revision
	channel  string
	active   bool
	sinfo    snap.SideInfo

	old string
}

type fakeSnappyBackend struct {
	ops []fakeOp

	fakeCurrentProgress int
	fakeTotalProgress   int

	linkSnapFailTrigger string
}

func (f *fakeSnappyBackend) Download(name, channel string, checker func(*snap.Info) error, p progress.Meter, auther store.Authenticator) (*snap.Info, string, error) {
	p.Notify("download")
	var macaroon string
	if auther != nil {
		macaroon = auther.(*auth.MacaroonAuthenticator).Macaroon
	}
	f.ops = append(f.ops, fakeOp{
		op:       "download",
		macaroon: macaroon,
		name:     name,
		channel:  channel,
	})
	p.SetTotal(float64(f.fakeTotalProgress))
	p.Set(float64(f.fakeCurrentProgress))

	revno := snap.R(11)
	if channel == "channel-for-7" {
		revno.N = 7
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

func (f *fakeSnappyBackend) OpenSnapFile(snapFilePath string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
	op := fakeOp{
		op:   "open-snap-file",
		name: snapFilePath,
	}

	if si != nil {
		op.sinfo = *si
	}

	f.ops = append(f.ops, op)
	return &snap.Info{Architectures: []string{"all"}}, nil, nil
}

func (f *fakeSnappyBackend) SetupSnap(snapFilePath string, si *snap.SideInfo) error {
	revno := snap.R(0)
	if si != nil {
		revno = si.Revision
	}
	f.ops = append(f.ops, fakeOp{
		op:    "setup-snap",
		name:  snapFilePath,
		revno: revno,
	})
	return nil
}

func (f *fakeSnappyBackend) ReadInfo(name string, si *snap.SideInfo) (*snap.Info, error) {
	// naive emulation for now, always works
	info := &snap.Info{SuggestedName: name, SideInfo: *si}
	if name == "gadget" {
		info.Type = snap.TypeGadget
	}
	return info, nil
}

func (f *fakeSnappyBackend) CopySnapData(newInfo, oldInfo *snap.Info, p progress.Meter) error {
	p.Notify("copy-data")
	old := "<no-old>"
	if oldInfo != nil {
		old = oldInfo.MountDir()
	}
	f.ops = append(f.ops, fakeOp{
		op:   "copy-data",
		name: newInfo.MountDir(),
		old:  old,
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

func (f *fakeSnappyBackend) UndoCopySnapData(newInfo *snap.Info, oldInfo *snap.Info, p progress.Meter) error {
	p.Notify("undo-copy-data")
	old := "<no-old>"
	if oldInfo != nil {
		old = oldInfo.MountDir()
	}
	f.ops = append(f.ops, fakeOp{
		op:   "undo-copy-snap-data",
		name: newInfo.MountDir(),
		old:  old,
	})
	return nil
}

func (f *fakeSnappyBackend) UnlinkSnap(info *snap.Info, meter progress.Meter) error {
	meter.Notify("unlink")
	f.ops = append(f.ops, fakeOp{
		op:   "unlink-snap",
		name: info.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapFiles(s snap.PlaceInfo, meter progress.Meter) error {
	meter.Notify("remove-snap-files")
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

func (f *fakeSnappyBackend) RemoveSnapCommonData(info *snap.Info) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-common-data",
		name: info.MountDir(),
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

func (f *fakeSnappyBackend) Current(curInfo *snap.Info) {
	old := "<no-current>"
	if curInfo != nil {
		old = curInfo.MountDir()
	}
	f.ops = append(f.ops, fakeOp{
		op:  "current",
		old: old,
	})
}

func (f *fakeSnappyBackend) ForeignTask(kind string, status state.Status, ss *snapstate.SnapSetup) {
	f.ops = append(f.ops, fakeOp{
		op:    kind + ":" + status.String(),
		name:  ss.Name,
		revno: ss.Revision,
	})
}
