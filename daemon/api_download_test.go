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

package daemon_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
)

type fakeStore struct{}

var _ = check.Suite(&snapDownloadSuite{})

type snapDownloadSuite struct {
	storetest.Store
	d *daemon.Daemon

	snaps []string
}

func (s *snapDownloadSuite) SetUpTest(c *check.C) {
	s.snaps = nil

	o := overlord.Mock()
	s.d = daemon.NewWithOverlord(o)

	st := o.State()
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)
	dirs.SetRootDir(c.MkDir())
}

var snapContent = "SNAP"

var storeSnaps = map[string]*snap.Info{
	"bar": {
		SideInfo: snap.SideInfo{
			RealName: "bar",
			Revision: snap.R(1),
		},
		DownloadInfo: snap.DownloadInfo{
			Size:            int64(len(snapContent)),
			AnonDownloadURL: "http://localhost/bar",
			Sha3_384:        "sha3sha3sha3",
		},
	},
	"edge-bar": {
		SideInfo: snap.SideInfo{
			RealName: "edge-bar",
			Revision: snap.R(1),
			// this is the channel we expect in the test
			Channel: "edge",
		},
		DownloadInfo: snap.DownloadInfo{
			Size:            int64(len(snapContent)),
			AnonDownloadURL: "http://localhost/edge-bar",
			Sha3_384:        "sha3sha3sha3",
		},
	},
	"rev7-bar": {
		SideInfo: snap.SideInfo{
			RealName: "rev7-bar",
			// this is the revision we expect in the test
			Revision: snap.R(7),
		},
		DownloadInfo: snap.DownloadInfo{
			Size:            int64(len(snapContent)),
			AnonDownloadURL: "http://localhost/rev7-bar",
			Sha3_384:        "sha3sha3sha3",
		},
	},
	"download-error-trigger-snap": {
		DownloadInfo: snap.DownloadInfo{
			Size:            100,
			AnonDownloadURL: "http://localhost/foo",
			Sha3_384:        "sha3sha3sha3",
		},
	},
	"foo-resume-3": {
		SideInfo: snap.SideInfo{
			RealName: "foo-resume-3",
			Revision: snap.R(1),
		},
		DownloadInfo: snap.DownloadInfo{
			Size:            int64(len(snapContent)),
			AnonDownloadURL: "http://localhost/foo-resume-3",
			Sha3_384:        "sha3sha3sha3",
		},
	},
}

func (s *snapDownloadSuite) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, error) {
	if len(actions) != 1 {
		panic(fmt.Sprintf("unexpected amount of actions: %v", len(actions)))
	}
	action := actions[0]
	if action.Action != "download" {
		panic(fmt.Sprintf("unexpected action: %q", action.Action))
	}
	info, ok := storeSnaps[action.InstanceName]
	if !ok {
		return nil, store.ErrSnapNotFound
	}
	if action.Channel != info.Channel {
		panic(fmt.Sprintf("unexpected channel %q for %v snap", action.Channel, action.InstanceName))
	}
	if !action.Revision.Unset() && action.Revision != info.Revision {
		panic(fmt.Sprintf("unexpected revision %q for %s snap", action.Revision, action.InstanceName))
	}
	return []store.SnapActionResult{{Info: info}}, nil
}

func (s *snapDownloadSuite) DownloadStream(ctx context.Context, name string, downloadInfo *snap.DownloadInfo, resume int64, user *auth.UserState) (io.ReadCloser, int, error) {
	if name == "download-error-trigger-snap" {
		return nil, 0, fmt.Errorf("error triggered by download-error-trigger-snap")
	}
	if name == "foo-resume-3" && resume != 3 {
		return nil, 0, fmt.Errorf("foo-resume-3 should set resume position to 3 instead of %v", resume)
	}
	if _, ok := storeSnaps[name]; ok {
		status := 200
		if resume > 0 {
			status = 206
		}
		return ioutil.NopCloser(bytes.NewReader([]byte(snapContent[resume:]))), status, nil
	}
	panic(fmt.Sprintf("internal error: trying to download %s but not in storeSnaps", name))
}

func (s *snapDownloadSuite) TestDownloadSnapErrors(c *check.C) {
	type scenario struct {
		dataJSON string
		status   int
		err      string
	}

	for _, scen := range []scenario{
		{
			dataJSON: `{"snap-name": ""}`,
			status:   400,
			err:      "download operation requires one snap name",
		},
		{
			dataJSON: `{"}`,
			status:   400,
			err:      `cannot decode request body into download operation: unexpected EOF`,
		},
		{
			dataJSON: `{"snap-name": "doom","channel":"latest/potato"}`,
			status:   400,
			err:      `invalid risk in channel name: latest/potato`,
		},
	} {
		var err error
		data := []byte(scen.dataJSON)

		req, err := http.NewRequest("POST", "/v2/download", bytes.NewBuffer(data))
		c.Assert(err, check.IsNil)
		rsp := daemon.PostSnapDownload(daemon.SnapDownloadCmd, req, nil)

		c.Assert(rsp.(*daemon.Resp).Status, check.Equals, scen.status)
		if scen.err == "" {
			c.Errorf("error was expected")
		}
		result := rsp.(*daemon.Resp).Result
		c.Check(result.(*daemon.ErrorResult).Message, check.Matches, scen.err)
	}
}

