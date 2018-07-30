// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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
	"io"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
)

type fakeOp struct {
	op string

	name    string
	channel string
	path    string
	revno   snap.Revision
	sinfo   snap.SideInfo
	stype   snap.Type

	curSnaps []store.CurrentSnap
	action   store.SnapAction

	old string

	aliases   []*backend.Alias
	rmAliases []*backend.Alias

	userID int
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
	target   string
}

type byName []store.CurrentSnap

func (bna byName) Len() int      { return len(bna) }
func (bna byName) Swap(i, j int) { bna[i], bna[j] = bna[j], bna[i] }
func (bna byName) Less(i, j int) bool {
	return bna[i].InstanceName < bna[j].InstanceName
}

type byAction []*store.SnapAction

func (ba byAction) Len() int      { return len(ba) }
func (ba byAction) Swap(i, j int) { ba[i], ba[j] = ba[j], ba[i] }
func (ba byAction) Less(i, j int) bool {
	if ba[i].Action == ba[j].Action {
		if ba[i].Action == "refresh" {
			return ba[i].SnapID < ba[j].SnapID
		} else {
			return ba[i].InstanceName < ba[j].InstanceName
		}
	}
	return ba[i].Action < ba[j].Action
}

type fakeStore struct {
	storetest.Store

	downloads           []fakeDownload
	refreshRevnos       map[string]snap.Revision
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

	_, instanceKey := snap.SplitInstanceName(spec.Name)
	if instanceKey != "" {
		return nil, fmt.Errorf("internal error: unexpected instance name: %q", spec.Name)
	}
	sspec := snapSpec{
		Name: spec.Name,
	}
	info, err := f.snap(sspec, user)

	userID := 0
	if user != nil {
		userID = user.ID
	}
	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{op: "storesvc-snap", name: spec.Name, revno: info.Revision, userID: userID})

	return info, err
}

type snapSpec struct {
	Name     string
	Channel  string
	Revision snap.Revision
}

func (f *fakeStore) snap(spec snapSpec, user *auth.UserState) (*snap.Info, error) {
	if spec.Revision.Unset() {
		spec.Revision = snap.R(11)
		if spec.Channel == "channel-for-7" {
			spec.Revision.N = 7
		}
	}

	confinement := snap.StrictConfinement

	typ := snap.TypeApp
	switch spec.Name {
	case "core", "ubuntu-core", "some-core":
		typ = snap.TypeOS
	case "some-base":
		typ = snap.TypeBase
	}

	if spec.Name == "snap-unknown" {
		return nil, store.ErrSnapNotFound
	}

	info := &snap.Info{
		Architectures: []string{"all"},
		SideInfo: snap.SideInfo{
			RealName: spec.Name,
			Channel:  spec.Channel,
			SnapID:   spec.Name + "-id",
			Revision: spec.Revision,
		},
		Version: spec.Name,
		DownloadInfo: snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		Confinement: confinement,
		Type:        typ,
	}
	switch spec.Channel {
	case "channel-for-devmode":
		info.Confinement = snap.DevModeConfinement
	case "channel-for-classic":
		info.Confinement = snap.ClassicConfinement
	case "channel-for-paid":
		info.Prices = map[string]float64{"USD": 0.77}
		info.SideInfo.Paid = true
	case "channel-for-private":
		info.SideInfo.Private = true
	case "channel-for-layout":
		info.Layout = map[string]*snap.Layout{
			"/usr": {
				Snap:    info,
				Path:    "/usr",
				Symlink: "$SNAP/usr",
			},
		}
	}

	return info, nil
}

type refreshCand struct {
	snapID           string
	channel          string
	revision         snap.Revision
	block            []snap.Revision
	ignoreValidation bool
}

