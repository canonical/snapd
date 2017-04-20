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
	"sort"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

type fakeOp struct {
	op string

	name    string
	channel string
	revno   snap.Revision
	sinfo   snap.SideInfo
	stype   snap.Type
	cand    store.RefreshCandidate

	old string

	aliases   []*backend.Alias
	rmAliases []*backend.Alias
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

func (f *fakeStore) SnapInfo(spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	f.pokeStateLock()

	if spec.Revision.Unset() {
		spec.Revision = snap.R(11)
		if spec.Channel == "channel-for-7" {
			spec.Revision.N = 7
		}
	}

	confinement := snap.StrictConfinement
	switch spec.Channel {
	case "channel-for-devmode":
		confinement = snap.DevModeConfinement
	case "channel-for-classic":
		confinement = snap.ClassicConfinement
	}

	typ := snap.TypeApp
	if spec.Name == "some-core" {
		typ = snap.TypeOS
	}

	info := &snap.Info{
		Architectures: []string{"all"},
		SideInfo: snap.SideInfo{
			RealName: spec.Name,
			Channel:  spec.Channel,
			SnapID:   "snapIDsnapidsnapidsnapidsnapidsn",
			Revision: spec.Revision,
		},
		Version: spec.Name,
		DownloadInfo: snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		Confinement: confinement,
		Type:        typ,
	}
	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{op: "storesvc-snap", name: spec.Name, revno: spec.Revision})

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
	if len(cands) > 2 {
		panic("ListRefresh unexpectedly called with more than two candidates")
	}

	var res []*snap.Info
	for _, cand := range cands {
		snapID := cand.SnapID

		if snapID == "" || snapID == "other-snap-id" {
			continue
		}

		if snapID == "fakestore-please-error-on-refresh" {
			return nil, fmt.Errorf("failing as requested")
		}

		var name string
		switch snapID {
		case "some-snap-id":
			name = "some-snap"
		case "core-snap-id":
			name = "core"
		default:
			panic(fmt.Sprintf("ListRefresh: unknown snap-id: %s", snapID))
		}

		revno := snap.R(11)
		confinement := snap.StrictConfinement
		switch cand.Channel {
		case "channel-for-7":
			revno = snap.R(7)
		case "channel-for-classic":
			confinement = snap.ClassicConfinement
		case "channel-for-devmode":
			confinement = snap.DevModeConfinement
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
			Confinement:   confinement,
			Architectures: []string{"all"},
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

		if !hit.Unset() {
			res = append(res, info)
		}
	}

	return res, nil
}

func (f *fakeStore) SuggestedCurrency() string {
	f.pokeStateLock()

	return "XTS"
}

func (f *fakeStore) Download(ctx context.Context, name, targetFn string, snapInfo *snap.DownloadInfo, pb progress.Meter, user *auth.UserState) error {
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

func (f *fakeStore) Sections(user *auth.UserState) ([]string, error) {
	panic("Sections called")
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
	info := &snap.Info{
		SuggestedName: name,
		SideInfo:      *si,
		Architectures: []string{"all"},
	}
	info.Type = snap.TypeApp
	if name == "gadget" {
		info.Type = snap.TypeGadget
	}
	if name == "core" {
		info.Type = snap.TypeOS
	}
	if name == "alias-snap" {
		var err error
		info, err = snap.InfoFromSnapYaml([]byte(`name: alias-snap
apps:
  cmd1:
    aliases: [alias1, alias1.cmd1]
  cmd2:
    aliases: [alias2]
  cmd3:
    aliases: [alias3]
  cmd4:
    aliases: [alias4]
  cmd5:
    aliases: [alias5]
`))
		if err != nil {
			panic(err)
		}
		info.SideInfo = *si
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

func (f *fakeSnappyBackend) ForeignTask(kind string, status state.Status, snapsup *snapstate.SnapSetup) {
	f.ops = append(f.ops, fakeOp{
		op:    kind + ":" + status.String(),
		name:  snapsup.Name(),
		revno: snapsup.Revision(),
	})
}

func (f *fakeSnappyBackend) MatchingAliases(aliases []*backend.Alias) ([]*backend.Alias, error) {
	f.ops = append(f.ops, fakeOp{
		op:      "matching-aliases",
		aliases: aliases,
	})
	return aliases, nil
}

func (f *fakeSnappyBackend) MissingAliases(aliases []*backend.Alias) ([]*backend.Alias, error) {
	f.ops = append(f.ops, fakeOp{
		op:      "missing-aliases",
		aliases: aliases,
	})
	return aliases, nil
}

type byAlias []*backend.Alias

func (ba byAlias) Len() int      { return len(ba) }
func (ba byAlias) Swap(i, j int) { ba[i], ba[j] = ba[j], ba[i] }
func (ba byAlias) Less(i, j int) bool {
	return ba[i].Name < ba[j].Name
}

func (f *fakeSnappyBackend) UpdateAliases(add []*backend.Alias, remove []*backend.Alias) error {
	if len(add) != 0 {
		add = append([]*backend.Alias(nil), add...)
		sort.Sort(byAlias(add))
	}
	if len(remove) != 0 {
		remove = append([]*backend.Alias(nil), remove...)
		sort.Sort(byAlias(remove))
	}
	f.ops = append(f.ops, fakeOp{
		op:        "update-aliases",
		aliases:   add,
		rmAliases: remove,
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapAliases(snapName string) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-aliases",
		name: snapName,
	})
	return nil
}
