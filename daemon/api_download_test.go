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
}

func (s *snapDownloadSuite) SnapAction(ctx context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, user *auth.UserState, opts *store.RefreshOptions) ([]*snap.Info, error) {
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
	return []*snap.Info{info}, nil
}

func (s *snapDownloadSuite) DownloadStream(ctx context.Context, name string, downloadInfo *snap.DownloadInfo, user *auth.UserState) (io.ReadCloser, error) {
	if name == "download-error-trigger-snap" {
		return nil, fmt.Errorf("error triggered by download-error-trigger-snap")
	}
	if _, ok := storeSnaps[name]; ok {
		return ioutil.NopCloser(bytes.NewReader([]byte(snapContent))), nil
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
		err      string
	}

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
	} {
		req, err := http.NewRequest("POST", "/v2/download", strings.NewReader(s.dataJSON))
		c.Assert(err, check.IsNil)
		rsp := daemon.SnapDownloadCmd.POST(daemon.SnapDownloadCmd, req, nil)

		if s.err != "" {
			c.Check(rsp.(*daemon.Resp).Status, check.Equals, s.status, check.Commentf("unexpected result for %v", s.dataJSON))
			result := rsp.(*daemon.Resp).Result
			c.Check(result.(*daemon.ErrorResult).Message, check.Matches, s.err, check.Commentf("unexpected result for %v", s.dataJSON))
		} else {
			c.Assert(rsp, check.FitsTypeOf, daemon.FileStream{}, check.Commentf("unexpected result for %v", s.dataJSON))
			c.Assert(rsp.(daemon.FileStream).SnapName, check.Equals, s.snapName, check.Commentf("invalid result %v for %v", rsp, s.dataJSON))
			c.Assert(rsp.(daemon.FileStream).Info.Size, check.Equals, int64(len(snapContent)))

			w := httptest.NewRecorder()
			rsp.(daemon.FileStream).ServeHTTP(w, nil)

			expectedLength := fmt.Sprintf("%d", len(snapContent))

			info := storeSnaps[s.snapName]
			c.Assert(w.Code, check.Equals, s.status)
			c.Assert(w.Header().Get("Content-Length"), check.Equals, expectedLength)
			c.Assert(w.Header().Get("Content-Type"), check.Equals, "application/octet-stream")
			c.Assert(w.Header().Get("Content-Disposition"), check.Equals, fmt.Sprintf("attachment; filename=%s_%s.snap", s.snapName, info.Revision))
			c.Assert(w.Header().Get("X-Sha3-384"), check.Equals, "sha3sha3sha3", check.Commentf("invalid sha3 for %v", s.snapName))
			c.Assert(w.Body.String(), check.Equals, "SNAP")
		}
	}
}
