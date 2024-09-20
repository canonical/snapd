// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type handlersSuite struct {
	baseHandlerSuite
}

var _ = Suite(&handlersSuite{})

func (s *handlersSuite) SetUpTest(c *C) {
	s.baseHandlerSuite.SetUpTest(c)

	s.AddCleanup(snapstatetest.MockDeviceModel(DefaultModel()))
	s.AddCleanup(osutil.MockMountInfo(""))
}

func (s *handlersSuite) TestSetTaskSnapSetupFirstTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// make a new task which will be the snap-setup-task for other tasks and
	// write a SnapSetup to it
	t := s.state.NewTask("link-snap", "test")
	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
			SnapID:   "foo-id",
		},
		Channel: "beta",
		UserID:  2,
	}
	t.Set("snap-setup", snapsup)
	s.state.NewChange("sample", "...").AddTask(t)

	// mutate it and rewrite it with the helper
	snapsup.Channel = "edge"
	err := snapstate.SetTaskSnapSetup(t, snapsup)
	c.Assert(err, IsNil)

	// get a fresh version of this task from state to check that the task's
	/// snap-setup has the changes in it now
	newT := s.state.Task(t.ID())
	c.Assert(newT, Not(IsNil))
	var newsnapsup snapstate.SnapSetup
	err = newT.Get("snap-setup", &newsnapsup)
	c.Assert(err, IsNil)
	c.Assert(newsnapsup.Channel, Equals, snapsup.Channel)
}

func (s *handlersSuite) TestSetTaskSnapSetupLaterTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	t := s.state.NewTask("link-snap", "test")

	snapsup := &snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			Revision: snap.R(33),
			SnapID:   "foo-id",
		},
		Channel: "beta",
		UserID:  2,
	}
	// setup snap-setup for the first task
	t.Set("snap-setup", snapsup)

	// make a new task and reference the first one in snap-setup-task
	t2 := s.state.NewTask("next-task-snap", "test2")
	t2.Set("snap-setup-task", t.ID())

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)
	chg.AddTask(t2)

	// mutate it and rewrite it with the helper
	snapsup.Channel = "edge"
	err := snapstate.SetTaskSnapSetup(t2, snapsup)
	c.Assert(err, IsNil)

	// check that the original task's snap-setup is different now
	newT := s.state.Task(t.ID())
	c.Assert(newT, Not(IsNil))
	var newsnapsup snapstate.SnapSetup
	err = newT.Get("snap-setup", &newsnapsup)
	c.Assert(err, IsNil)
	c.Assert(newsnapsup.Channel, Equals, snapsup.Channel)
}

func (s *handlersSuite) TestComputeMissingDisabledServices(c *C) {
	for _, tt := range []struct {
		// inputs
		stDisabledSvcsList []string
		apps               map[string]*snap.AppInfo
		// outputs
		missing []string
		found   []string
		err     error
		comment string
	}{
		// no apps
		{
			[]string{},
			nil,
			[]string{},
			[]string{},
			nil,
			"no apps",
		},
		// only apps, no services
		{
			[]string{},
			map[string]*snap.AppInfo{
				"app": {
					Daemon: "",
				},
			},
			[]string{},
			[]string{},
			nil,
			"no services",
		},
		// services in snap, but not disabled
		{
			[]string{},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon: "simple",
				},
			},
			[]string{},
			[]string{},
			nil,
			"no disabled services",
		},
		// all disabled services, but not present in snap
		{
			[]string{"svc1"},
			nil,
			[]string{"svc1"},
			[]string{},
			nil,
			"all missing disabled services",
		},
		// all disabled services, and found in snap
		{
			[]string{"svc1"},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon: "simple",
				},
			},
			[]string{},
			[]string{"svc1"},
			nil,
			"all found disabled services",
		},
		// some disabled services, some missing, some present in snap
		{
			[]string{"svc1", "svc2"},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon: "simple",
				},
			},
			[]string{"svc2"},
			[]string{"svc1"},
			nil,
			"some missing, some found disabled services",
		},
		// some disabled services, but app is not service
		{
			[]string{"svc1"},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon: "",
				},
			},
			[]string{"svc1"},
			[]string{},
			nil,
			"some disabled services that are now apps",
		},
	} {
		info := &snap.Info{Apps: tt.apps}

		overview, err := snapstate.MissingDisabledServices(tt.stDisabledSvcsList, nil, info)
		c.Assert(overview.MissingSystemServices, DeepEquals, tt.missing, Commentf(tt.comment))
		c.Assert(overview.FoundSystemServices, DeepEquals, tt.found, Commentf(tt.comment))
		c.Assert(overview.MissingUserServices, HasLen, 0)
		c.Assert(overview.FoundUserServices, HasLen, 0)
		c.Assert(err, Equals, tt.err, Commentf(tt.comment))
	}
}

