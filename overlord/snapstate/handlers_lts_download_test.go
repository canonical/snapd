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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/ltschannel"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/timings"
)

type ltsDownloadSuite struct {
	baseHandlerSuite

	fakeStore *fakeStore
}

var _ = Suite(&ltsDownloadSuite{})

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
}

// makeSnapdBlobWithLTSTracks builds a minimal snapd squashfs at a temp path
// whose /usr/lib/snapd/info contains the given SNAPD_LTS_TRACKS JSON value
// and optional SNAPD_PATCH_LEVEL. An empty tracksJSON omits that key.
// A zero patchLevel omits SNAPD_PATCH_LEVEL.
func makeSnapdBlobWithLTSTracks(c *C, tracksJSON string, patchLevel int) string {
	infoContent := "VERSION=2.75\n"
	if tracksJSON != "" {
		infoContent += fmt.Sprintf("SNAPD_LTS_TRACKS='%s'\n", tracksJSON)
	}
	if patchLevel != 0 {
		infoContent += fmt.Sprintf("SNAPD_PATCH_LEVEL=%d\n", patchLevel)
	}
	return snaptest.MakeTestSnapWithFiles(c, `name: snapd
type: snapd
version: 2.75`, [][]string{
		{"usr/lib/snapd/info", infoContent},
	})
}

// snapdSnapsup returns a SnapSetup for a snapd store download targeting
// the given channel, with the blob path set to blobPath.
func snapdSnapsup(blobPath, channel string) *snapstate.SnapSetup {
	return &snapstate.SnapSetup{
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
		Channel:  channel,
		SnapPath: blobPath,
	}
}

// callRedirect is a thin shim to keep test bodies concise.
func (s *ltsDownloadSuite) callRedirect(snapsup *snapstate.SnapSetup, model *asserts.Model) error {
	return snapstate.MaybeRedirectSnapdToLTSChannel(
		context.Background(), s.state, snapsup, model,
		s.fakeStore, nil,
		progress.Null,
		&store.DownloadOptions{LeavePartialOnError: true},
		timings.New(nil),
	)
}

// ---- Gate tests (no redirect expected) --------------------------------

