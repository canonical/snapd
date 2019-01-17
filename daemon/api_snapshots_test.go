// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package daemon_test

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"golang.org/x/net/context"
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store/storetest"
)

var _ = check.Suite(&snapshotSuite{})

type snapshotSuite struct {
	d *daemon.Daemon
	o *overlord.Overlord
}

func (s *snapshotSuite) SetUpTest(c *check.C) {
	s.o = overlord.Mock()
	s.d = daemon.NewWithOverlord(s.o)

	st := s.o.State()
	// adds an assertion db
	assertstate.Manager(st, s.o.TaskRunner())
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, storetest.Store{})
}

func (s *snapshotSuite) TearDownTest(c *check.C) {
	s.o = nil
	s.d = nil
}

func (s *snapshotSuite) TestSnapshotMany(c *check.C) {
	defer daemon.MockSnapshotSave(func(s *state.State, snaps, users []string) (uint64, []string, *state.TaskSet, error) {
		c.Check(snaps, check.HasLen, 2)
		t := s.NewTask("fake-snapshot-2", "Snapshot two")
		return 1, snaps, state.NewTaskSet(t), nil
	})()

	inst := daemon.MustUnmarshalSnapInstruction(c, `{"action": "snapshot", "snaps": ["foo", "bar"]}`)
	st := s.o.State()
	st.Lock()
	res, err := daemon.SnapshotMany(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Snapshot snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
}

func (s *snapshotSuite) TestListSnapshots(c *check.C) {
	snapshots := []client.SnapshotSet{{ID: 1}, {ID: 42}}

	defer daemon.MockSnapshotList(func(context.Context, uint64, []string) ([]client.SnapshotSet, error) {
		return snapshots, nil
	})()

	c.Check(daemon.SnapshotCmd.Path, check.Equals, "/v2/snapshots")
	req, err := http.NewRequest("GET", "/v2/snapshots", nil)
	c.Assert(err, check.IsNil)

	rsp := daemon.ListSnapshots(daemon.SnapshotCmd, req, nil)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, snapshots)
}

func (s *snapshotSuite) TestListSnapshotsFiltering(c *check.C) {
	snapshots := []client.SnapshotSet{{ID: 1}, {ID: 42}}

	defer daemon.MockSnapshotList(func(_ context.Context, setID uint64, _ []string) ([]client.SnapshotSet, error) {
		c.Assert(setID, check.Equals, uint64(42))
		return snapshots[1:], nil
	})()

	req, err := http.NewRequest("GET", "/v2/snapshots?set=42", nil)
	c.Assert(err, check.IsNil)

	rsp := daemon.ListSnapshots(daemon.SnapshotCmd, req, nil)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, []client.SnapshotSet{{ID: 42}})
}

func (s *snapshotSuite) TestListSnapshotsBadFiltering(c *check.C) {
	defer daemon.MockSnapshotList(func(_ context.Context, setID uint64, _ []string) ([]client.SnapshotSet, error) {
		c.Fatal("snapshotList should not be reached (should have been blocked by validation!)")
		return nil, nil
	})()

	req, err := http.NewRequest("GET", "/v2/snapshots?set=no", nil)
	c.Assert(err, check.IsNil)

	rsp := daemon.ListSnapshots(daemon.SnapshotCmd, req, nil)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.ErrorResult().Message, check.Equals, `'set', if given, must be a positive base 10 number; got "no"`)
}

func (s *snapshotSuite) TestListSnapshotsListError(c *check.C) {
	defer daemon.MockSnapshotList(func(_ context.Context, setID uint64, _ []string) ([]client.SnapshotSet, error) {
		return nil, errors.New("no")
	})()

	c.Check(daemon.SnapshotCmd.Path, check.Equals, "/v2/snapshots")
	req, err := http.NewRequest("GET", "/v2/snapshots", nil)
	c.Assert(err, check.IsNil)

	rsp := daemon.ListSnapshots(daemon.SnapshotCmd, req, nil)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 500)
	c.Check(rsp.ErrorResult().Message, check.Equals, "no")
}