func (s *handlersSuite) TestComputeMissingDisabledUserServices(c *C) {
	// Just like TestComputeMissingDisabledServices, but more focused on
	// the user services part
	for _, tt := range []struct {
		// inputs
		stDisabledSvcsList map[int][]string
		apps               map[string]*snap.AppInfo
		// outputs
		missing map[int][]string
		found   map[int][]string
		err     error
		comment string
	}{
		// no apps
		{
			map[int][]string{},
			nil,
			map[int][]string{},
			map[int][]string{},
			nil,
			"no apps",
		},
		// only apps, no services
		{
			map[int][]string{},
			map[string]*snap.AppInfo{
				"app": {
					Daemon:      "",
					DaemonScope: snap.UserDaemon,
				},
			},
			map[int][]string{},
			map[int][]string{},
			nil,
			"no services",
		},
		// services in snap, but not disabled
		{
			map[int][]string{},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon:      "simple",
					DaemonScope: snap.UserDaemon,
				},
			},
			map[int][]string{},
			map[int][]string{},
			nil,
			"no disabled services",
		},
		// all disabled services, but not present in snap
		{
			map[int][]string{
				1337: {"svc1"},
			},
			nil,
			map[int][]string{
				1337: {"svc1"},
			},
			map[int][]string{
				1337: {},
			},
			nil,
			"all missing disabled services",
		},
		// all disabled services, and found in snap
		{
			map[int][]string{
				1337: {"svc1"},
			},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon:      "simple",
					DaemonScope: snap.UserDaemon,
				},
			},
			map[int][]string{
				1337: {},
			},
			map[int][]string{
				1337: {"svc1"},
			},
			nil,
			"all found disabled services",
		},
		// some disabled services, some missing, some present in snap
		{
			map[int][]string{
				1337: {"svc1", "svc2"},
			},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon:      "simple",
					DaemonScope: snap.UserDaemon,
				},
			},
			map[int][]string{
				1337: {"svc2"},
			},
			map[int][]string{
				1337: {"svc1"},
			},
			nil,
			"some missing, some found disabled services",
		},
		// some disabled services, but app is not service
		{
			map[int][]string{
				1337: {"svc1"},
			},
			map[string]*snap.AppInfo{
				"svc1": {
					Daemon:      "",
					DaemonScope: snap.UserDaemon,
				},
			},
			map[int][]string{
				1337: {"svc1"},
			},
			map[int][]string{
				1337: {},
			},
			nil,
			"some disabled services that are now apps",
		},
	} {
		info := &snap.Info{Apps: tt.apps}

		overview, err := snapstate.MissingDisabledServices(nil, tt.stDisabledSvcsList, info)
		c.Assert(overview.MissingUserServices, DeepEquals, tt.missing, Commentf(tt.comment))
		c.Assert(overview.FoundUserServices, DeepEquals, tt.found, Commentf(tt.comment))
		c.Assert(overview.MissingSystemServices, HasLen, 0)
		c.Assert(overview.FoundSystemServices, HasLen, 0)
		c.Assert(err, Equals, tt.err, Commentf(tt.comment))
	}
}

type testLinkParticipant struct {
	callCount      int
	instanceNames  []string
	linkageChanged func(st *state.State, snapsup *snapstate.SnapSetup) error
}

func (lp *testLinkParticipant) SnapLinkageChanged(st *state.State, snapsup *snapstate.SnapSetup) error {
	lp.callCount++
	lp.instanceNames = append(lp.instanceNames, snapsup.InstanceName())
	if lp.linkageChanged != nil {
		return lp.linkageChanged(st, snapsup)
	}
	return nil
}

