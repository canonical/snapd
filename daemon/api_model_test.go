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
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
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

func (s *apiSuite) testPostRemodel(c *check.C, newModel map[string]interface{}, expectedChgSummary string) {
	d := s.daemonWithOverlordMock(c)
	hookMgr, err := hookstate.Manager(d.overlord.State(), d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	deviceMgr, err := devicestate.Manager(d.overlord.State(), hookMgr, d.overlord.TaskRunner())
	c.Assert(err, check.IsNil)
	d.overlord.AddManager(deviceMgr)
	st := d.overlord.State()
	st.Lock()
	s.mockModel(c, st)
	st.Unlock()

	var devicestateRemodelGotModel *asserts.Model
	devicestateRemodel = func(st *state.State, nm *asserts.Model) ([]*state.TaskSet, error) {
		devicestateRemodelGotModel = nm
		return nil, nil
	}

	// create a valid model assertion
	mockModel, err := s.storeSigning.RootSigning.Sign(asserts.ModelType, newModel, nil, "")
	c.Assert(err, check.IsNil)
	mockModelEncoded := string(asserts.Encode(mockModel))
	data, err := json.Marshal(postModelData{NewModel: mockModelEncoded})
	c.Check(err, check.IsNil)

	// set it and validate that this is what we was passed to
	// devicestateRemodel
	req, err := http.NewRequest("POST", "/v2/model", bytes.NewBuffer(data))
	c.Assert(err, check.IsNil)
	rsp := postModel(appsCmd, req, nil).(*resp)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Check(devicestateRemodelGotModel, check.DeepEquals, mockModel)

	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Check(chg.Summary(), check.Equals, expectedChgSummary)
}

func (s *apiSuite) TestPostRemodelDifferentBrandModel(c *check.C) {
	newModel := map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	expectedChgSummary := "Remodel device to my-brand/my-model (0)"
	s.testPostRemodel(c, newModel, expectedChgSummary)
}

func (s *apiSuite) TestPostRemodelSameBrandModelDifferentRev(c *check.C) {
	newModel := make(map[string]interface{})
	for k, v := range makeMockModelHdrs() {
		newModel[k] = v
	}
	newModel["revision"] = "2"

	expectedChgSummary := "Refresh model assertion from revision 0 to 2"
	s.testPostRemodel(c, newModel, expectedChgSummary)
}
