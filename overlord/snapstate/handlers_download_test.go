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
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type downloadSnapSuite struct {
	baseHandlerSuite

	fakeStore *fakeStore
}

var _ = Suite(&downloadSnapSuite{})

func (s *downloadSnapSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	s.fakeStore = &fakeStore{
		state:       s.state,
		fakeBackend: s.fakeBackend,
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

func (s *downloadSnapSuite) TestDoDownloadSnapCompatibility(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockStoreDownloadIcon(func(ctx context.Context, name, targetPath, downloadURL string) error {
		c.Fatal("should not have called DownloadIcon when no icon URL was present")
		return nil
	})
	defer restore()

	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
		Channel: "some-channel",
		// explicitly set to "nil", this ensures the compatibility
		// code path in the task is hit and the store is queried
		// in the task (instead of using the new
		// SnapSetup.{SideInfo,DownloadInfo} that gets set in
		// snapstate.{Install,Update} directly.
		DownloadInfo: nil,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	// the compat code called the store "Snap" endpoint
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "foo",
				Channel:      "some-channel",
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(11),
		Channel:  "some-channel",
	})
	c.Check(t.Status(), Equals, state.DoneStatus)

	// check that a debug log was recorded because there no icon URL
	c.Check(logbuf.String(), testutil.Contains, fmt.Sprintf("cannot download snap icon for %q: no icon URL", "foo"))
}

func (s *downloadSnapSuite) TestDoDownloadSnapCompatibilityWithIcon(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapID := "foo-id"
	iconURL := "my-icon-url"

	downloadIconCount := 0
	restore := snapstate.MockStoreDownloadIcon(func(ctx context.Context, name, targetPath, downloadURL string) error {
		c.Check(name, Equals, "foo")
		expectedPath := snapstate.IconDownloadFilename(snapID)
		c.Check(targetPath, Equals, expectedPath)
		c.Check(downloadURL, Equals, iconURL)

		downloadIconCount++
		return nil
	})
	defer restore()

	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	mediaInfos := snap.MediaInfos{
		{
			Type: "icon",
			URL:  iconURL,
		},
	}
	initialSnapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
		Channel: "some-channel",
		// explicitly set to "nil", this ensures the compatibility
		// code path in the task is hit and the store is queried
		// in the task (instead of using the new
		// SnapSetup.{SideInfo,DownloadInfo} that gets set in
		// snapstate.{Install,Update} directly.
		DownloadInfo: nil,
	}
	initialSnapsup.Media = mediaInfos

	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", initialSnapsup)
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	// the compat code called the store "Snap" endpoint
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "foo",
				Channel:      "some-channel",
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(11),
		Channel:  "some-channel",
	})
	c.Check(t.Status(), Equals, state.DoneStatus)

	c.Check(downloadIconCount, Equals, 1)
	c.Check(logbuf.String(), Not(testutil.Contains), "cannot download snap icon")
}

func (s *downloadSnapSuite) TestDoDownloadSnapCompatibilityWithIconErrors(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapID := "foo-id"
	iconURL := "my-icon-url"

	downloadIconCount := 0
	errorMsg := "fake error"
	restore := snapstate.MockStoreDownloadIcon(func(ctx context.Context, name, targetPath, downloadURL string) error {
		c.Check(name, Equals, "foo")
		expectedPath := snapstate.IconDownloadFilename(snapID)
		c.Check(targetPath, Equals, expectedPath)
		c.Check(downloadURL, Equals, iconURL)

		downloadIconCount++
		return errors.New(errorMsg)
	})
	defer restore()

	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	mediaInfos := snap.MediaInfos{
		{
			Type: "icon",
			URL:  iconURL,
		},
	}
	initialSnapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
		Channel: "some-channel",
		// explicitly set to "nil", this ensures the compatibility
		// code path in the task is hit and the store is queried
		// in the task (instead of using the new
		// SnapSetup.{SideInfo,DownloadInfo} that gets set in
		// snapstate.{Install,Update} directly.
		DownloadInfo: nil,
	}
	initialSnapsup.Media = mediaInfos

	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", initialSnapsup)
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%v", chg.Err()))

	// the compat code called the store "Snap" endpoint
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:       "install",
				InstanceName: "foo",
				Channel:      "some-channel",
			},
			revno: snap.R(11),
		},
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(11),
		Channel:  "some-channel",
	})
	c.Check(t.Status(), Equals, state.DoneStatus)

	c.Check(downloadIconCount, Equals, 1)
	c.Check(logbuf.String(), testutil.Contains, `cannot download snap icon for "foo":`)
	c.Check(logbuf.String(), testutil.Contains, errorMsg)
}