func (s *snapshotSuite) TestFormatSnapshotAction(c *check.C) {
	type table struct {
		action   string
		expected string
	}
	tests := []table{
		{
			`{"set": 2, "action": "verb"}`,
			`Verb of snapshot set #2`,
		}, {
			`{"set": 2, "action": "verb", "snaps": ["foo"]}`,
			`Verb of snapshot set #2 for snaps "foo"`,
		}, {
			`{"set": 2, "action": "verb", "snaps": ["foo", "bar"]}`,
			`Verb of snapshot set #2 for snaps "foo", "bar"`,
		}, {
			`{"set": 2, "action": "verb", "users": ["meep"]}`,
			`Verb of snapshot set #2 for users "meep"`,
		}, {
			`{"set": 2, "action": "verb", "users": ["meep", "quux"]}`,
			`Verb of snapshot set #2 for users "meep", "quux"`,
		}, {
			`{"set": 2, "action": "verb", "users": ["meep", "quux"], "snaps": ["foo", "bar"]}`,
			`Verb of snapshot set #2 for snaps "foo", "bar" for users "meep", "quux"`,
		},
	}

	for _, test := range tests {
		action := daemon.MustUnmarshalSnapshotAction(c, test.action)
		c.Check(action.String(), check.Equals, test.expected)
	}
}

func (s *snapshotSuite) TestChangeSnapshots400(c *check.C) {
	type table struct{ body, error string }
	tests := []table{
		{
			body:  `"woodchucks`,
			error: "cannot decode request body into snapshot operation:.*",
		}, {
			body:  `{}"woodchucks`,
			error: "extra content found after snapshot operation",
		}, {
			body:  `{}`,
			error: "snapshot operation requires snapshot set ID",
		}, {
			body:  `{"set": 42}`,
			error: "snapshot operation requires action",
		}, {
			body:  `{"set": 42, "action": "carrots"}`,
			error: `unknown snapshot operation "carrots"`,
		}, {
			body:  `{"set": 42, "action": "forget", "users": ["foo"]}`,
			error: `snapshot "forget" operation cannot specify users`,
		},
	}

	for i, test := range tests {
		comm := check.Commentf("%d:%q", i, test.body)
		req, err := http.NewRequest("POST", "/v2/snapshots", strings.NewReader(test.body))
		c.Assert(err, check.IsNil, comm)

		rsp := daemon.ChangeSnapshots(daemon.SnapshotCmd, req, nil)
		c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError, comm)
		c.Check(rsp.Status, check.Equals, 400, comm)
		c.Check(rsp.ErrorResult().Message, check.Matches, test.error, comm)
	}
}

func (s *snapshotSuite) TestChangeSnapshots404(c *check.C) {
	var done string
	expectedError := errors.New("bzzt")
	defer daemon.MockSnapshotCheck(func(*state.State, uint64, []string, []string) ([]string, *state.TaskSet, error) {
		done = "check"
		return nil, nil, expectedError
	})()
	defer daemon.MockSnapshotRestore(func(*state.State, uint64, []string, []string) ([]string, *state.TaskSet, error) {
		done = "restore"
		return nil, nil, expectedError
	})()
	defer daemon.MockSnapshotForget(func(*state.State, uint64, []string) ([]string, *state.TaskSet, error) {
		done = "forget"
		return nil, nil, expectedError
	})()
	for _, expectedError = range []error{client.ErrSnapshotSetNotFound, client.ErrSnapshotSnapsNotFound} {
		for _, action := range []string{"check", "restore", "forget"} {
			done = ""
			comm := check.Commentf("%s/%s", action, expectedError)
			body := fmt.Sprintf(`{"set": 42, "action": "%s"}`, action)
			req, err := http.NewRequest("POST", "/v2/snapshots", strings.NewReader(body))
			c.Assert(err, check.IsNil, comm)

			rsp := daemon.ChangeSnapshots(daemon.SnapshotCmd, req, nil)
			c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError, comm)
			c.Check(rsp.Status, check.Equals, 404, comm)
			c.Check(rsp.ErrorResult().Message, check.Matches, expectedError.Error(), comm)
			c.Check(done, check.Equals, action, comm)
		}
	}
}