func (s *snapDownloadSuite) TestStreamOneSnap(c *check.C) {
	type scenario struct {
		snapName string
		dataJSON string
		status   int
		resume   int
		noBody   bool
		err      string
	}

	sec, err := daemon.DownloadTokensSecret(daemon.SnapDownloadCmd)
	c.Assert(err, check.IsNil)

	fooResume3SS, err := daemon.NewSnapStream("foo-resume-3", storeSnaps["foo-resume-3"], sec)
	c.Assert(err, check.IsNil)
	tok, err := base64.RawURLEncoding.DecodeString(fooResume3SS.Token)
	c.Assert(err, check.IsNil)
	c.Assert(bytes.HasPrefix(tok, []byte(`{"snap-name":"foo-resume-3","filename":"foo-resume-3_1.snap","dl-info":{"`)), check.Equals, true)

	brokenHashToken := base64.RawURLEncoding.EncodeToString(append(tok[:len(tok)-1], tok[len(tok)-1]-1))

	for _, s := range []scenario{
		{
			snapName: "doom",
			dataJSON: `{"snap-name": "doom"}`,
			status:   404,
			err:      "snap not found",
		},
		{
			snapName: "download-error-trigger-snap",
			dataJSON: `{"snap-name": "download-error-trigger-snap"}`,
			status:   500,
			err:      "error triggered by download-error-trigger-snap",
		},
		{
			snapName: "bar",
			dataJSON: `{"snap-name": "bar"}`,
			status:   200,
			err:      "",
		},
		{
			snapName: "edge-bar",
			dataJSON: `{"snap-name": "edge-bar", "channel":"edge"}`,
			status:   200,
			err:      "",
		},
		{
			snapName: "rev7-bar",
			dataJSON: `{"snap-name": "rev7-bar", "revision":"7"}`,
			status:   200,
			err:      "",
		},
		// happy resume
		{
			snapName: "foo-resume-3",
			dataJSON: fmt.Sprintf(`{"snap-name": "foo-resume-3", "resume-token": %q}`, fooResume3SS.Token),
			status:   206,
			resume:   3,
			err:      "",
		},
		// unhappy resume
		{
			snapName: "foo-resume-3",
			dataJSON: fmt.Sprintf(`{"snap-name": "foo-resume-other", "resume-token": %q}`, fooResume3SS.Token),
			status:   400,
			resume:   3,
			err:      "resume snap name does not match original snap name",
		},
		{
			snapName: "foo-resume-3",
			dataJSON: `{"snap-name": "foo-resume-3", "resume-token": "invalid token"}`, // not base64
			status:   400,
			resume:   3,
			err:      "download token is invalid",
		},
		{
			snapName: "foo-resume-3",
			dataJSON: `{"snap-name": "foo-resume-3", "resume-token": "e30"}`, // too short token content
			status:   400,
			resume:   3,
			err:      "download token is invalid",
		},
		{
			snapName: "foo-resume-3",
			dataJSON: fmt.Sprintf(`{"snap-name": "foo-resume-3", "resume-token": %q}`, brokenHashToken), // token with broken hash
			status:   400,
			resume:   3,
			err:      "download token is invalid",
		},

		{
			snapName: "foo-resume-3",
			dataJSON: `{"snap-name": "foo-resume-3", "resume-stamp": ""}`,
			status:   400,
			resume:   3,
			err:      "cannot resume without a token",
		},
		{
			snapName: "foo-resume-3",
			dataJSON: fmt.Sprintf(`{"snap-name": "foo-resume-3", "resume-stamp": %q}`, fooResume3SS.Token),
			status:   500,
			resume:   -10,
			// negative values are ignored and resume is set to 0
			err: "foo-resume-3 should set resume position to 3 instead of 0",
		},
		{
			snapName: "foo-resume-3",
			dataJSON: `{"snap-name": "foo-resume-3", "header-peek": true}`,
			status:   400,
			resume:   3,
			err:      "cannot request header-only peek when resuming",
		},
		{
			snapName: "foo-resume-3",
			dataJSON: `{"snap-name": "foo-resume-3", "header-peek": true, "resume-token": "something"}`,
			status:   400,
			err:      "cannot request header-only peek when resuming",
		},
		{
			snapName: "foo-resume-3",
			dataJSON: `{"snap-name": "foo-resume-3", "header-peek": true, "resume-token": "something"}`,
			resume:   3,
			status:   400,
			err:      "cannot request header-only peek when resuming",
		},
	} {
		req, err := http.NewRequest("POST", "/v2/download", strings.NewReader(s.dataJSON))
		c.Assert(err, check.IsNil)
		if s.resume != 0 {
			req.Header.Add("Range", fmt.Sprintf("bytes=%d-", s.resume))
		}

		rsp := daemon.SnapDownloadCmd.POST(daemon.SnapDownloadCmd, req, nil)

		if s.err != "" {
			c.Check(rsp.(*daemon.Resp).Status, check.Equals, s.status, check.Commentf("unexpected result for %v", s.dataJSON))
			result := rsp.(*daemon.Resp).Result
			c.Check(result.(*daemon.ErrorResult).Message, check.Matches, s.err, check.Commentf("unexpected result for %v", s.dataJSON))
		} else {
			c.Assert(rsp, check.FitsTypeOf, &daemon.SnapStream{}, check.Commentf("unexpected result for %v", s.dataJSON))
			ss := rsp.(*daemon.SnapStream)
			c.Assert(ss.SnapName, check.Equals, s.snapName, check.Commentf("invalid result %v for %v", rsp, s.dataJSON))
			c.Assert(ss.Info.Size, check.Equals, int64(len(snapContent)))

			w := httptest.NewRecorder()
			ss.ServeHTTP(w, nil)

			expectedLength := fmt.Sprintf("%d", len(snapContent)-s.resume)

			info := storeSnaps[s.snapName]
			c.Assert(w.Code, check.Equals, s.status)
			c.Assert(w.Header().Get("Content-Length"), check.Equals, expectedLength)
			c.Assert(w.Header().Get("Content-Type"), check.Equals, "application/octet-stream")
			c.Assert(w.Header().Get("Content-Disposition"), check.Equals, fmt.Sprintf("attachment; filename=%s_%s.snap", s.snapName, info.Revision))
			c.Assert(w.Header().Get("Snap-Sha3-384"), check.Equals, "sha3sha3sha3", check.Commentf("invalid sha3 for %v", s.snapName))
			c.Assert(w.Body.Bytes(), check.DeepEquals, []byte("SNAP")[s.resume:])
			c.Assert(w.Header().Get("Snap-Download-Token"), check.Equals, ss.Token)
			if s.status == 206 {
				c.Assert(w.Header().Get("Content-Range"), check.Equals, fmt.Sprintf("bytes %d-%d/%d", s.resume, len(snapContent)-1, len(snapContent)))
				c.Assert(ss.Token, check.Equals, "")
			}
		}
	}
}