func (s *downloadSnapSuite) TestDoDownloadSnapCompatibilityValidationSets(c *C) {
	s.state.Lock()

	headers := map[string]interface{}{
		"type":         "validation-set",
		"timestamp":    time.Now().Format(time.RFC3339),
		"authority-id": "foo",
		"series":       "16",
		"account-id":   "foo",
		"name":         "bar",
		"sequence":     "3",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "foo",
				"id":       snaptest.AssertedSnapID("foo"),
				"presence": "required",
				"revision": "15",
			},
		},
	}

	signing := assertstest.NewStoreStack("can0nical", nil)
	a, err := signing.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)
	vs := a.(*asserts.ValidationSet)

	vsets := snapasserts.NewValidationSets()
	err = vsets.Add(vs)
	c.Assert(err, IsNil)
	c.Assert(vsets.Conflict(), IsNil)

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return vsets, nil
	})
	s.AddCleanup(restore)

	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
		Channel: "some-channel",
		// explicitly set to "nil", this ensures the compatibility
		// code path in the task is hit and the store is queried
		// in the task (instead of using the new
		// SnapSetup.{SideInfo,DownloadInfo} that gets set in
		// snapstate.{Install,Update} directly.
		DownloadInfo: nil,
	})
	s.state.NewChange("sample", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	// the compat code called the store "Snap" endpoint
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op: "storesvc-snap-action",
		},
		{
			op: "storesvc-snap-action:action",
			action: store.SnapAction{
				Action:         "install",
				InstanceName:   "foo",
				Revision:       snap.R(15),
				ValidationSets: []snapasserts.ValidationSetKey{"16/foo/bar/3"},
				// channel is explicitly empty, since we're sending a revision
				// from the validation sets
				Channel: "",
			},
			revno: snap.R(15),
		},
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})

	s.state.Lock()
	defer s.state.Unlock()

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(15),
	})
	c.Check(t.Status(), Equals, state.DoneStatus)
}

func (s *downloadSnapSuite) TestDoDownloadSnapCompatibilityValidationSetsInvalid(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	headers := map[string]interface{}{
		"type":         "validation-set",
		"timestamp":    time.Now().Format(time.RFC3339),
		"authority-id": "foo",
		"series":       "16",
		"account-id":   "foo",
		"name":         "bar",
		"sequence":     "3",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "foo",
				"id":       snaptest.AssertedSnapID("foo"),
				"presence": "invalid",
			},
		},
	}

	signing := assertstest.NewStoreStack("can0nical", nil)
	a, err := signing.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)
	vs := a.(*asserts.ValidationSet)

	vsets := snapasserts.NewValidationSets()
	err = vsets.Add(vs)
	c.Assert(err, IsNil)
	c.Assert(vsets.Conflict(), IsNil)

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return vsets, nil
	})
	s.AddCleanup(restore)

	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
		},
		Channel: "some-channel",
		// explicitly set to "nil", this ensures the compatibility
		// code path in the task is hit and the store is queried
		// in the task (instead of using the new
		// SnapSetup.{SideInfo,DownloadInfo} that gets set in
		// snapstate.{Install,Update} directly.
		DownloadInfo: nil,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot install snap "foo" due to enforcing rules of validation set 16/foo/bar/3.*`)
}

func (s *downloadSnapSuite) TestDoDownloadSnapCompatibilityValidationSetsWrongRevision(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	headers := map[string]interface{}{
		"type":         "validation-set",
		"timestamp":    time.Now().Format(time.RFC3339),
		"authority-id": "foo",
		"series":       "16",
		"account-id":   "foo",
		"name":         "bar",
		"sequence":     "3",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "foo",
				"id":       snaptest.AssertedSnapID("foo"),
				"presence": "required",
				"revision": "15",
			},
		},
	}

	signing := assertstest.NewStoreStack("can0nical", nil)
	a, err := signing.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)
	vs := a.(*asserts.ValidationSet)

	vsets := snapasserts.NewValidationSets()
	err = vsets.Add(vs)
	c.Assert(err, IsNil)
	c.Assert(vsets.Conflict(), IsNil)

	restore := snapstate.MockEnforcedValidationSets(func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return vsets, nil
	})
	s.AddCleanup(restore)

	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(14),
		},
		Channel: "some-channel",
		// explicitly set to "nil", this ensures the compatibility
		// code path in the task is hit and the store is queried
		// in the task (instead of using the new
		// SnapSetup.{SideInfo,DownloadInfo} that gets set in
		// snapstate.{Install,Update} directly.
		DownloadInfo: nil,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()
	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot install snap "foo" at revision 14 without --ignore-validation, revision 15 is required by validation sets: 16/foo/bar/3.*`)
}