func (f *fakeStore) lookupRefresh(cand refreshCand) (*snap.Info, error) {
	var name string

	typ := snap.TypeApp
	switch cand.snapID {
	case "":
		panic("store refresh APIs expect snap-ids")
	case "other-snap-id":
		return nil, store.ErrNoUpdateAvailable
	case "fakestore-please-error-on-refresh":
		return nil, fmt.Errorf("failing as requested")
	case "services-snap-id":
		name = "services-snap"
	case "some-snap-id":
		name = "some-snap"
	case "core-snap-id":
		name = "core"
		typ = snap.TypeOS
	case "snap-with-snapd-control-id":
		name = "snap-with-snapd-control"
	case "producer-id":
		name = "producer"
	case "consumer-id":
		name = "consumer"
	case "some-base-id":
		name = "some-base"
		typ = snap.TypeBase
	case "snap-content-plug-id":
		name = "snap-content-plug"
	case "snapd-id":
		name = "snapd"
	case "kernel-id":
		name = "kernel"
		typ = snap.TypeKernel
	default:
		panic(fmt.Sprintf("refresh: unknown snap-id: %s", cand.snapID))
	}

	revno := snap.R(11)
	if r := f.refreshRevnos[cand.snapID]; !r.Unset() {
		revno = r
	}
	confinement := snap.StrictConfinement
	switch cand.channel {
	case "channel-for-7":
		revno = snap.R(7)
	case "channel-for-classic":
		confinement = snap.ClassicConfinement
	case "channel-for-devmode":
		confinement = snap.DevModeConfinement
	}

	info := &snap.Info{
		Type: typ,
		SideInfo: snap.SideInfo{
			RealName: name,
			Channel:  cand.channel,
			SnapID:   cand.snapID,
			Revision: revno,
		},
		Version: name,
		DownloadInfo: snap.DownloadInfo{
			DownloadURL: "https://some-server.com/some/path.snap",
		},
		Confinement:   confinement,
		Architectures: []string{"all"},
	}
	switch cand.channel {
	case "channel-for-layout":
		info.Layout = map[string]*snap.Layout{
			"/usr": {
				Snap:    info,
				Path:    "/usr",
				Symlink: "$SNAP/usr",
			},
		}
	case "channel-for-base":
		info.Base = "some-base"
	}

	var hit snap.Revision
	if cand.revision != revno {
		hit = revno
	}
	for _, blocked := range cand.block {
		if blocked == revno {
			hit = snap.Revision{}
			break
		}
	}

	if !hit.Unset() {
		return info, nil
	}

	return nil, store.ErrNoUpdateAvailable
}

func (f *fakeStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error) {
	if ctx == nil {
		panic("context required")
	}
	f.pokeStateLock()

	if len(currentSnaps) == 0 && len(actions) == 0 {
		return nil, nil
	}
	if len(actions) > 3 {
		panic("fake SnapAction unexpectedly called with more than 3 actions")
	}

	curByID := make(map[string]*store.CurrentSnap, len(currentSnaps))
	curSnaps := make(byName, len(currentSnaps))
	for i, cur := range currentSnaps {
		if cur.InstanceName == "" || cur.SnapID == "" || cur.Revision.Unset() {
			return nil, fmt.Errorf("internal error: incomplete current snap info")
		}
		curByID[cur.SnapID] = cur
		curSnaps[i] = *cur
	}
	sort.Sort(curSnaps)

	userID := 0
	if user != nil {
		userID = user.ID
	}
	if len(curSnaps) == 0 {
		curSnaps = nil
	}
	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{op: "storesvc-snap-action", curSnaps: curSnaps, userID: userID})

	sorted := make(byAction, len(actions))
	copy(sorted, actions)
	sort.Sort(sorted)

	refreshErrors := make(map[string]error)
	installErrors := make(map[string]error)
	var res []*snap.Info
	for _, a := range sorted {
		if a.Action != "install" && a.Action != "refresh" {
			panic("not supported")
		}
		if a.InstanceName == "" {
			return nil, fmt.Errorf("internal error: action without instance name")
		}

		snapName, instanceKey := snap.SplitInstanceName(a.InstanceName)

		if a.Action == "install" {
			spec := snapSpec{
				Name:     snapName,
				Channel:  a.Channel,
				Revision: a.Revision,
			}
			info, err := f.snap(spec, user)
			if err != nil {
				installErrors[a.InstanceName] = err
				continue
			}
			f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{
				op:     "storesvc-snap-action:action",
				action: *a,
				revno:  info.Revision,
				userID: userID,
			})
			if !a.Revision.Unset() {
				info.Channel = ""
			}
			info.InstanceKey = instanceKey
			res = append(res, info)
			continue
		}

		// refresh

		cur := curByID[a.SnapID]
		channel := a.Channel
		if channel == "" {
			channel = cur.TrackingChannel
		}
		ignoreValidation := cur.IgnoreValidation
		if a.Flags&store.SnapActionIgnoreValidation != 0 {
			ignoreValidation = true
		} else if a.Flags&store.SnapActionEnforceValidation != 0 {
			ignoreValidation = false
		}
		cand := refreshCand{
			snapID:           a.SnapID,
			channel:          channel,
			revision:         cur.Revision,
			block:            cur.Block,
			ignoreValidation: ignoreValidation,
		}
		info, err := f.lookupRefresh(cand)
		var hit snap.Revision
		if info != nil {
			if !a.Revision.Unset() {
				info.Revision = a.Revision
			}
			hit = info.Revision
		}
		f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{
			op:     "storesvc-snap-action:action",
			action: *a,
			revno:  hit,
			userID: userID,
		})
		if err == store.ErrNoUpdateAvailable {
			refreshErrors[cur.InstanceName] = err
			continue
		}
		if err != nil {
			return nil, err
		}
		if !a.Revision.Unset() {
			info.Channel = ""
		}
		info.InstanceKey = instanceKey
		res = append(res, info)
	}

	if len(refreshErrors)+len(installErrors) > 0 || len(res) == 0 {
		if len(refreshErrors) == 0 {
			refreshErrors = nil
		}
		if len(installErrors) == 0 {
			installErrors = nil
		}
		return res, &store.SnapActionError{
			NoResults: len(refreshErrors)+len(installErrors)+len(res) == 0,
			Refresh:   refreshErrors,
			Install:   installErrors,
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

	if _, key := snap.SplitInstanceName(name); key != "" {
		return fmt.Errorf("internal error: unsupported download with instance name %q", name)
	}
	var macaroon string
	if user != nil {
		macaroon = user.StoreMacaroon
	}
	f.downloads = append(f.downloads, fakeDownload{
		macaroon: macaroon,
		name:     name,
		target:   targetFn,
	})
	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{op: "storesvc-download", name: name})

	pb.SetTotal(float64(f.fakeTotalProgress))
	pb.Set(float64(f.fakeCurrentProgress))

	return nil
}

