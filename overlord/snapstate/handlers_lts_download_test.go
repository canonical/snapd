// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/timings"
)

type ltsDownloadSuite struct {
	baseHandlerSuite

	fakeStore *fakeStore
}

var _ = Suite(&ltsDownloadSuite{})

// ltsCachedStore is the default/cached store during a store-switch remodel.
// SnapAction must not be used for LTS resolve when DeviceCtx.Store() is set.
type ltsCachedStore struct {
	storetest.Store
}

func (ltsCachedStore) SnapAction(context.Context, []*store.CurrentSnap, []*store.SnapAction, store.AssertionQuery, *auth.UserState, *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	return nil, nil, fmt.Errorf("cached store must not be used for LTS resolve")
}

func (s *ltsDownloadSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	s.fakeStore = &fakeStore{
		state:       s.state,
		fakeBackend: s.fakeBackend,
		expectedDefaultDownloadOpts: &store.DownloadOptions{
			LeavePartialOnError: true,
		},
	}
	s.state.Lock()
	defer s.state.Unlock()
	snapstate.ReplaceStore(s.state, s.fakeStore)
	s.state.Set("refresh-privacy-key", "privacy-key")

	s.AddCleanup(snapstatetest.UseFallbackDeviceModel())

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return snapasserts.NewValidationSets(), nil
	})
	s.AddCleanup(restore)

	restore = snapstate.MockValidationSetsFromKeys(func(st *state.State, keys []snapasserts.ValidationSetKey) (*snapasserts.ValidationSets, error) {
		if len(keys) == 0 {
			return snapasserts.NewValidationSets(), nil
		}
		return nil, fmt.Errorf("unexpected ValidationSetsFromKeys with keys %v", keys)
	})
	s.AddCleanup(restore)
}

// makeSnapdBlobWithLTSTracks builds a minimal snapd squashfs at a temp path
// whose /usr/lib/snapd/info contains the given SNAPD_LTS_TRACKS JSON value.
// An empty tracksJSON omits that key.
func makeSnapdBlobWithLTSTracks(c *C, tracksJSON string) string {
	infoContent := "VERSION=2.75\n"
	if tracksJSON != "" {
		infoContent += fmt.Sprintf("SNAPD_LTS_TRACKS='%s'\n", tracksJSON)
	}
	return snaptest.MakeTestSnapWithFiles(c, `name: snapd
type: snapd
version: 2.75`, [][]string{
		{"usr/lib/snapd/info", infoContent},
	})
}

func snapdSnapsup(c *C, tracksJSON, channel string) *snapstate.SnapSetup {
	return snapdSnapsupFromBlob(c, makeSnapdBlobWithLTSTracks(c, tracksJSON), channel)
}

func snapdSnapsupFromBlob(c *C, blobSrc, channel string) *snapstate.SnapSetup {
	snapsup := &snapstate.SnapSetup{
		Type: snap.TypeSnapd,
		SideInfo: &snap.SideInfo{
			RealName: "snapd",
			SnapID:   snaptest.AssertedSnapID("snapd"),
			Revision: snap.R(100),
			Channel:  channel,
		},
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://store.example.com/snapd_100.snap",
		},
		Channel: channel,
	}
	dest := snapsup.BlobPath()
	c.Assert(os.MkdirAll(filepath.Dir(dest), 0755), IsNil)
	c.Assert(osutil.CopyFile(blobSrc, dest, osutil.CopyFlagOverwrite), IsNil)
	snapsup.SnapPath = dest
	return snapsup
}

func (s *ltsDownloadSuite) callRedirect(snapsup *snapstate.SnapSetup, model *asserts.Model) error {
	return s.callRedirectIgnoringChange(snapsup, model, "")
}

func (s *ltsDownloadSuite) callRedirectIgnoringChange(snapsup *snapstate.SnapSetup, model *asserts.Model, ignoreChangeID string) error {
	var deviceCtx snapstate.DeviceContext
	if model != nil {
		deviceCtx = &snapstatetest.TrivialDeviceContext{DeviceModel: model}
	}
	return s.callRedirectWithDeviceCtx(snapsup, deviceCtx, ignoreChangeID)
}