func (s *handlersSuite) TestAddLinkParticipant(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// Mock link snap participants. This ensures we can add a participant
	// without affecting the other tests, as the original list will be
	// restored.
	restore := snapstate.MockLinkSnapParticipants(nil)
	defer restore()

	lp := &testLinkParticipant{
		linkageChanged: func(st *state.State, snapsup *snapstate.SnapSetup) error {
			c.Assert(st, NotNil)
			c.Check(snapsup.InstanceName(), Equals, "snap-name")
			return nil
		},
	}
	snapstate.AddLinkSnapParticipant(lp)

	t := s.state.NewTask("link-snap", "test")
	snapstate.NotifyLinkParticipants(t, &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "snap-name"}})
	c.Assert(lp.callCount, Equals, 1)
}

func (s *handlersSuite) TestNotifyLinkParticipantsErrorHandling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// See comment in TestAddLinkParticipant for details.
	restore := snapstate.MockLinkSnapParticipants(nil)
	defer restore()

	lp := &testLinkParticipant{
		linkageChanged: func(st *state.State, snapsup *snapstate.SnapSetup) error {
			return fmt.Errorf("something failed")
		},
	}
	snapstate.AddLinkSnapParticipant(lp)

	t := s.state.NewTask("link-snap", "test")
	snapstate.NotifyLinkParticipants(t, &snapstate.SnapSetup{SideInfo: &snap.SideInfo{RealName: "snap-name"}})
	c.Assert(lp.callCount, Equals, 1)
	logs := t.Log()
	c.Assert(logs, HasLen, 1)
	c.Check(logs[0], testutil.Contains, "ERROR something failed")
}

func (s *handlersSuite) TestGetHiddenDirOptionsFromSnapState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)

	confKey := fmt.Sprintf("experimental.%s", features.HiddenSnapDataHomeDir)
	err := tr.Set("core", confKey, "true")
	c.Assert(err, IsNil)
	tr.Commit()

	// check options reflect flag and SnapState
	snapst := &snapstate.SnapState{MigratedHidden: true}
	opts, err := snapstate.GetDirMigrationOpts(s.state, snapst, nil)

	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &snapstate.DirMigrationOptions{UseHidden: true, MigratedToHidden: true})
}

func (s *handlersSuite) TestGetHiddenDirOptionsFromSnapSetup(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, t := range []struct {
		snapsup   snapstate.SnapSetup
		opts      *snapstate.DirMigrationOptions
		expectErr bool
	}{
		{snapstate.SnapSetup{MigratedHidden: true}, &snapstate.DirMigrationOptions{MigratedToHidden: true}, false},
		{snapstate.SnapSetup{UndidHiddenMigration: true}, &snapstate.DirMigrationOptions{}, false},
		{snapstate.SnapSetup{}, &snapstate.DirMigrationOptions{}, false},
		{snapstate.SnapSetup{MigratedToExposedHome: true}, &snapstate.DirMigrationOptions{MigratedToExposedHome: true}, false},
		{snapstate.SnapSetup{EnableExposedHome: true}, &snapstate.DirMigrationOptions{MigratedToExposedHome: true}, false},
		{snapstate.SnapSetup{RemovedExposedHome: true}, &snapstate.DirMigrationOptions{}, false},
		{snapstate.SnapSetup{DisableExposedHome: true}, &snapstate.DirMigrationOptions{}, false},
		{snapstate.SnapSetup{EnableExposedHome: true, DisableExposedHome: true}, nil, true},
		{snapstate.SnapSetup{EnableExposedHome: true, RemovedExposedHome: true}, nil, true},
		{snapstate.SnapSetup{MigratedToExposedHome: true, RemovedExposedHome: true}, nil, true},
		{snapstate.SnapSetup{MigratedToExposedHome: true, DisableExposedHome: true}, nil, true},
		{snapstate.SnapSetup{MigratedHidden: true, UndidHiddenMigration: true}, nil, true},
	} {

		opts, err := snapstate.GetDirMigrationOpts(s.state, nil, &t.snapsup)
		if t.expectErr {
			c.Check(err, Not(IsNil))
		} else {
			c.Check(err, IsNil)
			c.Check(opts, DeepEquals, t.opts)
		}
	}
}

func (s *handlersSuite) TestGetHiddenDirOptionsSnapSetupOverrideSnapState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)

	confKey := fmt.Sprintf("experimental.%s", features.HiddenSnapDataHomeDir)
	err := tr.Set("core", confKey, "true")
	c.Assert(err, IsNil)
	tr.Commit()

	snapst := &snapstate.SnapState{MigratedHidden: true, MigratedToExposedHome: false}
	snapsup := &snapstate.SnapSetup{UndidHiddenMigration: true, MigratedToExposedHome: true}
	opts, err := snapstate.GetDirMigrationOpts(s.state, snapst, snapsup)

	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &snapstate.DirMigrationOptions{UseHidden: true, MigratedToHidden: false, MigratedToExposedHome: true})
}

