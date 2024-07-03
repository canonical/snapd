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
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/snapshotstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var _ = check.Suite(&snapshotSuite{})

type snapshotSuite struct {
	apiBaseSuite
}

func (s *snapshotSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)
	s.daemonWithOverlordMock()
	s.expectAuthenticatedAccess()
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
}

func (s *snapshotSuite) TestSnapshotManyOptionsNone(c *check.C) {
	defer daemon.MockSnapshotSave(func(s *state.State, snaps, users []string,
		options map[string]*snap.SnapshotOptions) (uint64, []string, *state.TaskSet, error) {
		c.Check(snaps, check.HasLen, 2)
		c.Check(options, check.IsNil)
		t := s.NewTask("fake-snapshot-2", "Snapshot two")
		return 1, snaps, state.NewTaskSet(t), nil
	})()

	inst := daemon.MustUnmarshalSnapInstruction(c, `{"action": "snapshot", "snaps": ["foo", "bar"]}`)

	st := s.d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(context.Background(), inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Snapshot snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
}

func (s *snapshotSuite) TestSnapshotManyOptionsFull(c *check.C) {
	var snapshotSaveCalled int
	defer daemon.MockSnapshotSave(func(s *state.State, snaps, users []string,
		options map[string]*snap.SnapshotOptions) (uint64, []string, *state.TaskSet, error) {
		snapshotSaveCalled++
		c.Check(snaps, check.HasLen, 2)
		c.Check(options, check.HasLen, 2)
		c.Check(options, check.DeepEquals, map[string]*snap.SnapshotOptions{
			"foo": {Exclude: []string{"foo-path-1", "foo-path-2"}},
			"bar": {Exclude: []string{"bar-path-1", "bar-path-2"}},
		})
		t := s.NewTask("fake-snapshot-2", "Snapshot two")
		return 1, snaps, state.NewTaskSet(t), nil
	})()

	inst := daemon.MustUnmarshalSnapInstruction(c, `{"action": "snapshot", "snaps": ["foo", "bar"],
	"snapshot-options": {"foo": {"exclude":["foo-path-1", "foo-path-2"]}, "bar":{"exclude":["bar-path-1", "bar-path-2"]}}}`)

	st := s.d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(context.Background(), inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Snapshot snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
	c.Check(snapshotSaveCalled, check.Equals, 1)
}

func (s *snapshotSuite) TestSnapshotManyError(c *check.C) {
	defer daemon.MockSnapshotSave(func(s *state.State, snaps, users []string,
		options map[string]*snap.SnapshotOptions) (uint64, []string, *state.TaskSet, error) {
		c.Check(snaps, check.HasLen, 2)
		return 0, nil, nil, &snap.NotInstalledError{Snap: "foo"}
	})()

	inst := daemon.MustUnmarshalSnapInstruction(c, `{"action": "snapshot", "snaps": ["foo", "bar"]}`)

	st := s.d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(context.Background(), inst, st)
	st.Unlock()
	c.Check(res, check.IsNil)
	c.Check(err, check.ErrorMatches, `snap "foo" is not installed`)
}

func (s *snapshotSuite) TestListSnapshots(c *check.C) {
	s.expectOpenAccess()

	snapshots := []client.SnapshotSet{{ID: 1}, {ID: 42}}

	defer daemon.MockSnapshotList(func(context.Context, *state.State, uint64, []string) ([]client.SnapshotSet, error) {
		return snapshots, nil
	})()

	req, err := http.NewRequest("GET", "/v2/snapshots", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, snapshots)
}

func (s *snapshotSuite) TestListSnapshotsFiltering(c *check.C) {
	s.expectOpenAccess()

	snapshots := []client.SnapshotSet{{ID: 1}, {ID: 42}}

	defer daemon.MockSnapshotList(func(_ context.Context, st *state.State, setID uint64, _ []string) ([]client.SnapshotSet, error) {
		c.Assert(setID, check.Equals, uint64(42))
		return snapshots[1:], nil
	})()

	req, err := http.NewRequest("GET", "/v2/snapshots?set=42", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, []client.SnapshotSet{{ID: 42}})
}

func (s *snapshotSuite) TestListSnapshotsBadFiltering(c *check.C) {
	s.expectOpenAccess()

	defer daemon.MockSnapshotList(func(_ context.Context, _ *state.State, setID uint64, _ []string) ([]client.SnapshotSet, error) {
		c.Fatal("snapshotList should not be reached (should have been blocked by validation!)")
		return nil, nil
	})()

	req, err := http.NewRequest("GET", "/v2/snapshots?set=no", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `'set', if given, must be a positive base 10 number; got "no"`)
}

func (s *snapshotSuite) TestListSnapshotsListError(c *check.C) {
	s.expectOpenAccess()

	defer daemon.MockSnapshotList(func(_ context.Context, _ *state.State, setID uint64, _ []string) ([]client.SnapshotSet, error) {
		return nil, errors.New("no")
	})()

	req, err := http.NewRequest("GET", "/v2/snapshots", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 500)
	c.Check(rspe.Message, check.Equals, "no")
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

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400, comm)
		c.Check(rspe.Message, check.Matches, test.error, comm)
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

			rspe := s.errorReq(c, req, nil)
			c.Check(rspe.Status, check.Equals, 404, comm)
			c.Check(rspe.Message, check.Matches, expectedError.Error(), comm)
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

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 500, comm)
		c.Check(rspe.Message, check.Matches, expectedError.Error(), comm)
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

	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	for _, action := range []string{"check", "restore", "forget"} {
		comm := check.Commentf("%s", action)
		body := fmt.Sprintf(`{"set": 42, "action": "%s"}`, action)
		req, err := http.NewRequest("POST", "/v2/snapshots", strings.NewReader(body))

		c.Assert(err, check.IsNil, comm)

		st.Unlock()
		rsp := s.asyncReq(c, req, nil)
		st.Lock()

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

func (s *snapshotSuite) TestExportSnapshots(c *check.C) {
	var snapshotExportCalled int

	defer daemon.MockSnapshotExport(func(ctx context.Context, st *state.State, setID uint64) (*snapshotstate.SnapshotExport, error) {
		snapshotExportCalled++
		c.Check(setID, check.Equals, uint64(1))
		return &snapshotstate.SnapshotExport{}, nil
	})()

	req, err := http.NewRequest("GET", "/v2/snapshots/1/export", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil)
	c.Check(rsp, check.FitsTypeOf, &daemon.SnapshotExportResponse{})
	c.Check(snapshotExportCalled, check.Equals, 1)
}

func (s *snapshotSuite) TestExportSnapshotsBadRequestOnNonNumericID(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/snapshots/xxx/export", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `'id' must be a positive base 10 number; got "xxx"`)
}

func (s *snapshotSuite) TestExportSnapshotsBadRequestOnError(c *check.C) {
	var snapshotExportCalled int

	defer daemon.MockSnapshotExport(func(ctx context.Context, st *state.State, setID uint64) (*snapshotstate.SnapshotExport, error) {
		snapshotExportCalled++
		return nil, fmt.Errorf("boom")
	})()

	req, err := http.NewRequest("GET", "/v2/snapshots/1/export", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `cannot export 1: boom`)
	c.Check(snapshotExportCalled, check.Equals, 1)
}

func (s *snapshotSuite) TestImportSnapshot(c *check.C) {
	data := []byte("mocked snapshot export data file")

	setID := uint64(3)
	snapNames := []string{"baz", "bar", "foo"}
	defer daemon.MockSnapshotImport(func(context.Context, *state.State, io.Reader) (uint64, []string, error) {
		return setID, snapNames, nil
	})()

	req, err := http.NewRequest("POST", "/v2/snapshots", bytes.NewReader(data))
	req.Header.Add("Content-Length", strconv.Itoa(len(data)))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", client.SnapshotExportMediaType)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{"set-id": setID, "snaps": snapNames})
}

func (s *snapshotSuite) TestImportSnapshotError(c *check.C) {
	defer daemon.MockSnapshotImport(func(context.Context, *state.State, io.Reader) (uint64, []string, error) {
		return uint64(0), nil, errors.New("no")
	})()

	data := []byte("mocked snapshot export data file")
	req, err := http.NewRequest("POST", "/v2/snapshots", bytes.NewReader(data))
	req.Header.Add("Content-Length", strconv.Itoa(len(data)))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", client.SnapshotExportMediaType)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, "no")
}

func (s *snapshotSuite) TestImportSnapshotNoContentLengthError(c *check.C) {
	data := []byte("mocked snapshot export data file")
	req, err := http.NewRequest("POST", "/v2/snapshots", bytes.NewReader(data))
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", client.SnapshotExportMediaType)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `cannot parse Content-Length: strconv.ParseInt: parsing "": invalid syntax`)
}

func (s *snapshotSuite) TestImportSnapshotLimits(c *check.C) {
	var dataRead int

	defer daemon.MockSnapshotImport(func(ctx context.Context, st *state.State, r io.Reader) (uint64, []string, error) {
		data, err := io.ReadAll(r)
		c.Assert(err, check.IsNil)
		dataRead = len(data)
		return uint64(0), nil, nil
	})()

	data := []byte("much more data than expected from Content-Length")
	req, err := http.NewRequest("POST", "/v2/snapshots", bytes.NewReader(data))
	// limit to 10 and check that this is really all that is read
	req.Header.Add("Content-Length", "10")
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", client.SnapshotExportMediaType)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(dataRead, check.Equals, 10)
}
