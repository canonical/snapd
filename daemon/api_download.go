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
	"errors"
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

var snapDownloadCmd = &Command{
	Path:     "/v2/download",
	PolkitOK: "io.snapcraft.snapd.manage",
	POST:     postSnapDownload,
}

// SnapDownloadAction is used to request a snap download
type snapDownloadAction struct {
	SnapName string `json:"snap-name,omitempty"`
	snapRevisionOptions
}

var errDownloadNameRequired = errors.New("download operation requires one snap name")

func (action *snapDownloadAction) validate() error {
	if action.SnapName == "" {
		return errDownloadNameRequired
	}
	return action.snapRevisionOptions.validate()
}

func postSnapDownload(c *Command, r *http.Request, user *auth.UserState) Response {
	var action snapDownloadAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&action); err != nil {
		return BadRequest("cannot decode request body into download operation: %v", err)
	}
	if decoder.More() {
		return BadRequest("extra content found after download operation")
	}

	if err := action.validate(); err != nil {
		return BadRequest(err.Error())
	}

	return streamOneSnap(c, action, user)
}

func streamOneSnap(c *Command, action snapDownloadAction, user *auth.UserState) Response {
	actions := []*store.SnapAction{{
		Action:       "download",
		InstanceName: action.SnapName,
		Revision:     action.Revision,
		CohortKey:    action.CohortKey,
		Channel:      action.Channel,
	}}
	sars, err := getStore(c).SnapAction(context.TODO(), nil, actions, user, nil)
	if err != nil {
		return errToResponse(err, []string{action.SnapName}, InternalError, "cannot download snap: %v")
	}
	if len(sars) != 1 {
		return InternalError("internal error: unexpected number %v of results for a single download", len(sars))
	}
	info := sars[0].Info

	downloadInfo := info.DownloadInfo
	resume := int64(0)
	r, status, err := getStore(c).DownloadStream(context.TODO(), action.SnapName, &downloadInfo, resume, user)
	if err != nil {
		return InternalError(err.Error())
	}
	// XXX: check for 206 as well here once we set resume to something other
	// than "0"
	if status != 200 {
		return InternalError("internal error: unexpected status code from DownloadStream: %v", status)
	}

	return fileStream{
		SnapName: action.SnapName,
		Filename: info.Filename(),
		Info:     downloadInfo,
		stream:   r,
	}
}