func (s *ltsDownloadSuite) callRedirectWithDeviceCtx(snapsup *snapstate.SnapSetup, deviceCtx snapstate.DeviceContext, ignoreChangeID string) error {
	return snapstate.MaybeRedirectSnapdToLTSTrack(
		context.Background(), s.state, snapsup, deviceCtx,
		s.fakeStore, nil,
		progress.Null,
		&store.DownloadOptions{LeavePartialOnError: true},
		timings.New(nil),
		ignoreChangeID,
	)
}

func (s *ltsDownloadSuite) installSnapd(c *C, version string) {
	si := &snap.SideInfo{
		RealName: "snapd",
		SnapID:   snaptest.AssertedSnapID("snapd"),
		Revision: snap.R(1),
	}
	// CurrentInfo uses the mocked ReadInfo, not yaml on disk.
	restore := snapstate.MockSnapReadInfo(func(name string, sideInfo *snap.SideInfo) (*snap.Info, error) {
		info, err := s.fakeBackend.ReadInfo(name, sideInfo)
		if err != nil {
			return nil, err
		}
		if name == "snapd" {
			info.Version = version
		}
		return info, nil
	})
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  snap.R(1),
		SnapType: "snapd",
	})
}

func (s *ltsDownloadSuite) mutateSnapdStoreVersion(version string) {
	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		if info.SnapName() == "snapd" {
			info.Version = version
		}
		return nil
	}
}

func (s *ltsDownloadSuite) snapdStoreAction(c *C) *store.SnapAction {
	for _, op := range s.fakeBackend.ops {
		if op.op == "storesvc-snap-action:action" && op.action.InstanceName == "snapd" {
			a := op.action
			return &a
		}
	}
	c.Fatalf("no snapd store action recorded")
	return nil
}

