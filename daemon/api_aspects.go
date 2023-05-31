// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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
	"errors"
	"fmt"
	"net/http"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/overlord/auth"
)

var (
	aspectsCmd = &Command{
		Path:        "/v2/aspects",
		GET:         getAspect,
		POST:        setAspect,
		ReadAccess:  authenticatedAccess{Polkit: polkitActionManage},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
	}
)

type AspectRequest struct {
	Account    string      `json:"account"`
	BundleName string      `json:"bundle"`
	Aspect     string      `json:"aspect"`
	Field      string      `json:"field"`
	Value      interface{} `json:"value"`
}

func (r *AspectRequest) validate() error {
	const emptyFieldFmt = "cannot have empty %q field"

	if r.Account == "" {
		return fmt.Errorf(emptyFieldFmt, "account")
	} else if r.BundleName == "" {
		return fmt.Errorf(emptyFieldFmt, "bundle")
	} else if r.Aspect == "" {
		return fmt.Errorf(emptyFieldFmt, "aspect")
	} else if r.Field == "" {
		return fmt.Errorf(emptyFieldFmt, "field")
	}

	return nil
}

func getAspect(c *Command, r *http.Request, _ *auth.UserState) Response {
	decoder := json.NewDecoder(r.Body)
	var req AspectRequest
	if err := decoder.Decode(&req); err != nil {
		return BadRequest("cannot decode aspect request body: %v", err)
	}

	if err := req.validate(); err != nil {
		return BadRequest(err.Error())
	}

	var value interface{}
	err := aspectstateGet(c.d.state, req.Account, req.BundleName, req.Aspect, req.Field, &value)
	if err != nil {
		if aspects.IsNotFoundErr(err) {
			return NotFound(err.Error())
		} else if errors.Is(err, &aspects.InvalidAccessError{}) {
			return &apiError{
				Status:  403,
				Message: err.Error(),
			}
		}
		return InternalError(err.Error())
	}

	return SyncResponse(value)
}

func setAspect(c *Command, r *http.Request, _ *auth.UserState) Response {
	decoder := json.NewDecoder(r.Body)
	var req AspectRequest
	if err := decoder.Decode(&req); err != nil {
		return BadRequest("cannot decode aspect request body: %v", err)
	}

	if err := req.validate(); err != nil {
		return BadRequest(err.Error())
	}

	err := aspectstateSet(c.d.state, req.Account, req.BundleName, req.Aspect, req.Field, req.Value)
	if err != nil {
		if aspects.IsNotFoundErr(err) {
			return NotFound(err.Error())
		} else if errors.Is(err, &aspects.InvalidAccessError{}) {
			return &apiError{
				Status:  403,
				Message: err.Error(),
			}
		}
		return InternalError(err.Error())
	}

	return SyncResponse(nil)
}