func (s *downloadSnapSuite) TestDoDownloadSnapNormal(c *C) {
	s.state.Lock()

	restore := snapstate.MockStoreDownloadIcon(func(ctx context.Context, name, targetPath, downloadURL string) error {
		c.Fatal("should not have called DownloadIcon when no icon URL was present")
		return nil
	})
	defer restore()

	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   "mySnapID",
		Revision: snap.R(11),
		Channel:  "my-channel",
	}

	// download, ensure the store does not query
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Channel:  "some-channel",
		SideInfo: si,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://some-url.com/snap",
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

	// only the download endpoint of the store was hit
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, si)
	c.Check(t.Status(), Equals, state.DoneStatus)

	// check no IsAutoRefresh was passed in
	c.Assert(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{
			name:   "foo",
			target: filepath.Join(dirs.SnapBlobDir, "foo_11.snap"),
			opts:   nil,
		},
	})

	// check that a debug log was recorded because there no icon URL
	c.Check(logbuf.String(), testutil.Contains, fmt.Sprintf("cannot download snap icon for %q: no icon URL", "foo"))
}

func (s *downloadSnapSuite) TestDoDownloadSnapWithIcon(c *C) {
	s.state.Lock()

	snapName := "foo"
	snapID := "mySnapID"
	iconURL := "my-icon-url"

	downloadIconCount := 0
	restore := snapstate.MockStoreDownloadIcon(func(ctx context.Context, name, targetPath, downloadURL string) error {
		c.Check(name, Equals, snapName)
		expectedPath := snapstate.IconDownloadFilename(snapID)
		c.Check(targetPath, Equals, expectedPath)
		c.Check(downloadURL, Equals, iconURL)

		downloadIconCount++
		return nil
	})
	defer restore()

	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(11),
		Channel:  "my-channel",
	}

	mediaInfos := snap.MediaInfos{
		{
			Type: "icon",
			URL:  iconURL,
		},
	}
	initialSnapsup := &snapstate.SnapSetup{
		Channel:  "some-channel",
		SideInfo: si,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://some-url.com/snap",
		},
	}
	initialSnapsup.Media = mediaInfos

	// download
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", initialSnapsup)
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), IsNil)

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, si)
	c.Check(t.Status(), Equals, state.DoneStatus)

	c.Check(downloadIconCount, Equals, 1)
	c.Check(logbuf.String(), Not(testutil.Contains), "cannot download snap icon")
}

