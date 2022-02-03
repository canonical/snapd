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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/cmd/snaplock/runinhibit"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/timings"
)

type fakeOp struct {
	op string

	name  string
	path  string
	revno snap.Revision
	sinfo snap.SideInfo
	stype snap.Type

	curSnaps []store.CurrentSnap
	action   store.SnapAction

	old string

	aliases   []*backend.Alias
	rmAliases []*backend.Alias

	userID int

	otherInstances         bool
	unlinkFirstInstallUndo bool
	skipKernelExtraction   bool

	services         []string
	disabledServices []string

	vitalityRank int

	inhibitHint runinhibit.Hint

	requireSnapdTooling bool

	dirOpts *dirs.SnapDirOptions
}

type fakeOps []fakeOp

func (ops fakeOps) MustFindOp(c *C, opName string) *fakeOp {
	for _, op := range ops {
		if op.op == opName {
			return &op
		}
	}
	c.Errorf("cannot find operation with op: %q, all ops: %v", opName, ops.Ops())
	c.FailNow()
	return nil
}

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
	opts     *store.DownloadOptions
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
	// snap -> error map for simulating download errors
	downloadError   map[string]error
	state           *state.State
	seenPrivacyKeys map[string]bool
}

func (f *fakeStore) pokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	f.state.Lock()
	f.state.Unlock()
}

func (f *fakeStore) SnapInfo(ctx context.Context, spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	f.pokeStateLock()

	_, instanceKey := snap.SplitInstanceName(spec.Name)
	if instanceKey != "" {
		return nil, fmt.Errorf("internal error: unexpected instance name: %q", spec.Name)
	}
	sspec := snapSpec{
		Name: spec.Name,
	}
	info, err := f.snap(sspec)

	userID := 0
	if user != nil {
		userID = user.ID
	}
	f.fakeBackend.appendOp(&fakeOp{op: "storesvc-snap", name: spec.Name, revno: info.Revision, userID: userID})

	return info, err
}

type snapSpec struct {
	Name     string
	Channel  string
	Revision snap.Revision
	Cohort   string
}

