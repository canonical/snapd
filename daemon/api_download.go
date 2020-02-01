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
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

var snapDownloadCmd = &Command{
	Path:     "/v2/download",
	PolkitOK: "io.snapcraft.snapd.manage",
	POST:     postSnapDownload,
}

var validRangeRegexp = regexp.MustCompile(`^\s*bytes=(\d+)-\s*`)

// SnapDownloadAction is used to request a snap download
type snapDownloadAction struct {
	SnapName string `json:"snap-name"`
	snapRevisionOptions
	ResumeStamp    string `json:"resume-stamp"`
	resumePosition int64
	NoBody         bool `json:"no-body"`
}

var (
	errDownloadNameRequired  = errors.New("download operation requires one snap name")
	errDownloadNoBodyResume  = errors.New("cannot request no body when resuming")
	errDownloadResumeNoStamp = errors.New("cannot resume without a stamp")
	errDownloadBadResume     = errors.New("resume position cannot be negative")
)

func (action *snapDownloadAction) validate() error {
	if action.SnapName == "" {
		return errDownloadNameRequired
	}
	if action.NoBody && (action.resumePosition > 0 || action.ResumeStamp != "") {
		return errDownloadNoBodyResume
	}
	if action.resumePosition > 0 && action.ResumeStamp == "" {
		return errDownloadResumeNoStamp
	}
	if action.resumePosition < 0 {
		return errDownloadBadResume
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
	if rangestr := r.Header.Get("Range"); rangestr != "" {
		// "An origin server MUST ignore a Range header field
		//  that contains a range unit it does not understand."
		subs := validRangeRegexp.FindStringSubmatch(rangestr)
		if len(subs) == 2 {
			n, err := strconv.ParseInt(subs[1], 10, 64)
			if err == nil {
				action.resumePosition = n
			}
		}
	}

	if err := action.validate(); err != nil {
		return BadRequest(err.Error())
	}

	return streamOneSnap(c, action, user)
}

func streamOneSnap(c *Command, action snapDownloadAction, user *auth.UserState) Response {
	theStore := getStore(c)
	var info *snap.Info
	if true {
		// XXX: once we're HMAC'ing, we only do this bit on the first pass
		actions := []*store.SnapAction{{
			Action:       "download",
			InstanceName: action.SnapName,
			Revision:     action.Revision,
			CohortKey:    action.CohortKey,
			Channel:      action.Channel,
		}}
		sars, err := theStore.SnapAction(context.TODO(), nil, actions, user, nil)
		if err != nil {
			return errToResponse(err, []string{action.SnapName}, InternalError, "cannot download snap: %v")
		}
		if len(sars) != 1 {
			return InternalError("internal error: unexpected number %v of results for a single download", len(sars))
		}
		info = sars[0].Info
	}
	downloadInfo := info.DownloadInfo

	// XXX: this bit goes away once we HMAC
	if action.ResumeStamp != "" && downloadInfo.Sha3_384 != action.ResumeStamp {
		return BadRequest("snap to download has different hash")
	}

	rsp := fileStream{
		SnapName: action.SnapName,
		Filename: filepath.Base(info.MountFile()),
		Info:     downloadInfo,
		resume:   action.resumePosition,
	}
	if !action.NoBody {
		r, s, err := theStore.DownloadStream(context.TODO(), action.SnapName, &downloadInfo, action.resumePosition, user)
		if err != nil {
			return InternalError(err.Error())
		}
		rsp.stream = r
		if s != 206 {
			// AFAICT this happens with the CDN every time
			// but this might be transient
			// (in any case it's valid as per the RFC)
			logger.Debugf("store refused our range request")
			rsp.resume = 0
		}
	}

	return rsp
}
