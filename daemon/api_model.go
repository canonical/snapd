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
	"encoding/json"
	"net/http"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
)

var modelCmd = &Command{
	Path: "/v2/model",
	POST: postModel,
	// TODO: provide GET here too once we decided on the details of the API
}

var devicestateRemodel = devicestate.Remodel

type postModelData struct {
	NewModel string `json:"new-model"`
}

func postModel(c *Command, r *http.Request, _ *auth.UserState) Response {
	defer r.Body.Close()
	var data postModelData
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&data); err != nil {
		return BadRequest("cannot decode request body into remodel operation: %v", err)
	}
	rawNewModel, err := asserts.Decode([]byte(data.NewModel))
	if err != nil {
		return BadRequest("cannot decode new model assertion: %v", err)
	}
	newModel, ok := rawNewModel.(*asserts.Model)
	if !ok {
		return BadRequest("new model is not a model assertion: %v", newModel.Type())
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	chg, err := devicestateRemodel(st, newModel)
	if err != nil {
		return BadRequest("cannot remodel device: %v", err)
	}
	ensureStateSoon(st)

	return AsyncResponse(nil, &Meta{Change: chg.ID()})

}
