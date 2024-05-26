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
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/randutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

var snapDownloadCmd = &Command{
	Path:        "/v2/download",
	POST:        postSnapDownload,
	WriteAccess: authenticatedAccess{Polkit: polkitActionManage},
}

var validRangeRegexp = regexp.MustCompile(`^\s*bytes=(\d+)-\s*$`)

// SnapDownloadAction is used to request a snap download
type snapDownloadAction struct {
	SnapName string `json:"snap-name"`
	snapRevisionOptions

	// HeaderPeek if set requests a peek at the header without the
	// body being returned.
	HeaderPeek bool `json:"header-peek"`

	ResumeToken    string `json:"resume-token"`
	resumePosition int64
}

var (
	errDownloadNameRequired     = errors.New("download operation requires one snap name")
	errDownloadHeaderPeekResume = errors.New("cannot request header-only peek when resuming")
	errDownloadResumeNoToken    = errors.New("cannot resume without a token")
)

func (action *snapDownloadAction) validate() error {
	if action.SnapName == "" {
		return errDownloadNameRequired
	}
	if action.HeaderPeek && (action.resumePosition > 0 || action.ResumeToken != "") {
		return errDownloadHeaderPeekResume
	}
	if action.resumePosition > 0 && action.ResumeToken == "" {
		return errDownloadResumeNoToken
	}
	return action.snapRevisionOptions.validate()
}

func postSnapDownload(c *Command, r *http.Request, user *auth.UserState) Response {
	var action snapDownloadAction
	decoder := json.NewDecoder(r.Body)
	mylog.Check(decoder.Decode(&action))

	if decoder.More() {
		return BadRequest("extra content found after download operation")
	}
	if rangestr := r.Header.Get("Range"); rangestr != "" {
		// "An origin server MUST ignore a Range header field
		//  that contains a range unit it does not understand."
		subs := validRangeRegexp.FindStringSubmatch(rangestr)
		if len(subs) == 2 {
			n := mylog.Check2(strconv.ParseInt(subs[1], 10, 64))
			if err == nil {
				action.resumePosition = n
			}
		}
	}
	mylog.Check(action.validate())

	return streamOneSnap(c, action, user)
}

func streamOneSnap(c *Command, action snapDownloadAction, user *auth.UserState) Response {
	secret := mylog.Check2(downloadTokensSecret(c.d))

	theStore := storeFrom(c.d)

	var ss *snapStream
	if action.ResumeToken == "" {
		var info *snap.Info
		actions := []*store.SnapAction{{
			Action:       "download",
			InstanceName: action.SnapName,
			Revision:     action.Revision,
			CohortKey:    action.CohortKey,
			Channel:      action.Channel,
		}}
		results, _ := mylog.Check3(theStore.SnapAction(context.TODO(), nil, actions, nil, user, nil))

		if len(results) != 1 {
			return InternalError("internal error: unexpected number %v of results for a single download", len(results))
		}
		info = results[0].Info

		ss = mylog.Check2(newSnapStream(action.SnapName, info, secret))

	} else {

		ss = mylog.Check2(newResumingSnapStream(action.SnapName, action.ResumeToken, secret))

		ss.resume = action.resumePosition
	}

	if !action.HeaderPeek {
		stream, status := mylog.Check3(theStore.DownloadStream(context.TODO(), action.SnapName, ss.Info, action.resumePosition, user))

		ss.stream = stream
		if status != 206 {
			// store/cdn has no partial content (valid
			// reply per RFC)
			logger.Debugf("store refused our range request")
			ss.resume = 0
		}
	}

	return ss
}

func newSnapStream(snapName string, info *snap.Info, secret []byte) (*snapStream, error) {
	dlInfo := &info.DownloadInfo
	fname := filepath.Base(info.MountFile())
	tokenJSON := downloadTokenJSON{
		SnapName: snapName,
		Filename: fname,
		Info:     dlInfo,
	}
	tokStr := mylog.Check2(sealDownloadToken(&tokenJSON, secret))

	return &snapStream{
		SnapName: snapName,
		Filename: fname,
		Info:     dlInfo,
		Token:    tokStr,
	}, nil
}

func newResumingSnapStream(snapName string, tokStr string, secret []byte) (*snapStream, error) {
	d := mylog.Check2(unsealDownloadToken(tokStr, secret))

	if d.SnapName != snapName {
		return nil, fmt.Errorf("resume snap name does not match original snap name")
	}
	return &snapStream{
		SnapName: snapName,
		Filename: d.Filename,
		Info:     d.Info,
		Token:    tokStr,
	}, nil
}

type downloadTokenJSON struct {
	SnapName string             `json:"snap-name"`
	Filename string             `json:"filename"`
	Info     *snap.DownloadInfo `json:"dl-info"`
}

func sealDownloadToken(d *downloadTokenJSON, secret []byte) (string, error) {
	b := mylog.Check2(json.Marshal(d))

	mac := hmac.New(sha256.New, secret)
	mac.Write(b)
	// append the HMAC hash to b to build the full raw token tok
	tok := mac.Sum(b)
	return base64.RawURLEncoding.EncodeToString(tok), nil
}

var errInvalidDownloadToken = errors.New("download token is invalid")

func unsealDownloadToken(tokStr string, secret []byte) (*downloadTokenJSON, error) {
	tok := mylog.Check2(base64.RawURLEncoding.DecodeString(tokStr))

	sz := len(tok)
	if sz < sha256.Size {
		return nil, errInvalidDownloadToken
	}
	h := tok[sz-sha256.Size:]
	b := tok[:sz-sha256.Size]
	mac := hmac.New(sha256.New, secret)
	mac.Write(b)
	if !hmac.Equal(h, mac.Sum(nil)) {
		return nil, errInvalidDownloadToken
	}
	var d downloadTokenJSON
	mylog.Check(json.Unmarshal(b, &d))

	return &d, nil
}

func downloadTokensSecret(d *Daemon) (secret []byte, err error) {
	st := d.overlord.State()
	st.Lock()
	defer st.Unlock()
	const k = "api-download-tokens-secret"
	mylog.Check(st.Get(k, &secret))
	if err == nil {
		return secret, nil
	}
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, err
	}
	secret = mylog.Check2(randutil.CryptoTokenBytes(32))

	st.Set(k, secret)
	st.Set(k+"-time", time.Now().UTC())
	return secret, nil
}