func (f *fakeStore) WriteCatalogs(ctx context.Context, _ io.Writer, _ store.SnapAdder) error {
	if ctx == nil {
		panic("context required")
	}
	f.pokeStateLock()
	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{
		op: "x-commands",
	})

	return nil
}

func (f *fakeStore) Sections(ctx context.Context, _ *auth.UserState) ([]string, error) {
	if ctx == nil {
		panic("context required")
	}
	f.pokeStateLock()
	f.fakeBackend.ops = append(f.fakeBackend.ops, fakeOp{
		op: "x-sections",
	})

	return nil, nil
}

type fakeSnappyBackend struct {
	ops fakeOps

	linkSnapFailTrigger     string
	copySnapDataFailTrigger string
	emptyContainer          snap.Container
}

func (f *fakeSnappyBackend) OpenSnapFile(snapFilePath string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
	op := fakeOp{
		op:   "open-snap-file",
		path: snapFilePath,
	}

	if si != nil {
		op.sinfo = *si
	}

	name := filepath.Base(snapFilePath)
	split := strings.Split(name, "_")
	if len(split) >= 2 {
		// <snap>_<rev>.snap
		// <snap>_<instance-key>_<rev>.snap
		name = split[0]
	}

	f.ops = append(f.ops, op)
	return &snap.Info{SuggestedName: name, Architectures: []string{"all"}}, f.emptyContainer, nil
}

func (f *fakeSnappyBackend) SetupSnap(snapFilePath, instanceName string, si *snap.SideInfo, p progress.Meter) (snap.Type, error) {
	p.Notify("setup-snap")
	revno := snap.R(0)
	if si != nil {
		revno = si.Revision
	}
	f.ops = append(f.ops, fakeOp{
		op:    "setup-snap",
		name:  instanceName,
		path:  snapFilePath,
		revno: revno,
	})
	snapType := snap.TypeApp
	switch si.RealName {
	case "core":
		snapType = snap.TypeOS
	case "gadget":
		snapType = snap.TypeGadget
	}
	return snapType, nil
}