func (f *fakeStore) snap(spec snapSpec) (*snap.Info, error) {
	if spec.Revision.Unset() {
		switch {
		case spec.Cohort != "":
			spec.Revision = snap.R(666)
		case spec.Channel == "channel-for-7":
			spec.Revision = snap.R(7)
		default:
			spec.Revision = snap.R(11)
		}
	}

	confinement := snap.StrictConfinement

	typ := snap.TypeApp
	epoch := snap.E("1*")
	switch spec.Name {
	case "core", "core16", "ubuntu-core", "some-core":
		typ = snap.TypeOS
	case "some-base", "other-base", "some-other-base", "yet-another-base", "core18":
		typ = snap.TypeBase
	case "some-kernel":
		typ = snap.TypeKernel
	case "some-gadget", "brand-gadget":
		typ = snap.TypeGadget
	case "some-snapd":
		typ = snap.TypeSnapd
	case "snapd":
		typ = snap.TypeSnapd
	case "some-snap-now-classic":
		confinement = "classic"
	case "some-epoch-snap":
		epoch = snap.E("42")
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
			Size:        5,
		},
		Confinement: confinement,
		SnapType:    typ,
		Epoch:       epoch,
	}
	switch spec.Channel {
	case "channel-no-revision":
		return nil, &store.RevisionNotAvailableError{}
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
	case "channel-for-base/stable":
		info.Base = "some-base"
	case "channel-for-user-daemon":
		info.Apps = map[string]*snap.AppInfo{
			"user-daemon": {
				Snap:        info,
				Name:        "user-daemon",
				Daemon:      "simple",
				DaemonScope: "user",
			},
		}
	case "channel-for-dbus-activation":
		slot := &snap.SlotInfo{
			Snap:      info,
			Name:      "dbus-slot",
			Interface: "dbus",
			Attrs: map[string]interface{}{
				"bus":  "system",
				"name": "org.example.Foo",
			},
			Apps: make(map[string]*snap.AppInfo),
		}
		info.Apps = map[string]*snap.AppInfo{
			"dbus-daemon": {
				Snap:        info,
				Name:        "dbus-daemon",
				Daemon:      "simple",
				DaemonScope: snap.SystemDaemon,
				ActivatesOn: []*snap.SlotInfo{slot},
				Slots: map[string]*snap.SlotInfo{
					slot.Name: slot,
				},
			},
		}
		slot.Apps["dbus-daemon"] = info.Apps["dbus-daemon"]
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
	epoch := snap.E("1*")

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
	case "some-other-snap-id":
		name = "some-other-snap"
	case "some-epoch-snap-id":
		name = "some-epoch-snap"
		epoch = snap.E("42")
	case "some-snap-now-classic-id":
		name = "some-snap-now-classic"
	case "some-snap-was-classic-id":
		name = "some-snap-was-classic"
	case "core-snap-id":
		name = "core"
		typ = snap.TypeOS
	case "core18-snap-id":
		name = "core18"
		typ = snap.TypeBase
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
	case "snap-content-slot-id":
		name = "snap-content-slot"
	case "snapd-snap-id":
		name = "snapd"
		typ = snap.TypeSnapd
	case "kernel-id":
		name = "kernel"
		typ = snap.TypeKernel
	case "brand-kernel-id":
		name = "brand-kernel"
		typ = snap.TypeKernel
	case "brand-gadget-id":
		name = "brand-gadget"
		typ = snap.TypeGadget
	case "alias-snap-id":
		name = "snap-id"
	case "snap-c-id":
		name = "snap-c"
	case "outdated-consumer-id":
		name = "outdated-consumer"
	case "outdated-producer-id":
		name = "outdated-producer"
	default:
		panic(fmt.Sprintf("refresh: unknown snap-id: %s", cand.snapID))
	}

	revno := snap.R(11)
	if r := f.refreshRevnos[cand.snapID]; !r.Unset() {
		revno = r
	}
	confinement := snap.StrictConfinement
	switch cand.channel {
	case "channel-for-7/stable":
		revno = snap.R(7)
	case "channel-for-classic/stable":
		confinement = snap.ClassicConfinement
	case "channel-for-devmode/stable":
		confinement = snap.DevModeConfinement
	}
	if name == "some-snap-now-classic" {
		confinement = "classic"
	}

	info := &snap.Info{
		SnapType: typ,
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
		Epoch:         epoch,
	}

	if name == "outdated-consumer" {
		info.Plugs = map[string]*snap.PlugInfo{
			"content-plug": {
				Snap:      info,
				Interface: "content",
				Attrs: map[string]interface{}{
					"content":          "some-content",
					"default-provider": "outdated-producer",
				},
			},
		}
	} else if name == "outdated-producer" {
		info.Slots = map[string]*snap.SlotInfo{
			"content-slot": {
				Snap:      info,
				Interface: "content",
				Attrs:     map[string]interface{}{"content": "some-content"},
			},
		}
	}

	switch cand.channel {
	case "channel-for-layout/stable":
		info.Layout = map[string]*snap.Layout{
			"/usr": {
				Snap:    info,
				Path:    "/usr",
				Symlink: "$SNAP/usr",
			},
		}
	case "channel-for-base/stable":
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

func (f *fakeStore) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if ctx == nil {
		panic("context required")
	}
	f.pokeStateLock()
	if assertQuery != nil {
		panic("no assertion query support")
	}

	if len(currentSnaps) == 0 && len(actions) == 0 {
		return nil, nil, nil
	}
	if len(actions) > 4 {
		panic("fake SnapAction unexpectedly called with more than 3 actions")
	}

	curByInstanceName := make(map[string]*store.CurrentSnap, len(currentSnaps))
	curSnaps := make(byName, len(currentSnaps))
	for i, cur := range currentSnaps {
		if cur.InstanceName == "" || cur.SnapID == "" || cur.Revision.Unset() {
			return nil, nil, fmt.Errorf("internal error: incomplete current snap info")
		}
		curByInstanceName[cur.InstanceName] = cur
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
	f.fakeBackend.appendOp(&fakeOp{
		op:       "storesvc-snap-action",
		curSnaps: curSnaps,
		userID:   userID,
	})

	if f.seenPrivacyKeys == nil {
		// so that checks don't topple over this being uninitialized
		f.seenPrivacyKeys = make(map[string]bool)
	}
	if opts != nil && opts.PrivacyKey != "" {
		f.seenPrivacyKeys[opts.PrivacyKey] = true
	}

	sorted := make(byAction, len(actions))
	copy(sorted, actions)
	sort.Sort(sorted)

	refreshErrors := make(map[string]error)
	installErrors := make(map[string]error)
	var res []store.SnapActionResult
	for _, a := range sorted {
		if a.Action != "install" && a.Action != "refresh" {
			panic("not supported")
		}
		if a.InstanceName == "" {
			return nil, nil, fmt.Errorf("internal error: action without instance name")
		}

		snapName, instanceKey := snap.SplitInstanceName(a.InstanceName)

		if a.Action == "install" {
			spec := snapSpec{
				Name:     snapName,
				Channel:  a.Channel,
				Revision: a.Revision,
				Cohort:   a.CohortKey,
			}
			info, err := f.snap(spec)
			if err != nil {
				installErrors[a.InstanceName] = err
				continue
			}
			f.fakeBackend.appendOp(&fakeOp{
				op:     "storesvc-snap-action:action",
				action: *a,
				revno:  info.Revision,
				userID: userID,
			})
			if !a.Revision.Unset() {
				info.Channel = ""
			}
			info.InstanceKey = instanceKey
			sar := store.SnapActionResult{Info: info}
			if strings.HasSuffix(snapName, "-with-default-track") && strutil.ListContains([]string{"stable", "candidate", "beta", "edge"}, a.Channel) {
				sar.RedirectChannel = "2.0/" + a.Channel
			}
			res = append(res, sar)
			continue
		}

		// refresh

		cur := curByInstanceName[a.InstanceName]
		if cur == nil {
			return nil, nil, fmt.Errorf("internal error: no matching current snap for %q", a.InstanceName)
		}
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
			return nil, nil, err
		}
		if !a.Revision.Unset() {
			info.Channel = ""
		}
		info.InstanceKey = instanceKey
		res = append(res, store.SnapActionResult{Info: info})
	}

	if len(refreshErrors)+len(installErrors) > 0 || len(res) == 0 {
		if len(refreshErrors) == 0 {
			refreshErrors = nil
		}
		if len(installErrors) == 0 {
			installErrors = nil
		}
		return res, nil, &store.SnapActionError{
			NoResults: len(refreshErrors)+len(installErrors)+len(res) == 0,
			Refresh:   refreshErrors,
			Install:   installErrors,
		}
	}

	return res, nil, nil
}

