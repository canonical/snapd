// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var _ = check.Suite(&aliasesSuite{})

type aliasesSuite struct {
	apiBaseSuite
}

const aliasYaml = `
name: alias-snap
version: 1
apps:
 app:
 app2:
`

func (s *aliasesSuite) TestAliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &daemon.AliasAction{
		Action: "alias",
		Snap:   "alias-snap",
		App:    "app",
		Alias:  "alias1",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// validity check
	c.Check(osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, "alias1")), check.Equals, true)
}

func (s *aliasesSuite) TestAliasChangeConflict(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	s.daemon(c)

	s.mockSnap(c, aliasYaml)

	s.simulateConflict("alias-snap")

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	action := &daemon.AliasAction{
		Action: "alias",
		Snap:   "alias-snap",
		App:    "app",
		Alias:  "alias1",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Check(rec.Code, check.Equals, 409)

	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	c.Check(body, check.DeepEquals, map[string]interface{}{
		"status-code": 409.,
		"status":      "Conflict",
		"result": map[string]interface{}{
			"message": `snap "alias-snap" has "manip" change in progress`,
			"kind":    "snap-change-conflict",
			"value": map[string]interface{}{
				"change-kind": "manip",
				"snap-name":   "alias-snap",
			},
		},
		"type": "error"})
}

func (s *aliasesSuite) TestAliasErrors(c *check.C) {
	s.daemon(c)

	errScenarios := []struct {
		mangle func(*daemon.AliasAction)
		err    string
	}{
		{func(a *daemon.AliasAction) { a.Action = "" }, `unsupported alias action: ""`},
		{func(a *daemon.AliasAction) { a.Action = "what" }, `unsupported alias action: "what"`},
		{func(a *daemon.AliasAction) { a.Snap = "lalala" }, `snap "lalala" is not installed`},
		{func(a *daemon.AliasAction) { a.Alias = ".foo" }, `invalid alias name: ".foo"`},
		{func(a *daemon.AliasAction) { a.Aliases = []string{"baz"} }, `cannot interpret request, snaps can no longer be expected to declare their aliases`},
	}

	for _, scen := range errScenarios {
		action := &daemon.AliasAction{
			Action: "alias",
			Snap:   "alias-snap",
			App:    "app",
			Alias:  "alias1",
		}
		scen.mangle(action)

		text, err := json.Marshal(action)
		c.Assert(err, check.IsNil)
		buf := bytes.NewBuffer(text)
		req, err := http.NewRequest("POST", "/v2/aliases", buf)
		c.Assert(err, check.IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400)
		c.Check(rspe.Message, check.Matches, scen.err)
	}
}

func (s *aliasesSuite) TestUnaliasSnapSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &daemon.AliasAction{
		Action: "unalias",
		Snap:   "alias-snap",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Disable all aliases for snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// validity check
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "alias-snap", &snapst)
	c.Assert(err, check.IsNil)
	c.Check(snapst.AutoAliasesDisabled, check.Equals, true)
}

func (s *aliasesSuite) TestUnaliasDWIMSnapSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &daemon.AliasAction{
		Action: "unalias",
		Snap:   "alias-snap",
		Alias:  "alias-snap",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Disable all aliases for snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// validity check
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "alias-snap", &snapst)
	c.Assert(err, check.IsNil)
	c.Check(snapst.AutoAliasesDisabled, check.Equals, true)
}

func (s *aliasesSuite) TestUnaliasAliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &daemon.AliasAction{
		Action: "alias",
		Snap:   "alias-snap",
		App:    "app",
		Alias:  "alias1",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// unalias
	action = &daemon.AliasAction{
		Action: "unalias",
		Alias:  "alias1",
	}
	text, err = json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf = bytes.NewBuffer(text)
	req, err = http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec = httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id = body["change"].(string)

	st.Lock()
	chg = st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Remove manual alias "alias1" for snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// validity check
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapBinariesDir, "alias1")), check.Equals, false)
}

func (s *aliasesSuite) TestUnaliasDWIMAliasSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &daemon.AliasAction{
		Action: "alias",
		Snap:   "alias-snap",
		App:    "app",
		Alias:  "alias1",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	err = chg.Err()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// DWIM unalias an alias
	action = &daemon.AliasAction{
		Action: "unalias",
		Snap:   "alias1",
		Alias:  "alias1",
	}
	text, err = json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf = bytes.NewBuffer(text)
	req, err = http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec = httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id = body["change"].(string)

	st.Lock()
	chg = st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Remove manual alias "alias1" for snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// validity check
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapBinariesDir, "alias1")), check.Equals, false)
}