func (s *snapshotSuite) TestChangeSnapshots500(c *check.C) {
	var done string
	expectedError := errors.New("bzzt")
	defer daemon.MockSnapshotCheck(func(*state.State, uint64, []string, []string) ([]string, *state.TaskSet, error) {
		done = "check"
		return nil, nil, expectedError
	})()
	defer daemon.MockSnapshotRestore(func(*state.State, uint64, []string, []string) ([]string, *state.TaskSet, error) {
		done = "restore"
		return nil, nil, expectedError
	})()
	defer daemon.MockSnapshotForget(func(*state.State, uint64, []string) ([]string, *state.TaskSet, error) {
		done = "forget"
		return nil, nil, expectedError
	})()
	for _, action := range []string{"check", "restore", "forget"} {
		comm := check.Commentf("%s", action)
		body := fmt.Sprintf(`{"set": 42, "action": "%s"}`, action)
		req, err := http.NewRequest("POST", "/v2/snapshots", strings.NewReader(body))
		c.Assert(err, check.IsNil, comm)

		rsp := daemon.ChangeSnapshots(daemon.SnapshotCmd, req, nil)
		c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError, comm)
		c.Check(rsp.Status, check.Equals, 500, comm)
		c.Check(rsp.ErrorResult().Message, check.Matches, expectedError.Error(), comm)
		c.Check(done, check.Equals, action, comm)
	}
}

func (s *snapshotSuite) TestChangeSnapshot(c *check.C) {
	var done string
	defer daemon.MockSnapshotCheck(func(*state.State, uint64, []string, []string) ([]string, *state.TaskSet, error) {
		done = "check"
		return []string{"foo"}, state.NewTaskSet(), nil
	})()
	defer daemon.MockSnapshotRestore(func(*state.State, uint64, []string, []string) ([]string, *state.TaskSet, error) {
		done = "restore"
		return []string{"foo"}, state.NewTaskSet(), nil
	})()
	defer daemon.MockSnapshotForget(func(*state.State, uint64, []string) ([]string, *state.TaskSet, error) {
		done = "forget"
		return []string{"foo"}, state.NewTaskSet(), nil
	})()

	st := s.o.State()
	st.Lock()
	defer st.Unlock()
	for _, action := range []string{"check", "restore", "forget"} {
		comm := check.Commentf("%s", action)
		body := fmt.Sprintf(`{"set": 42, "action": "%s"}`, action)
		req, err := http.NewRequest("POST", "/v2/snapshots", strings.NewReader(body))

		c.Assert(err, check.IsNil, comm)

		st.Unlock()
		rsp := daemon.ChangeSnapshots(daemon.SnapshotCmd, req, nil)
		st.Lock()

		c.Check(rsp.Type, check.Equals, daemon.ResponseTypeAsync, comm)
		c.Check(rsp.Status, check.Equals, 202, comm)
		c.Check(done, check.Equals, action, comm)

		chg := st.Change(rsp.Change)
		c.Assert(chg, check.NotNil)
		c.Assert(chg.Tasks(), check.HasLen, 0)

		c.Check(chg.Kind(), check.Equals, action+"-snapshot")
		var apiData map[string]interface{}
		err = chg.Get("api-data", &apiData)
		c.Assert(err, check.IsNil)
		c.Check(apiData, check.DeepEquals, map[string]interface{}{
			"snap-names": []interface{}{"foo"},
		})

	}
}