func (s *ltsDownloadSuite) TestRedirectSkipIsExplicitChannel(c *C) {
	snapsup := snapdSnapsup(c, `{"18":{"latest":"18"}}`, "latest/stable")
	snapsup.IsExplicitChannel = true
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipIsExplicitRevision(c *C) {
	snapsup := snapdSnapsup(c, `{"18":{"latest":"18"}}`, "latest/stable")
	snapsup.IsExplicitRevision = true
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectNilModelFails(c *C) {
	snapsup := snapdSnapsup(c, `{"18":{"latest":"18"}}`, "latest/stable")

	err := s.callRedirect(snapsup, nil)
	c.Assert(err, ErrorMatches, `cannot inspect snapd LTS tracks after download: no device model`)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipClassicModel(c *C) {
	snapsup := snapdSnapsup(c, `{"18":{"latest":"18"}}`, "latest/stable")
	// Prove we do not open the squashfs: a missing blob would fail inspect.
	snapsup.SnapPath = "/nonexistent/snapd.snap"
	model := ClassicModel()
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipUC16(c *C) {
	snapsup := snapdSnapsup(c, `{"18":{"latest":"18"}}`, "latest/stable")
	snapsup.SnapPath = "/nonexistent/snapd.snap"
	model := ModelWithBase("core16")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipOmittedTrackMap(c *C) {
	snapsup := snapdSnapsup(c, "", "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipEmptyTrackMap(c *C) {
	snapsup := snapdSnapsup(c, `{}`, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectRewritesSnapSetup(c *C) {
	snapsup := snapdSnapsup(c, `{"18":{"latest":"18"}}`, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	s.mutateSnapdStoreVersion("2.58")

	discardPath := snapsup.BlobPath()
	c.Assert(s.callRedirect(snapsup, model), IsNil)

	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapsup.SideInfo.Channel, Equals, "18/stable")
	c.Check(snapsup.SideInfo.Revision, Equals, snap.R(11))
	c.Check(snapsup.SnapPath, Equals, filepath.Join(dirs.SnapBlobDir, "snapd_11.snap"))
	c.Check(snapsup.Version, Equals, "2.58")
	c.Check(snapsup.ExpectedProvenance, Equals, "")
	c.Check(snapsup.Base, Equals, "")
	c.Check(snapsup.IntegrityDataInfo, IsNil)
	c.Assert(snapsup.DownloadInfo, NotNil)
	c.Check(snapsup.DownloadInfo.DownloadURL, Equals, "https://some-server.com/some/path.snap")
	// Discard is the caller's job after persisting snap-setup.
	c.Check(osutil.FileExists(discardPath), Equals, true)

	c.Assert(s.fakeBackend.ops, HasLen, 3)
	c.Check(s.fakeBackend.ops[0].op, Equals, "storesvc-snap-action")
	c.Check(s.fakeBackend.ops[1].op, Equals, "storesvc-snap-action:action")
	c.Check(s.fakeBackend.ops[1].action.Action, Equals, "install")
	c.Check(s.fakeBackend.ops[1].action.InstanceName, Equals, "snapd")
	c.Check(s.fakeBackend.ops[1].action.Channel, Equals, "18/stable")
	c.Check(s.fakeBackend.ops[1].action.CohortKey, Equals, "")
	c.Check(s.fakeBackend.ops[1].action.Revision.Unset(), Equals, true)
	c.Check(s.fakeBackend.ops[1].revno, Equals, snap.R(11))
	c.Check(s.fakeBackend.ops[2], DeepEquals, fakeOp{op: "storesvc-download", name: "snapd"})

	c.Assert(s.fakeStore.downloads, HasLen, 1)
	c.Check(s.fakeStore.downloads[0].name, Equals, "snapd")
	c.Check(s.fakeStore.downloads[0].target, Equals, filepath.Join(dirs.SnapBlobDir, "snapd_11.snap"))
}

func (s *ltsDownloadSuite) TestRedirectUsesDeviceCtxStore(c *C) {
	s.state.Lock()
	snapstate.ReplaceStore(s.state, ltsCachedStore{})
	s.state.Unlock()

	snapsup := snapdSnapsup(c, `{"18":{"latest":"18"}}`, "latest/stable")
	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: ModelWithBase("core18"),
		CtxStore:    s.fakeStore,
	}

	c.Assert(s.callRedirectWithDeviceCtx(snapsup, deviceCtx, ""), IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapsup.SideInfo.Revision, Equals, snap.R(11))
	c.Check(s.snapdStoreAction(c).Channel, Equals, "18/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipWhenExclusiveDowngradeConflict(c *C) {
	s.installSnapd(c, "2.75")
	s.mutateSnapdStoreVersion("2.58")

	s.state.Lock()
	other := s.state.NewChange("install-snap", "unrelated work")
	other.AddTask(s.state.NewTask("dummy", "dummy"))
	s.state.Unlock()

	snapsup := snapdSnapsup(c, `{"18":{"latest":"18"}}`, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(snapsup.Channel, Equals, "latest/stable")
	c.Check(snapsup.Version, Equals, "")
	c.Check(snapsup.SideInfo.Revision, Equals, snap.R(100))
	c.Check(s.fakeStore.downloads, HasLen, 0)
}

func (s *ltsDownloadSuite) TestRedirectUnreadableMapError(c *C) {
	snapsup := snapdSnapsup(c, `{bad`, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	err := s.callRedirect(snapsup, model)
	c.Assert(err, ErrorMatches, `cannot retrieve LTS track map from candidate snapd snap 2.75: cannot parse SNAPD_LTS_TRACKS:.*`)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestNeedsGateIsExplicitChannel(c *C) {
	snapsup := &snapstate.SnapSetup{
		Type:              snap.TypeSnapd,
		SideInfo:          &snap.SideInfo{SnapID: "some-id"},
		IsExplicitChannel: true,
	}
	c.Check(snapstate.NeedsSnapdLTSTrackResolve(snapsup), Equals, false)
}

func (s *ltsDownloadSuite) TestNeedsGateIsExplicitRevision(c *C) {
	snapsup := &snapstate.SnapSetup{
		Type:               snap.TypeSnapd,
		SideInfo:           &snap.SideInfo{SnapID: "some-id"},
		IsExplicitRevision: true,
	}
	c.Check(snapstate.NeedsSnapdLTSTrackResolve(snapsup), Equals, false)
}

func (s *ltsDownloadSuite) TestDoDownloadSnapRedirectsSnapdToLTSTrack(c *C) {
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	s.installSnapd(c, "2.75")
	s.mutateSnapdStoreVersion("2.58")

	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`)
	dest := filepath.Join(dirs.SnapBlobDir, "snapd_100.snap")
	ltsDest := filepath.Join(dirs.SnapBlobDir, "snapd_11.snap")
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), IsNil)
	s.fakeStore.downloadCallback = func() {
		c.Assert(osutil.CopyFile(blobPath, dest, osutil.CopyFlagOverwrite), IsNil)
	}

	s.state.Lock()
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Type:    snap.TypeSnapd,
		Channel: "latest/stable",
		SideInfo: &snap.SideInfo{
			RealName: "snapd",
			SnapID:   snaptest.AssertedSnapID("snapd"),
			Revision: snap.R(100),
			Channel:  "latest/stable",
		},
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://store.example.com/snapd_100.snap",
		},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), IsNil)
	c.Check(t.Status(), Equals, state.DoneStatus)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	var snapsup snapstate.SnapSetup
	c.Assert(t.Get("snap-setup", &snapsup), IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
	c.Check(snapsup.SideInfo.Channel, Equals, "18/stable")
	c.Check(snapsup.SideInfo.Revision, Equals, snap.R(11))
	c.Check(snapsup.SnapPath, Equals, ltsDest)
	c.Check(snapsup.Version, Equals, "2.58")

	c.Assert(s.fakeBackend.ops, HasLen, 4)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{op: "storesvc-download", name: "snapd"})
	c.Check(s.fakeBackend.ops[1].op, Equals, "storesvc-snap-action")
	c.Check(s.fakeBackend.ops[2].op, Equals, "storesvc-snap-action:action")
	c.Check(s.fakeBackend.ops[2].action.Action, Equals, "install")
	c.Check(s.fakeBackend.ops[2].action.InstanceName, Equals, "snapd")
	c.Check(s.fakeBackend.ops[2].action.Channel, Equals, "18/stable")
	c.Check(s.fakeBackend.ops[2].action.Revision.Unset(), Equals, true)
	c.Check(s.fakeBackend.ops[2].revno, Equals, snap.R(11))
	c.Check(s.fakeBackend.ops[3], DeepEquals, fakeOp{op: "storesvc-download", name: "snapd"})

	c.Assert(s.fakeStore.downloads, HasLen, 2)
	c.Check(s.fakeStore.downloads[0].name, Equals, "snapd")
	c.Check(s.fakeStore.downloads[0].target, Equals, dest)
	c.Check(s.fakeStore.downloads[0].revision(), Equals, snap.R(100))
	c.Check(s.fakeStore.downloads[1].name, Equals, "snapd")
	c.Check(s.fakeStore.downloads[1].target, Equals, ltsDest)
	c.Check(s.fakeStore.downloads[1].revision(), Equals, snap.R(11))
	c.Check(osutil.FileExists(dest), Equals, false)
}

func (s *ltsDownloadSuite) TestDoDownloadSnapDeviceCtxErrorFailsTask(c *C) {
	s.AddCleanup(snapstatetest.ReplaceDeviceCtxHook(func(st *state.State, task *state.Task, providedDeviceCtx snapstate.DeviceContext) (snapstate.DeviceContext, error) {
		if providedDeviceCtx != nil {
			return providedDeviceCtx, nil
		}
		return nil, fmt.Errorf("device context unavailable")
	}))

	s.state.Lock()
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Type:    snap.TypeSnapd,
		Channel: "latest/stable",
		SideInfo: &snap.SideInfo{
			RealName: "snapd",
			SnapID:   snaptest.AssertedSnapID("snapd"),
			Revision: snap.R(100),
			Channel:  "latest/stable",
		},
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "https://store.example.com/snapd_100.snap",
		},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*device context unavailable.*`)
	c.Check(t.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(s.fakeStore.downloads, HasLen, 0)
}
