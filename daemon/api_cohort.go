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
	"context"
	"encoding/json"
	"net/http"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/auth"
)

var cohortsCmd = &Command{
	Path:        "/v2/cohorts",
	POST:        postCohorts,
	WriteAccess: authenticatedAccess{},
}

func postCohorts(c *Command, r *http.Request, user *auth.UserState) Response {
	var inst client.CohortAction
	dec := json.NewDecoder(r.Body)
	mylog.Check(dec.Decode(&inst))

	if dec.More() {
		return BadRequest("spurious content after cohort instruction")
	}

	if inst.Action != "create" {
		return BadRequest("unknown cohort action %q", inst.Action)
	}

	if len(inst.Snaps) == 0 {
		// nothing to do ¯\_(ツ)_/¯
		return SyncResponse(map[string]string{})
	}

	cohorts := mylog.Check2(storeFrom(c.d).CreateCohorts(context.TODO(), inst.Snaps))

	return SyncResponse(cohorts)
}
