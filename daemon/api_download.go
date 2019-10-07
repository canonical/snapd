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
	"path/filepath"

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
	SnapName string              `json:"snap-name,omitempty"`
	Options  snapRevisionOptions `json:"options,omitempty"`
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

	if action.SnapName == "" {
		return BadRequest("download operation requires one snap name")
	}

	return streamOneSnap(c, user, action.SnapName, &action.Options)
}

func streamOneSnap(c *Command, user *auth.UserState, snapName string, dlOpts *snapRevisionOptions) Response {
	if dlOpts == nil {
		dlOpts = &snapRevisionOptions{}
	}
	actions := []*store.SnapAction{{
		Action:       "download",
		InstanceName: snapName,
		Revision:     dlOpts.Revision,
		CohortKey:    dlOpts.CohortKey,
		Channel:      dlOpts.Channel,
	}}
	snaps, err := getStore(c).SnapAction(context.TODO(), nil, actions, user, nil)
	if err != nil {
		return SnapNotFound(snapName, err)
	}
	if len(snaps) != 1 {
		return InternalError("internal error: unexpected number %v of results for a single download", len(snaps))
	}
	info := snaps[0]

	downloadInfo := info.DownloadInfo
	r, err := getStore(c).DownloadStream(context.TODO(), snapName, &downloadInfo, user)
	if err != nil {
		return InternalError(err.Error())
	}

	return fileStream{
		SnapName: snapName,
		Filename: filepath.Base(info.MountFile()),
		Info:     downloadInfo,
		stream:   r,
	}
}