func (s *aliasesSuite) TestPreferSuccess(c *check.C) {
	err := os.MkdirAll(dirs.SnapBinariesDir, 0755)
	c.Assert(err, check.IsNil)
	d := s.daemon(c)

	s.mockSnap(c, aliasYaml)

	oldAutoAliases := snapstate.AutoAliases
	snapstate.AutoAliases = func(*state.State, *snap.Info) (map[string]string, error) {
		return nil, nil
	}
	defer func() { snapstate.AutoAliases = oldAutoAliases }()

	d.Overlord().Loop()
	defer d.Overlord().Stop()

	action := &daemon.AliasAction{
		Action: "prefer",
		Snap:   "alias-snap",
	}
	text, err := json.Marshal(action)
	c.Assert(err, check.IsNil)
	buf := bytes.NewBuffer(text)
	req, err := http.NewRequest("POST", "/v2/aliases", buf)
	c.Assert(err, check.IsNil)
	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	var body map[string]interface{}
	err = json.Unmarshal(rec.Body.Bytes(), &body)
	c.Check(err, check.IsNil)
	id := body["change"].(string)

	st := d.Overlord().State()
	st.Lock()
	chg := st.Change(id)
	c.Check(chg.Summary(), check.Equals, `Prefer aliases of snap "alias-snap"`)
	st.Unlock()
	c.Assert(chg, check.NotNil)

	<-chg.Ready()

	st.Lock()
	defer st.Unlock()
	err = chg.Err()
	c.Assert(err, check.IsNil)

	// validity check
	var snapst snapstate.SnapState
	err = snapstate.Get(st, "alias-snap", &snapst)
	c.Assert(err, check.IsNil)
	c.Check(snapst.AutoAliasesDisabled, check.Equals, false)
}

func (s *aliasesSuite) TestAliases(c *check.C) {
	d := s.daemon(c)

	st := d.Overlord().State()
	st.Lock()
	snapstate.Set(st, "alias-snap1", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap1", Revision: snap.R(11)},
		}),
		Current: snap.R(11),
		Active:  true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias1": {Manual: "cmd1x", Auto: "cmd1"},
			"alias2": {Auto: "cmd2"},
		},
	})
	snapstate.Set(st, "alias-snap2", &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "alias-snap2", Revision: snap.R(12)},
		}),
		Current:             snap.R(12),
		Active:              true,
		AutoAliasesDisabled: true,
		Aliases: map[string]*snapstate.AliasTarget{
			"alias2": {Auto: "cmd2"},
			"alias3": {Manual: "cmd3"},
			"alias4": {Manual: "cmd4x", Auto: "cmd4"},
		},
	})
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/aliases", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, map[string]map[string]daemon.AliasStatus{
		"alias-snap1": {
			"alias1": {
				Command: "alias-snap1.cmd1x",
				Status:  "manual",
				Manual:  "cmd1x",
				Auto:    "cmd1",
			},
			"alias2": {
				Command: "alias-snap1.cmd2",
				Status:  "auto",
				Auto:    "cmd2",
			},
		},
		"alias-snap2": {
			"alias2": {
				Command: "alias-snap2.cmd2",
				Status:  "disabled",
				Auto:    "cmd2",
			},
			"alias3": {
				Command: "alias-snap2.cmd3",
				Status:  "manual",
				Manual:  "cmd3",
			},
			"alias4": {
				Command: "alias-snap2.cmd4x",
				Status:  "manual",
				Manual:  "cmd4x",
				Auto:    "cmd4",
			},
		},
	})

}

func (s *aliasesSuite) TestInstallUnaliased(c *check.C) {
	var calledFlags snapstate.Flags

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "install",
		// Install the snap without enabled automatic aliases
		Unaliased: true,
		Snaps:     []string{"fake"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.Unaliased, check.Equals, true)
}

func (s *aliasesSuite) TestInstallIgnoreRunning(c *check.C) {
	var calledFlags snapstate.Flags

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "install",
		// Install the snap without enabled automatic aliases
		IgnoreRunning: true,
		Snaps:         []string{"fake"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.IgnoreRunning, check.Equals, true)
}

func (s *aliasesSuite) TestInstallPrefer(c *check.C) {
	var calledFlags snapstate.Flags

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "install",
		Prefer: true,
		Snaps:  []string{"fake"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.Prefer, check.Equals, true)
}
