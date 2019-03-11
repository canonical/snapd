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
	"net/http"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
)

var _ = check.Suite(&snapFileSuite{})

type snapFileSuite struct{}

func (s *snapFileSuite) SetUpTest(c *check.C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *snapFileSuite) TestGetFile(c *check.C) {
	defer daemon.MockMuxVars(func(*http.Request) map[string]string {
		return map[string]string{"name": "foo"}
	})()

	c.Check(daemon.SnapFileCmd.Path, check.Equals, "/v2/snaps/{name}/file")

	o := overlord.Mock()
	daemon.NewWithOverlord(o)
	st := o.State()

	type scenario struct {
		status                   int
		exists, active, try, wat bool
		err                      string
	}

	req, err := http.NewRequest("GET", "/v2/snaps/foo/file", nil)
	c.Assert(err, check.IsNil)

	for i, scen := range []scenario{
		{exists: true, active: true},
		{exists: false, err: "no state entry for key"},
		{exists: true, active: false, err: `cannot download file of inactive snap "foo"`},
		{exists: true, active: true, try: true, err: `cannot download file for try-mode snap "foo"`},
		{exists: true, wat: true, err: `cannot download file for snap "foo": internal error: .*`},
	} {
		var snapst snapstate.SnapState
		if scen.wat {
			st.Lock()
			st.Set("snaps", 42)
			st.Unlock()
		} else {
			if scen.exists {
				sideInfo := &snap.SideInfo{Revision: snap.R(-1), RealName: "foo"}
				snapst.Active = scen.active
				snapst.Current = sideInfo.Revision
				snapst.Sequence = append(snapst.Sequence, sideInfo)
				if scen.try {
					snapst.TryMode = true
				}
			}
			st.Lock()
			snapstate.Set(st, "foo", &snapst)
			st.Unlock()
		}

		rsp := daemon.GetSnapFile(daemon.SnapFileCmd, req, nil)
		if scen.err == "" {
			c.Check(string(rsp.(daemon.FileResponse)), check.Equals, filepath.Join(dirs.SnapBlobDir, "foo_x1.snap"), check.Commentf("%d", i))
		} else {
			c.Assert(rsp, check.FitsTypeOf, &daemon.Resp{}, check.Commentf("%d", i))
			result := rsp.(*daemon.Resp).Result
			c.Assert(result, check.FitsTypeOf, &daemon.ErrorResult{}, check.Commentf("%d", i))
			c.Check(result.(*daemon.ErrorResult).Message, check.Matches, scen.err, check.Commentf("%d", i))
		}
	}
}
