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
	"fmt"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type fakeOp struct {
	op string

	name  string
	revno snap.Revision
	sinfo snap.SideInfo
	stype snap.Type
	cand  store.RefreshCandidate

	old string
}

type fakeOps []fakeOp

func (ops fakeOps) Ops() []string {
	opsOps := make([]string, len(ops))
	for i, op := range ops {
		opsOps[i] = op.op
	}

	return opsOps
}

func (ops fakeOps) Count(op string) int {
	n := 0
	for i := range ops {
		if ops[i].op == op {
			n++
		}
	}
	return n
}

func (ops fakeOps) First(op string) *fakeOp {
	for i := range ops {
		if ops[i].op == op {
			return &ops[i]
		}
	}

	return nil
}

type fakeDownload struct {
	name     string
	macaroon string
}

type fakeStore struct {
	downloads           []fakeDownload
	fakeBackend         *fakeSnappyBackend
	fakeCurrentProgress int
	fakeTotalProgress   int
	state               *state.State
}

func (f *fakeStore) pokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	f.state.Lock()
	f.state.Unlock()
}

func (f *fakeStore) Snap(name, channel string, devmode bool, revision snap.Revision, user *auth.UserState) (*snap.Info, error) {
	f.pokeStateLock()

	if revision.Unset() {
		revision = snap.R(11)
		if channel == "channel-for-7" {
			revision.N = 7
		}
	}

	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: strings.Split(name, ".")[0],
			Channel:  channel,
			SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
			Revision: revision,
		},
		Version: name,
		DownloadInfo: snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
	}
	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{op: "storesvc-snap", name: name, revno: revision})

	return info, nil
}

func (f *fakeStore) Find(search *store.Search, user *auth.UserState) ([]*snap.Info, error) {
	panic("Find called")
}

func (f *fakeStore) ListRefresh(cands []*store.RefreshCandidate, _ *auth.UserState) ([]*snap.Info, error) {
	f.pokeStateLock()

	if len(cands) == 0 {
		return nil, nil
	}
	if len(cands) != 1 {
		panic("ListRefresh unexpectedly called with more than one candidate")
	}
	cand := cands[0]

	snapID := cand.SnapID

	if snapID == "" {
		return nil, nil
	}

	if snapID == "fakestore-please-error-on-refresh" {
		return nil, fmt.Errorf("failing as requested")
	}

	var name string
	if snapID == "some-snap-id" {
		name = "some-snap"
	} else {
		panic(fmt.Sprintf("ListRefresh: unknown snap-id: %s", snapID))
	}

	revno := snap.R(11)
	if cand.Channel == "channel-for-7" {
		revno = snap.R(7)
	}

	info := &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: name,
			Channel:  cand.Channel,
			SnapID:   cand.SnapID,
			Revision: revno,
		},
		Version: name,
		DownloadInfo: snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
	}

	var hit snap.Revision
	if cand.Revision != revno {
		hit = revno
	}
	for _, blocked := range cand.Block {
		if blocked == revno {
			hit = snap.Revision{}
			break
		}
	}

	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{op: "storesvc-list-refresh", cand: *cand, revno: hit})

	if hit.Unset() {
		return nil, nil
	}

	return []*snap.Info{info}, nil
}

func (f *fakeStore) SuggestedCurrency() string {
	f.pokeStateLock()

	return "XTS"
}

func (f *fakeStore) Download(name, targetFn string, snapInfo *snap.DownloadInfo, pb progress.Meter, user *auth.UserState) error {
	f.pokeStateLock()

	var macaroon string
	if user != nil {
		macaroon = user.StoreMacaroon
	}
	f.downloads = append(f.downloads, fakeDownload{
		macaroon: macaroon,
		name:     name,
	})
	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{op: "storesvc-download", name: name})

	pb.SetTotal(float64(f.fakeTotalProgress))
	pb.Set(float64(f.fakeCurrentProgress))

	return nil
}

func (f *fakeStore) Buy(options *store.BuyOptions, user *auth.UserState) (*store.BuyResult, error) {
	panic("Never expected fakeStore.Buy to be called")
}

func (f *fakeStore) ReadyToBuy(user *auth.UserState) error {
	panic("Never expected fakeStore.ReadyToBuy to be called")
}

func (f *fakeStore) Assertion(*asserts.AssertionType, []string, *auth.UserState) (asserts.Assertion, error) {
	panic("Never expected fakeStore.Assertion to be called")
}

type fakeSnappyBackend struct {
	ops fakeOps

	linkSnapFailTrigger     string
	copySnapDataFailTrigger string
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

func (f *fakeSnappyBackend) SetupSnap(snapFilePath string, si *snap.SideInfo, p progress.Meter) error {
	p.Notify("setup-snap")
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
	if name == "borken" {
		return nil, errors.New(`cannot read info for "borken" snap`)
	}
	// naive emulation for now, always works
	info := &snap.Info{SuggestedName: name, SideInfo: *si}
	info.Type = snap.TypeApp
	if name == "gadget" {
		info.Type = snap.TypeGadget
	}
	if name == "core" {
		info.Type = snap.TypeOS
	}
	return info, nil
}

func (f *fakeSnappyBackend) ClearTrashedData(si *snap.Info) {
	f.ops = append(f.ops, fakeOp{
		op:    "cleanup-trash",
		name:  si.Name(),
		revno: si.Revision,
	})
}

func (f *fakeSnappyBackend) StoreInfo(st *state.State, name, channel string, userID int, flags snapstate.Flags) (*snap.Info, error) {
	return f.ReadInfo(name, &snap.SideInfo{
		RealName: name,
	})
}

func (f *fakeSnappyBackend) CopySnapData(newInfo, oldInfo *snap.Info, p progress.Meter) error {
	p.Notify("copy-data")
	old := "<no-old>"
	if oldInfo != nil {
		old = oldInfo.MountDir()
	}

	if newInfo.MountDir() == f.copySnapDataFailTrigger {
		f.ops = append(f.ops, fakeOp{
			op:   "copy-data.failed",
			name: newInfo.MountDir(),
			old:  old,
		})
		return errors.New("fail")
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

func (f *fakeSnappyBackend) StartSnapServices(info *snap.Info, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "start-snap-services",
		name: info.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) StopSnapServices(info *snap.Info, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "stop-snap-services",
		name: info.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, p progress.Meter) error {
	p.Notify("setup-snap")
	f.ops = append(f.ops, fakeOp{
		op:    "undo-setup-snap",
		name:  s.MountDir(),
		stype: typ,
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

func (f *fakeSnappyBackend) RemoveSnapFiles(s snap.PlaceInfo, typ snap.Type, meter progress.Meter) error {
	meter.Notify("remove-snap-files")
	f.ops = append(f.ops, fakeOp{
		op:    "remove-snap-files",
		name:  s.MountDir(),
		stype: typ,
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

func (f *fakeSnappyBackend) DiscardSnapNamespace(snapName string) error {
	f.ops = append(f.ops, fakeOp{
		op:   "discard-namespace",
		name: snapName,
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

func (f *fakeSnappyBackend) CurrentInfo(curInfo *snap.Info) {
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
		name:  ss.Name(),
		revno: ss.Revision(),
	})
}