func (s *handlersSuite) TestGetSnapDirOptsFromState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	restore := snapstate.MockGetHiddenDirOptions(func(*state.State, *snapstate.SnapState, *snapstate.SnapSetup) (*snapstate.DirMigrationOptions, error) {
		return &snapstate.DirMigrationOptions{UseHidden: true, MigratedToHidden: true, MigratedToExposedHome: true}, nil
	})
	defer restore()

	opts, err := snapstate.GetSnapDirOpts(s.state, "")
	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &dirs.SnapDirOptions{HiddenSnapDataDir: true, MigratedToExposedHome: true})
}

func (s *handlersSuite) TestGetHiddenDirOptionsNoState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)

	// set feature flag
	confKey := fmt.Sprintf("experimental.%s", features.HiddenSnapDataHomeDir)
	err := tr.Set("core", confKey, "true")
	c.Assert(err, IsNil)
	tr.Commit()

	// check options reflect flag and no SnapState
	opts, err := snapstate.GetDirMigrationOpts(s.state, nil, nil)

	c.Assert(err, IsNil)
	c.Check(opts, DeepEquals, &snapstate.DirMigrationOptions{UseHidden: true})
}

func (s *handlersSuite) TestDoEnforceValidationSetsTask(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	vsEncoded := fmt.Sprintf(`type: validation-set
authority-id: canonical
series: 16
account-id: canonical
name: foo-set
sequence: 2
snaps:
  -
    name: baz
    id: bazlinuxidididididididididididid
    presence: optional
    revision: 13
timestamp: %s
body-length: 0
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`, time.Now().Truncate(time.Second).UTC().Format(time.RFC3339))
	expectedVss := map[string][]byte{
		"foo-set": []byte(vsEncoded),
	}
	expectedPinnedSeqs := map[string]int{
		"foo-set": 4,
	}

	var enforcedCalls int
	r := snapstate.MockEnforceValidationSets(func(s *state.State, m1 map[string]*asserts.ValidationSet, m2 map[string]int, is []*snapasserts.InstalledSnap, m3 map[string]bool, userID int) error {
		enforcedCalls++
		c.Check(m1, HasLen, 1)
		c.Check(m1["foo-set"].AccountID(), Equals, "canonical")
		c.Check(m1["foo-set"].Name(), Equals, "foo-set")
		c.Check(m1["foo-set"].Sequence(), Equals, 2)
		c.Check(m2, DeepEquals, expectedPinnedSeqs)
		c.Check(userID, Equals, 1)
		return nil
	})
	defer r()

	t := s.state.NewTask("enforce-validation-sets", "test")
	t.Set("pinned-sequence-numbers", expectedPinnedSeqs)
	t.Set("validation-sets", expectedVss)
	t.Set("userID", 1)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()
	c.Check(enforcedCalls, Equals, 1)
}

func (s *handlersSuite) TestDoEnforceValidationSetsTaskLocal(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	expectedVsKeys := map[string][]string{
		"test-set-1": {"1", "2", "3", "4"},
	}
	expectedPinnedSeqs := map[string]int{
		"test-set-1": 4,
	}

	var enforcedCalls int
	r := snapstate.MockEnforceLocalValidationSets(func(s *state.State, m1 map[string][]string, m2 map[string]int, is []*snapasserts.InstalledSnap, m3 map[string]bool) error {
		enforcedCalls++
		c.Check(m1, DeepEquals, expectedVsKeys)
		c.Check(m2, DeepEquals, expectedPinnedSeqs)
		return nil
	})
	defer r()

	t := s.state.NewTask("enforce-validation-sets", "test")
	t.Set("pinned-sequence-numbers", expectedPinnedSeqs)
	t.Set("validation-set-keys", expectedVsKeys)

	chg := s.state.NewChange("sample", "...")
	chg.AddTask(t)

	s.state.Unlock()
	s.se.Ensure()
	s.se.Wait()
	s.state.Lock()
	c.Check(enforcedCalls, Equals, 1)
}