func (s *downloadSnapSuite) TestDoDownloadSnapWithIconErrors(c *C) {
	s.state.Lock()

	snapName := "foo"
	snapID := "mySnapID"
	iconURL := "my-icon-url"

	downloadIconCount := 0
	errorMsg := "fake error"
	restore := snapstate.MockStoreDownloadIcon(func(ctx context.Context, name, targetPath, downloadURL string) error {
		c.Check(name, Equals, snapName)
		expectedPath := snapstate.IconDownloadFilename(snapID)
		c.Check(targetPath, Equals, expectedPath)
		c.Check(downloadURL, Equals, iconURL)

		downloadIconCount++
		return errors.New(errorMsg)
	})
	defer restore()

	logbuf, restore := logger.MockDebugLogger()
	defer restore()

	si := &snap.SideInfo{
		RealName: snapName,
		SnapID:   snapID,
		Revision: snap.R(11),
		Channel:  "my-channel",
	}

	mediaInfos := snap.MediaInfos{
		{
			Type: "icon",
			URL:  iconURL,
		},
	}
	initialSnapsup := &snapstate.SnapSetup{
		Channel:  "some-channel",
		SideInfo: si,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://some-url.com/snap",
		},
	}
	initialSnapsup.Media = mediaInfos

	// download
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", initialSnapsup)
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), IsNil)

	var snapsup snapstate.SnapSetup
	t.Get("snap-setup", &snapsup)
	c.Check(snapsup.SideInfo, DeepEquals, si)
	c.Check(t.Status(), Equals, state.DoneStatus)

	c.Check(downloadIconCount, Equals, 1)
	c.Check(logbuf.String(), testutil.Contains, fmt.Sprintf("cannot download snap icon for %q:", snapName))
	c.Check(logbuf.String(), testutil.Contains, errorMsg)
}

func (s *downloadSnapSuite) TestDoDownloadSnapWithDeviceContext(c *C) {
	s.state.Lock()

	// unset the global store, it will need to come via the device context
	// CtxStore
	snapstate.ReplaceStore(s.state, nil)

	r := snapstatetest.MockDeviceContext(&snapstatetest.TrivialDeviceContext{
		CtxStore: s.fakeStore,
	})
	defer r()

	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   "mySnapID",
		Revision: snap.R(11),
		Channel:  "my-channel",
	}

	// download, ensure the store does not query
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		Channel:  "some-channel",
		SideInfo: si,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://some-url.com/snap",
		},
	})
	s.state.NewChange("sample", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	// only the download endpoint of the store was hit
	c.Assert(s.fakeBackend.ops, DeepEquals, fakeOps{
		{
			op:   "storesvc-download",
			name: "foo",
		},
	})
}

func (s *downloadSnapSuite) TestDoUndoDownloadSnap(c *C) {
	s.state.Lock()
	si := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(33),
	}
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://something.com/snap",
		},
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(t)
	chg.AddTask(terr)

	s.state.Unlock()

	for i := 0; i < 3; i++ {
		s.se.Ensure()
		s.se.Wait()
	}

	s.state.Lock()
	defer s.state.Unlock()

	// task was undone
	c.Check(t.Status(), Equals, state.UndoneStatus)

	// and nothing is in the state for "foo"
	var snapst snapstate.SnapState
	err := snapstate.Get(s.state, "foo", &snapst)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

}

func (s *downloadSnapSuite) TestDoDownloadRateLimitedIntegration(c *C) {
	s.state.Lock()

	// set auto-refresh rate-limit
	tr := config.NewTransaction(s.state)
	tr.Set("core", "refresh.rate-limit", "1234B")
	tr.Commit()

	// setup fake auto-refresh download
	si := &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
		Revision: snap.R(11),
	}
	t := s.state.NewTask("download-snap", "test")
	t.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: si,
		DownloadInfo: &snap.DownloadInfo{
			DownloadURL: "http://some-url.com/snap",
		},
		Flags: snapstate.Flags{
			IsAutoRefresh: true,
		},
	})
	s.state.NewChange("sample", "...").AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	// ensure that rate limit was honored
	c.Assert(s.fakeStore.downloads, DeepEquals, []fakeDownload{
		{
			name:   "foo",
			target: filepath.Join(dirs.SnapBlobDir, "foo_11.snap"),
			opts: &store.DownloadOptions{
				RateLimit: 1234,
				Scheduled: true,
			},
		},
	})

}
