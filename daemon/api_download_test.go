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

var content = "SNAP"

func (s *snapDownloadSuite) SnapInfo(ctx context.Context, spec store.SnapSpec, user *auth.UserState) (*snap.Info, error) {
	switch spec.Name {
	case "bar":
		return &snap.Info{
			SideInfo: snap.SideInfo{
				RealName: "bar",
				Revision: snap.R(1),
			},
			DownloadInfo: snap.DownloadInfo{
				Size:            int64(len(content)),
				AnonDownloadURL: "http://localhost/bar",
			},
		}, nil
	case "download-error-trigger-snap":
		return &snap.Info{
			DownloadInfo: snap.DownloadInfo{
				Size:            100,
				AnonDownloadURL: "http://localhost/foo",
			},
		}, nil
	default:
		return nil, store.ErrSnapNotFound
	}
}

func (s *snapDownloadSuite) DownloadStream(ctx context.Context, name string, downloadInfo *snap.DownloadInfo, user *auth.UserState) (io.ReadCloser, error) {
	if name == "bar" {
		return ioutil.NopCloser(bytes.NewReader([]byte(content))), nil
	}
	return nil, fmt.Errorf("unexpected error")
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
		dataJSON string
		status   int
		err      string
	}

	for _, s := range []scenario{
		{
			dataJSON: `{"snap-name": "doom"}`,
			status:   404,
			err:      "snap not found",
		},
		{
			dataJSON: `{"snap-name": "download-error-trigger-snap"}`,
			status:   500,
			err:      "unexpected error",
		},
		{
			dataJSON: `{"snap-name": "bar"}`,
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
			c.Assert(rsp.(daemon.FileStream).SnapName, check.Equals, "bar")
			c.Assert(rsp.(daemon.FileStream).Info.Size, check.Equals, int64(len(content)))

			w := httptest.NewRecorder()
			rsp.(daemon.FileStream).ServeHTTP(w, nil)

			expectedLength := fmt.Sprintf("%d", len(content))

			c.Assert(w.Code, check.Equals, s.status)
			c.Assert(w.Header().Get("Content-Length"), check.Equals, expectedLength)
			c.Assert(w.Header().Get("Content-Type"), check.Equals, "application/octet-stream")
			c.Assert(w.Header().Get("Content-Disposition"), check.Equals, "attachment; filename=bar_1.snap")
			c.Assert(w.Body.String(), check.Equals, "SNAP")
		}
	}
}