func (f *fakeSnappyBackend) ReadInfo(name string, si *snap.SideInfo) (*snap.Info, error) {
	if name == "borken" && si.Revision == snap.R(2) {
		return nil, errors.New(`cannot read info for "borken" snap`)
	}
	if name == "not-there" && si.Revision == snap.R(2) {
		return nil, &snap.NotFoundError{Snap: name, Revision: si.Revision}
	}
	snapName, instanceKey := snap.SplitInstanceName(name)
	// naive emulation for now, always works
	info := &snap.Info{
		SuggestedName: snapName,
		SideInfo:      *si,
		Architectures: []string{"all"},
		Type:          snap.TypeApp,
	}
	if strings.Contains(snapName, "alias-snap") {
		// only for the switch below
		snapName = "alias-snap"
	}
	switch snapName {
	case "gadget":
		info.Type = snap.TypeGadget
	case "core":
		info.Type = snap.TypeOS
	case "services-snap":
		var err error
		info, err = snap.InfoFromSnapYaml([]byte(`name: services-snap
apps:
  svc1:
    daemon: simple
  svc2:
    daemon: simple
`))
		if err != nil {
			panic(err)
		}
		info.SideInfo = *si
	case "alias-snap":
		var err error
		info, err = snap.InfoFromSnapYaml([]byte(`name: alias-snap
apps:
  cmd1:
  cmd2:
  cmd3:
  cmd4:
  cmd5:
  cmddaemon:
    daemon: simple
`))
		if err != nil {
			panic(err)
		}
		info.SideInfo = *si
	}

	info.InstanceKey = instanceKey
	return info, nil
}

func (f *fakeSnappyBackend) ClearTrashedData(si *snap.Info) {
	f.ops = append(f.ops, fakeOp{
		op:    "cleanup-trash",
		name:  si.InstanceName(),
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
			path: newInfo.MountDir(),
			old:  old,
		})
		return errors.New("fail")
	}

	f.ops = append(f.ops, fakeOp{
		op:   "copy-data",
		path: newInfo.MountDir(),
		old:  old,
	})
	return nil
}

func (f *fakeSnappyBackend) LinkSnap(info *snap.Info, model *asserts.Model) error {
	if info.MountDir() == f.linkSnapFailTrigger {
		f.ops = append(f.ops, fakeOp{
			op:   "link-snap.failed",
			path: info.MountDir(),
		})
		return errors.New("fail")
	}

	f.ops = append(f.ops, fakeOp{
		op:   "link-snap",
		path: info.MountDir(),
	})
	return nil
}

func svcSnapMountDir(svcs []*snap.AppInfo) string {
	if len(svcs) == 0 {
		return "<no services>"
	}
	if svcs[0].Snap == nil {
		return "<snapless service>"
	}
	return svcs[0].Snap.MountDir()
}

func (f *fakeSnappyBackend) StartServices(svcs []*snap.AppInfo, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "start-snap-services",
		path: svcSnapMountDir(svcs),
	})
	return nil
}

func (f *fakeSnappyBackend) StopServices(svcs []*snap.AppInfo, reason snap.ServiceStopReason, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   fmt.Sprintf("stop-snap-services:%s", reason),
		path: svcSnapMountDir(svcs),
	})
	return nil
}

func (f *fakeSnappyBackend) UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, p progress.Meter) error {
	p.Notify("setup-snap")
	f.ops = append(f.ops, fakeOp{
		op:    "undo-setup-snap",
		name:  s.InstanceName(),
		path:  s.MountDir(),
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
		path: newInfo.MountDir(),
		old:  old,
	})
	return nil
}

func (f *fakeSnappyBackend) UnlinkSnap(info *snap.Info, meter progress.Meter) error {
	meter.Notify("unlink")
	f.ops = append(f.ops, fakeOp{
		op:   "unlink-snap",
		path: info.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapFiles(s snap.PlaceInfo, typ snap.Type, meter progress.Meter) error {
	meter.Notify("remove-snap-files")
	f.ops = append(f.ops, fakeOp{
		op:    "remove-snap-files",
		path:  s.MountDir(),
		stype: typ,
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapData(info *snap.Info) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-data",
		path: info.MountDir(),
	})
	return nil
}

func (f *fakeSnappyBackend) RemoveSnapCommonData(info *snap.Info) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-common-data",
		path: info.MountDir(),
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
		name:  snapsup.InstanceName(),
		revno: snapsup.Revision(),
	})
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
