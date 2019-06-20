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

package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

func (s *apiSuite) TestPostRemodelUnhappy(c *check.C) {
	data, err := json.Marshal(postModelData{NewModel: "invalid model"})
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/model", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := postModel(appsCmd, req, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Assert(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, "cannot decode new model assertion: .*")
}

func (s *apiSuite) TestPostRemodel(c *check.C) {
	oldModel := s.brands.Model("my-brand", "my-old-model", modelDefaults)
	newModel := s.brands.Model("my-brand", "my-old-model", modelDefaults, map[string]interface{}{
		"revision": "2",
	})

	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(deviceMgr)
	st := d.overlord.State()
	st.Lock()
	assertstatetest.AddMany(st, s.storeSigning.StoreAccountKey(""))
	assertstatetest.AddMany(st, s.brands.AccountsAndKeys("my-brand")...)
	s.mockModel(c, st, oldModel)
	st.Unlock()

	soon := 0
	ensureStateSoon = func(st *state.State) {
		soon++
		ensureStateSoonImpl(st)
	}
	defer func() { ensureStateSoon = func(st *state.State) {} }()

	var devicestateRemodelGotModel *asserts.Model
	devicestateRemodel = func(st *state.State, nm *asserts.Model) (*state.Change, error) {
		devicestateRemodelGotModel = nm
		chg := st.NewChange("remodel", "...")
		return chg, nil
	}

	// create a valid model assertion
	c.Assert(err, check.IsNil)
	modelEncoded := string(asserts.Encode(newModel))
	data, err := json.Marshal(postModelData{NewModel: modelEncoded})
	c.Check(err, check.IsNil)

	// set it and validate that this is what we was passed to
	// devicestateRemodel
	req, err := http.NewRequest("POST", "/v2/model", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := postModel(appsCmd, req, nil).(*resp)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Check(devicestateRemodelGotModel, check.DeepEquals, newModel)

	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Assert(st.Changes(), check.HasLen, 1)
	chg1 := st.Changes()[0]
	c.Assert(chg, check.DeepEquals, chg1)
	c.Assert(chg.Kind(), check.Equals, "remodel")
	c.Assert(chg.Err(), check.IsNil)

	c.Assert(soon, check.Equals, 1)
}