func (s *snapDownloadSuite) TestStreamOneSnapHeaderOnlyPeek(c *check.C) {
	dataJSON := `{"snap-name": "bar", "header-peek": true}`
	req, err := http.NewRequest("POST", "/v2/download", strings.NewReader(dataJSON))
	c.Assert(err, check.IsNil)

	rsp := daemon.SnapDownloadCmd.POST(daemon.SnapDownloadCmd, req, nil)

	c.Assert(rsp, check.FitsTypeOf, &daemon.SnapStream{})
	ss := rsp.(*daemon.SnapStream)
	c.Assert(ss.SnapName, check.Equals, "bar")
	c.Assert(ss.Info.Size, check.Equals, int64(len(snapContent)))

	w := httptest.NewRecorder()
	ss.ServeHTTP(w, nil)
	c.Assert(w.Code, check.Equals, 200)

	// we get the relevant headers
	c.Check(w.Header().Get("Content-Disposition"), check.Equals, "attachment; filename=bar_1.snap")
	c.Check(w.Header().Get("Snap-Sha3-384"), check.Equals, "sha3sha3sha3")
	// but no body
	c.Check(w.Body.Bytes(), check.HasLen, 0)
}

func (s *snapDownloadSuite) TestStreamRangeHeaderErrors(c *check.C) {
	dataJSON := `{"snap-name":"bar"}`

	for _, s := range []string{
		// missing "-" at the end
		"bytes=123",
		// missing "bytes="
		"123-",
		// real range, not supported
		"bytes=1-2",
		// almost
		"bytes=1--",
	} {
		req, err := http.NewRequest("POST", "/v2/download", strings.NewReader(dataJSON))
		c.Assert(err, check.IsNil)
		// missng "-" at the end
		req.Header.Add("Range", s)

		rsp := daemon.SnapDownloadCmd.POST(daemon.SnapDownloadCmd, req, nil)
		if dr, ok := rsp.(*daemon.Resp); ok {
			c.Fatalf("unexpected daemon result (test broken): %v", dr.Result)
		}
		w := httptest.NewRecorder()
		ss := rsp.(*daemon.SnapStream)
		ss.ServeHTTP(w, nil)
		// range header is invalid and ignored
		c.Assert(w.Code, check.Equals, 200)
	}
}