func (s *ltsDownloadSuite) TestRedirectSkipNonSnapd(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	snapsup.Type = snap.TypeApp // not snapd
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipUnasserted(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	snapsup.SideInfo.SnapID = "" // unasserted
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipHasPrereqs(c *C) {
	// If snapd ever gains prerequisites the redirect must be skipped: the
	// prerequisites task already ran against the planned revision's metadata
	// and may not match the LTS-target's requirements.
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	snapsup.Prereq = []string{"some-content-provider"}
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipHasPrereqContentAttrs(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	snapsup.PrereqContentAttrs = map[string][]string{"some-provider": {"some-content"}}
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipNilModel(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")

	c.Assert(s.callRedirect(snapsup, nil), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipClassicModel(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ClassicModel()
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipUnmanagedBase(c *C) {
	// UC20 base with no LTS map entry → LTSNoTrackError → pass through
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core20")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipAlreadyOnLTSChannel(c *C) {
	// Already on the LTS channel → no remap needed
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "18/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "18/stable")
}

func (s *ltsDownloadSuite) TestRedirectSkipMissingInfoFile(c *C) {
	// Squashfs has no /usr/lib/snapd/info → error from inspectSnapdLTSAfterDownload
	// → log + pass through (v1 missing-map policy)
	blobPath := snaptest.MakeTestSnapWithFiles(c, `name: snapd
type: snapd
version: 2.75`, nil)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	// snap-setup channel unchanged (pass-through)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

// ---- Redirect success path --------------------------------------------

func (s *ltsDownloadSuite) TestRedirectRewritesSnapSetup(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)

	// snap-setup Channel updated to LTS channel
	c.Check(snapsup.Channel, Equals, "18/stable")
	// SideInfo.Channel updated
	c.Check(snapsup.SideInfo.Channel, Equals, "18/stable")
	// DownloadInfo updated (URL from fakeStore)
	c.Assert(snapsup.DownloadInfo, NotNil)
	c.Check(snapsup.DownloadInfo.DownloadURL, Equals, "https://some-server.com/some/path.snap")

	// One store action call (LTS channel install) + one download (re-download)
	c.Assert(s.fakeBackend.ops, HasLen, 3)
	c.Check(s.fakeBackend.ops[0], DeepEquals, fakeOp{op: "storesvc-snap-action"})
	c.Check(s.fakeBackend.ops[1].op, Equals, "storesvc-snap-action:action")
	c.Check(s.fakeBackend.ops[1].action.Action, Equals, "install")
	c.Check(s.fakeBackend.ops[1].action.InstanceName, Equals, "snapd")
	c.Check(s.fakeBackend.ops[1].action.Channel, Equals, "18/stable")
	c.Check(s.fakeBackend.ops[2], DeepEquals, fakeOp{op: "storesvc-download", name: "snapd"})

	// One re-download to the same blob path
	c.Assert(s.fakeStore.downloads, HasLen, 1)
	c.Check(s.fakeStore.downloads[0].name, Equals, "snapd")
	c.Check(s.fakeStore.downloads[0].target, Equals, blobPath)
}

func (s *ltsDownloadSuite) TestRedirectRiskPreserved(c *C) {
	// latest/edge → 18/edge (risk preserved)
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/edge")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(snapsup.Channel, Equals, "18/edge")
	c.Check(s.fakeBackend.ops[1].action.Channel, Equals, "18/edge")
}

func (s *ltsDownloadSuite) TestRedirectPassesValidationSets(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	var capturedVsets *snapasserts.ValidationSets
	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		capturedVsets = snapasserts.NewValidationSets()
		return capturedVsets, nil
	})
	s.AddCleanup(restore)

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	// validation sets were fetched (even if empty)
	c.Check(capturedVsets, NotNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
}

// ---- Redirect failure paths -------------------------------------------

func (s *ltsDownloadSuite) TestRedirectStoreActionError(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	// Simulate the store returning an error for the LTS install action.
	// We do this by marking "snapd" as unknown in the fakeStore.
	s.fakeStore.fakeBackend.ops = nil
	s.fakeStore.mutateSnapInfo = func(info *snap.Info) error {
		if info.SnapName() == "snapd" {
			return fmt.Errorf("store is down")
		}
		return nil
	}

	err := s.callRedirect(snapsup, model)
	c.Assert(err, ErrorMatches, `cannot resolve snapd LTS redirect to channel "18/stable": .*`)
	// snap-setup must NOT have been rewritten on error
	c.Check(snapsup.Channel, Equals, "latest/stable")
	c.Check(s.fakeStore.downloads, HasLen, 0)
}

func (s *ltsDownloadSuite) TestRedirectDownloadError(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	s.fakeStore.downloadError = map[string]error{
		"snapd": fmt.Errorf("network flap"),
	}

	err := s.callRedirect(snapsup, model)
	c.Assert(err, ErrorMatches, `cannot download snapd LTS target on channel "18/stable": .*`)
	// snap-setup must NOT have been rewritten on download error
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestRedirectValidationSetsError(c *C) {
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return nil, fmt.Errorf("validation sets db unavailable")
	})
	s.AddCleanup(restore)

	err := s.callRedirect(snapsup, model)
	c.Assert(err, ErrorMatches, `cannot get validation sets for snapd LTS redirect: .*`)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

// ---- NeedsSnapdLTSChannelResolve gate (unit) -------------------------

func (s *ltsDownloadSuite) TestNeedsGateSnapdType(c *C) {
	model := ModelWithBase("core18")
	snapsup := &snapstate.SnapSetup{
		Type:     snap.TypeSnapd,
		SideInfo: &snap.SideInfo{SnapID: "some-id"},
	}
	c.Check(snapstate.NeedsSnapdLTSChannelResolve(snapsup, model), Equals, true)
}

func (s *ltsDownloadSuite) TestNeedsGateNonSnapd(c *C) {
	model := ModelWithBase("core18")
	snapsup := &snapstate.SnapSetup{
		Type:     snap.TypeApp,
		SideInfo: &snap.SideInfo{SnapID: "some-id"},
	}
	c.Check(snapstate.NeedsSnapdLTSChannelResolve(snapsup, model), Equals, false)
}

func (s *ltsDownloadSuite) TestNeedsGateNoSnapID(c *C) {
	model := ModelWithBase("core18")
	snapsup := &snapstate.SnapSetup{
		Type:     snap.TypeSnapd,
		SideInfo: &snap.SideInfo{SnapID: ""},
	}
	c.Check(snapstate.NeedsSnapdLTSChannelResolve(snapsup, model), Equals, false)
}

func (s *ltsDownloadSuite) TestNeedsGateNilModel(c *C) {
	snapsup := &snapstate.SnapSetup{
		Type:     snap.TypeSnapd,
		SideInfo: &snap.SideInfo{SnapID: "some-id"},
	}
	c.Check(snapstate.NeedsSnapdLTSChannelResolve(snapsup, nil), Equals, false)
}

func (s *ltsDownloadSuite) TestNeedsGateHasPrereq(c *C) {
	model := ModelWithBase("core18")
	snapsup := &snapstate.SnapSetup{
		Type:     snap.TypeSnapd,
		SideInfo: &snap.SideInfo{SnapID: "some-id"},
		Prereq:   []string{"some-provider"},
	}
	c.Check(snapstate.NeedsSnapdLTSChannelResolve(snapsup, model), Equals, false)
}

func (s *ltsDownloadSuite) TestNeedsGateHasPrereqContentAttrs(c *C) {
	model := ModelWithBase("core18")
	snapsup := &snapstate.SnapSetup{
		Type:               snap.TypeSnapd,
		SideInfo:           &snap.SideInfo{SnapID: "some-id"},
		PrereqContentAttrs: map[string][]string{"some-provider": {"some-content"}},
	}
	c.Check(snapstate.NeedsSnapdLTSChannelResolve(snapsup, model), Equals, false)
}

// ---- LTS not-allowed errors are pass-through -------------------------

func (s *ltsDownloadSuite) TestRedirectLTSNotAllowedIsPassThrough(c *C) {
	// LTSNotAllowedError (classic model, UC16, etc.) should be treated as
	// "pass through" in the download path, not as a hard error.
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")

	// Restrict UC scope so any UC model produces LTSNotAllowedError.
	restore := ltschannel.MockSnapdLTSDeviceKindScope(false, false, false)
	s.AddCleanup(restore)

	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	// LTSNotAllowedError surfaces from inspectSnapdLTSAfterDownload and must
	// cause pass-through (log + continue), not a task failure.
	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(s.fakeBackend.ops, HasLen, 0)
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

// ---- Patch level pre-check -------------------------------------------

func (s *ltsDownloadSuite) TestPatchLevelCompatible(c *C) {
	// State patch level 6, target patch level 6 → compatible, redirect proceeds.
	s.state.Lock()
	s.state.Set("patch-level", 6)
	s.state.Unlock()

	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 6)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
}

func (s *ltsDownloadSuite) TestPatchLevelTargetNewer(c *C) {
	// State patch level 5, target patch level 6 → target is newer, redirect proceeds.
	s.state.Lock()
	s.state.Set("patch-level", 5)
	s.state.Unlock()

	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 6)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
}

func (s *ltsDownloadSuite) TestPatchLevelIncompatible(c *C) {
	// State patch level 7, target patch level 6 → target would be refused at
	// daemon start; redirect must be rejected here.
	s.state.Lock()
	s.state.Set("patch-level", 7)
	s.state.Unlock()

	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 6)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	err := s.callRedirect(snapsup, model)
	c.Assert(err, ErrorMatches, `cannot redirect snapd to LTS channel "18/stable": target version 2.75 patch level 6 is incompatible with current state patch level 7`)
	// snap-setup must not have been rewritten.
	c.Check(snapsup.Channel, Equals, "latest/stable")
}

func (s *ltsDownloadSuite) TestPatchLevelAbsentInBlob(c *C) {
	// Blob carries no SNAPD_PATCH_LEVEL key (older snap) → check skipped,
	// redirect proceeds normally.
	s.state.Lock()
	s.state.Set("patch-level", 99)
	s.state.Unlock()

	// patchLevel=0 → key omitted from info file
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 0)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
}

func (s *ltsDownloadSuite) TestPatchLevelNoPatchLevelInState(c *C) {
	// No patch-level in state (fresh device) → check skipped, redirect proceeds.
	blobPath := makeSnapdBlobWithLTSTracks(c, `{"18":{"latest":"18","18":"18"}}`, 6)
	snapsup := snapdSnapsup(blobPath, "latest/stable")
	model := ModelWithBase("core18")
	s.AddCleanup(snapstatetest.MockDeviceModel(model))

	c.Assert(s.callRedirect(snapsup, model), IsNil)
	c.Check(snapsup.Channel, Equals, "18/stable")
}

// ---- CheckSnapdLTSTargetPatchLevel unit tests ------------------------

func (s *ltsDownloadSuite) TestCheckPatchLevelDirectCompatible(c *C) {
	s.state.Lock()
	s.state.Set("patch-level", 6)
	s.state.Unlock()

	blobPath := makeSnapdBlobWithLTSTracks(c, "", 6)
	c.Assert(snapstate.CheckSnapdLTSTargetPatchLevel(s.state, blobPath, "18/stable"), IsNil)
}

func (s *ltsDownloadSuite) TestCheckPatchLevelDirectIncompatible(c *C) {
	s.state.Lock()
	s.state.Set("patch-level", 7)
	s.state.Unlock()

	blobPath := makeSnapdBlobWithLTSTracks(c, "", 6)
	err := snapstate.CheckSnapdLTSTargetPatchLevel(s.state, blobPath, "18/stable")
	c.Assert(err, ErrorMatches, `cannot redirect snapd to LTS channel "18/stable": .*`)
}

func (s *ltsDownloadSuite) TestCheckPatchLevelDirectAbsent(c *C) {
	s.state.Lock()
	s.state.Set("patch-level", 99)
	s.state.Unlock()

	blobPath := makeSnapdBlobWithLTSTracks(c, "", 0)
	c.Assert(snapstate.CheckSnapdLTSTargetPatchLevel(s.state, blobPath, "18/stable"), IsNil)
}