func (f *fakeStore) SuggestedCurrency() string {
	f.pokeStateLock()

	return "XTS"
}

func (f *fakeStore) Download(ctx context.Context, name, targetFn string, snapInfo *snap.DownloadInfo, pb progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error {
	f.pokeStateLock()

	if _, key := snap.SplitInstanceName(name); key != "" {
		return fmt.Errorf("internal error: unsupported download with instance name %q", name)
	}
	var macaroon string
	if user != nil {
		macaroon = user.StoreMacaroon
	}
	// only add the options if they contain anything interesting
	if *dlOpts == (store.DownloadOptions{}) {
		dlOpts = nil
	}
	f.downloads = append(f.downloads, fakeDownload{
		macaroon: macaroon,
		name:     name,
		target:   targetFn,
		opts:     dlOpts,
	})
	f.fakeBackend.appendOp(&fakeOp{op: "storesvc-download", name: name})

	pb.SetTotal(float64(f.fakeTotalProgress))
	pb.Set(float64(f.fakeCurrentProgress))

	if e, ok := f.downloadError[name]; ok {
		return e
	}

	return nil
}

func (f *fakeStore) WriteCatalogs(ctx context.Context, _ io.Writer, _ store.SnapAdder) error {
	if ctx == nil {
		panic("context required")
	}
	f.pokeStateLock()

	f.fakeBackend.appendOp(&fakeOp{
		op: "x-commands",
	})

	return nil
}

func (f *fakeStore) Sections(ctx context.Context, _ *auth.UserState) ([]string, error) {
	if ctx == nil {
		panic("context required")
	}
	f.pokeStateLock()

	f.fakeBackend.appendOp(&fakeOp{
		op: "x-sections",
	})

	return nil, nil
}

type fakeSnappyBackend struct {
	ops fakeOps
	mu  sync.Mutex

	linkSnapWaitCh      chan int
	linkSnapWaitTrigger string
	linkSnapFailTrigger string
	linkSnapMaybeReboot bool
	linkSnapRebootFor   map[string]bool

	copySnapDataFailTrigger string
	emptyContainer          snap.Container

	servicesCurrentlyDisabled []string

	lockDir string

	// TODO cleanup triggers above
	maybeInjectErr func(*fakeOp) error
}

func (f *fakeSnappyBackend) maybeErrForLastOp() error {
	if f.maybeInjectErr == nil {
		return nil
	}
	if len(f.ops) == 0 {
		return nil
	}
	return f.maybeInjectErr(&f.ops[len(f.ops)-1])
}

func (f *fakeSnappyBackend) OpenSnapFile(snapFilePath string, si *snap.SideInfo) (*snap.Info, snap.Container, error) {
	op := fakeOp{
		op:   "open-snap-file",
		path: snapFilePath,
	}

	if si != nil {
		op.sinfo = *si
	}

	var info *snap.Info
	if !osutil.IsDirectory(snapFilePath) {
		name := filepath.Base(snapFilePath)
		split := strings.Split(name, "_")
		if len(split) >= 2 {
			// <snap>_<rev>.snap
			// <snap>_<instance-key>_<rev>.snap
			name = split[0]
		}

		info = &snap.Info{SuggestedName: name, Architectures: []string{"all"}}
		if name == "some-snap-now-classic" {
			info.Confinement = "classic"
		}
		if name == "some-epoch-snap" {
			info.Epoch = snap.E("42")
		} else {
			info.Epoch = snap.E("1*")
		}
	} else {
		// for snap try only
		snapf, err := snapfile.Open(snapFilePath)
		if err != nil {
			return nil, nil, err
		}

		info, err = snap.ReadInfoFromSnapFile(snapf, si)
		if err != nil {
			return nil, nil, err
		}
	}

	if info == nil {
		return nil, nil, fmt.Errorf("internal error: no mocked snap for %q", snapFilePath)
	}
	f.appendOp(&op)
	return info, f.emptyContainer, nil
}

// XXX: this is now something that is overridden by tests that need a
//      different service setup so it should be configurable and part
//      of the fakeSnappyBackend?
var servicesSnapYaml = `name: services-snap
apps:
  svc1:
    daemon: simple
    before: [svc3]
  svc2:
    daemon: simple
    after: [svc1]
  svc3:
    daemon: simple
    before: [svc2]
`

func (f *fakeSnappyBackend) SetupSnap(snapFilePath, instanceName string, si *snap.SideInfo, dev boot.Device, opts *backend.SetupSnapOptions, p progress.Meter) (snap.Type, *backend.InstallRecord, error) {
	p.Notify("setup-snap")
	revno := snap.R(0)
	if si != nil {
		revno = si.Revision
	}
	f.appendOp(&fakeOp{
		op:    "setup-snap",
		name:  instanceName,
		path:  snapFilePath,
		revno: revno,

		skipKernelExtraction: opts != nil && opts.SkipKernelExtraction,
	})
	snapType := snap.TypeApp
	switch si.RealName {
	case "core":
		snapType = snap.TypeOS
	case "gadget":
		snapType = snap.TypeGadget
	}
	if instanceName == "borken-in-setup" {
		return snapType, nil, fmt.Errorf("cannot install snap %q", instanceName)
	}
	if instanceName == "some-snap-no-install-record" {
		return snapType, nil, nil
	}
	return snapType, &backend.InstallRecord{}, nil
}

func (f *fakeSnappyBackend) ReadInfo(name string, si *snap.SideInfo) (*snap.Info, error) {
	if name == "borken" && si.Revision == snap.R(2) {
		return nil, errors.New(`cannot read info for "borken" snap`)
	}
	if name == "borken-undo-setup" && si.Revision == snap.R(2) {
		return nil, errors.New(`cannot read info for "borken-undo-setup" snap`)
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
		SnapType:      snap.TypeApp,
		Epoch:         snap.E("1*"),
	}
	if strings.Contains(snapName, "alias-snap") {
		// only for the switch below
		snapName = "alias-snap"
	}
	switch snapName {
	case "snap-with-empty-epoch":
		info.Epoch = snap.Epoch{}
	case "some-epoch-snap":
		info.Epoch = snap.E("13")
	case "some-snap-with-base":
		info.Base = "core18"
	case "gadget", "brand-gadget":
		info.SnapType = snap.TypeGadget
	case "core":
		info.SnapType = snap.TypeOS
	case "snapd":
		info.SnapType = snap.TypeSnapd
	case "services-snap":
		var err error
		// fix services after/before so that there is only one solution
		// to dependency ordering
		info, err = snap.InfoFromSnapYaml([]byte(servicesSnapYaml))
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
	f.appendOp(&fakeOp{
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

func (f *fakeSnappyBackend) CopySnapData(newInfo, oldInfo *snap.Info, p progress.Meter, opts *dirs.SnapDirOptions) error {
	p.Notify("copy-data")
	op := &fakeOp{
		op:   "copy-data",
		path: newInfo.MountDir(),
		old:  "<no-old>",
	}

	if oldInfo != nil {
		op.old = oldInfo.MountDir()
	}

	if opts != nil && opts.HiddenSnapDataDir {
		op.dirOpts = opts
	}

	if newInfo.MountDir() == f.copySnapDataFailTrigger {
		op.op = "copy-data.failed"
		f.appendOp(op)
		return errors.New("fail")
	}

	f.appendOp(op)
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) LinkSnap(info *snap.Info, dev boot.Device, linkCtx backend.LinkContext, tm timings.Measurer) (rebootRequired bool, err error) {
	if info.MountDir() == f.linkSnapWaitTrigger {
		f.linkSnapWaitCh <- 1
		<-f.linkSnapWaitCh
	}

	vitalityRank := 0
	if linkCtx.ServiceOptions != nil {
		vitalityRank = linkCtx.ServiceOptions.VitalityRank
	}

	op := fakeOp{
		op:   "link-snap",
		path: info.MountDir(),

		vitalityRank:        vitalityRank,
		requireSnapdTooling: linkCtx.RequireMountedSnapdSnap,
	}

	if info.MountDir() == f.linkSnapFailTrigger {
		op.op = "link-snap.failed"
		f.ops = append(f.ops, op)
		return false, errors.New("fail")
	}

	f.appendOp(&op)

	reboot := false
	if f.linkSnapMaybeReboot {
		reboot = info.InstanceName() == dev.Base() ||
			(f.linkSnapRebootFor != nil && f.linkSnapRebootFor[info.InstanceName()])
	}

	return reboot, nil
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

func (f *fakeSnappyBackend) StartServices(svcs []*snap.AppInfo, disabledSvcs []string, meter progress.Meter, tm timings.Measurer) error {
	services := make([]string, 0, len(svcs))
	for _, svc := range svcs {
		services = append(services, svc.Name)
	}
	op := fakeOp{
		op:       "start-snap-services",
		path:     svcSnapMountDir(svcs),
		services: services,
	}
	// only add the services to the op if there's something to add
	if len(disabledSvcs) != 0 {
		op.disabledServices = disabledSvcs
	}
	f.appendOp(&op)
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) StopServices(svcs []*snap.AppInfo, reason snap.ServiceStopReason, meter progress.Meter, tm timings.Measurer) error {
	f.appendOp(&fakeOp{
		op:   fmt.Sprintf("stop-snap-services:%s", reason),
		path: svcSnapMountDir(svcs),
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) ServicesEnableState(info *snap.Info, meter progress.Meter) (map[string]bool, error) {
	// return the disabled services as disabled and nothing else
	m := make(map[string]bool)
	for _, svc := range f.servicesCurrentlyDisabled {
		m[svc] = false
	}

	f.appendOp(&fakeOp{
		op:               "current-snap-service-states",
		disabledServices: f.servicesCurrentlyDisabled,
	})

	return m, f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) QueryDisabledServices(info *snap.Info, meter progress.Meter) ([]string, error) {
	var l []string

	m, err := f.ServicesEnableState(info, meter)
	if err != nil {
		return nil, err
	}
	for name, enabled := range m {
		if !enabled {
			l = append(l, name)
		}
	}

	// XXX: add a fakeOp here?

	return l, nil
}

func (f *fakeSnappyBackend) UndoSetupSnap(s snap.PlaceInfo, typ snap.Type, installRecord *backend.InstallRecord, dev boot.Device, p progress.Meter) error {
	p.Notify("setup-snap")
	f.appendOp(&fakeOp{
		op:    "undo-setup-snap",
		name:  s.InstanceName(),
		path:  s.MountDir(),
		stype: typ,
	})
	if s.InstanceName() == "borken-undo-setup" {
		return errors.New(`cannot undo setup of "borken-undo-setup" snap`)
	}
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) UndoCopySnapData(newInfo *snap.Info, oldInfo *snap.Info, p progress.Meter, opts *dirs.SnapDirOptions) error {
	p.Notify("undo-copy-data")
	old := "<no-old>"
	if oldInfo != nil {
		old = oldInfo.MountDir()
	}
	f.appendOp(&fakeOp{
		op:   "undo-copy-snap-data",
		path: newInfo.MountDir(),
		old:  old,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) UnlinkSnap(info *snap.Info, linkCtx backend.LinkContext, meter progress.Meter) error {
	meter.Notify("unlink")
	f.appendOp(&fakeOp{
		op:   "unlink-snap",
		path: info.MountDir(),

		unlinkFirstInstallUndo: linkCtx.FirstInstall,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RemoveSnapFiles(s snap.PlaceInfo, typ snap.Type, installRecord *backend.InstallRecord, dev boot.Device, meter progress.Meter) error {
	meter.Notify("remove-snap-files")
	f.appendOp(&fakeOp{
		op:    "remove-snap-files",
		path:  s.MountDir(),
		stype: typ,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RemoveSnapData(info *snap.Info, opts *dirs.SnapDirOptions) error {
	f.appendOp(&fakeOp{
		op:   "remove-snap-data",
		path: info.MountDir(),
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RemoveSnapCommonData(info *snap.Info, opts *dirs.SnapDirOptions) error {
	f.appendOp(&fakeOp{
		op:   "remove-snap-common-data",
		path: info.MountDir(),
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RemoveSnapDataDir(info *snap.Info, otherInstances bool) error {
	f.ops = append(f.ops, fakeOp{
		op:             "remove-snap-data-dir",
		name:           info.InstanceName(),
		path:           snap.BaseDataDir(info.SnapName()),
		otherInstances: otherInstances,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RemoveSnapMountUnits(s snap.PlaceInfo, meter progress.Meter) error {
	f.ops = append(f.ops, fakeOp{
		op:   "remove-snap-mount-units",
		name: s.InstanceName(),
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RemoveSnapDir(s snap.PlaceInfo, otherInstances bool) error {
	f.ops = append(f.ops, fakeOp{
		op:             "remove-snap-dir",
		name:           s.InstanceName(),
		path:           snap.BaseDir(s.SnapName()),
		otherInstances: otherInstances,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) DiscardSnapNamespace(snapName string) error {
	f.appendOp(&fakeOp{
		op:   "discard-namespace",
		name: snapName,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RemoveSnapInhibitLock(snapName string) error {
	f.appendOp(&fakeOp{
		op:   "remove-inhibit-lock",
		name: snapName,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) Candidate(sideInfo *snap.SideInfo) {
	var sinfo snap.SideInfo
	if sideInfo != nil {
		sinfo = *sideInfo
	}
	f.appendOp(&fakeOp{
		op:    "candidate",
		sinfo: sinfo,
	})
}

func (f *fakeSnappyBackend) CurrentInfo(curInfo *snap.Info) {
	old := "<no-current>"
	if curInfo != nil {
		old = curInfo.MountDir()
	}
	f.appendOp(&fakeOp{
		op:  "current",
		old: old,
	})
}

func (f *fakeSnappyBackend) ForeignTask(kind string, status state.Status, snapsup *snapstate.SnapSetup) error {
	f.appendOp(&fakeOp{
		op:    kind + ":" + status.String(),
		name:  snapsup.InstanceName(),
		revno: snapsup.Revision(),
	})
	return f.maybeErrForLastOp()
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
	f.appendOp(&fakeOp{
		op:        "update-aliases",
		aliases:   add,
		rmAliases: remove,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RemoveSnapAliases(snapName string) error {
	f.appendOp(&fakeOp{
		op:   "remove-snap-aliases",
		name: snapName,
	})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) RunInhibitSnapForUnlink(info *snap.Info, hint runinhibit.Hint, decision func() error) (lock *osutil.FileLock, err error) {
	f.appendOp(&fakeOp{
		op:          "run-inhibit-snap-for-unlink",
		name:        info.InstanceName(),
		inhibitHint: hint,
	})
	if err := decision(); err != nil {
		return nil, err
	}
	if f.lockDir == "" {
		f.lockDir = os.TempDir()
	}
	// XXX: returning a real lock is somewhat annoying
	return osutil.NewFileLock(filepath.Join(f.lockDir, info.InstanceName()+".lock"))
}

func (f *fakeSnappyBackend) HideSnapData(snapName string) error {
	f.appendOp(&fakeOp{op: "hide-snap-data", name: snapName})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) UndoHideSnapData(snapName string) error {
	f.appendOp(&fakeOp{op: "undo-hide-snap-data", name: snapName})
	return f.maybeErrForLastOp()
}

func (f *fakeSnappyBackend) appendOp(op *fakeOp) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ops = append(f.ops, *op)
}
