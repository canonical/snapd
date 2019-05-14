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
	"context"
	"errors"
	"net/http"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/store/storetest"
)

var _ = check.Suite(&cohortSuite{})

type cohortSuite struct {
	storetest.Store
	d *daemon.Daemon

	snaps []string
	coh   map[string]string
	err   error
}

func (s *cohortSuite) CreateCohorts(_ context.Context, snaps []string) (map[string]string, error) {
	s.snaps = snaps[:]
	return s.coh, s.err
}

func (s *cohortSuite) SetUpTest(c *check.C) {
	s.snaps = nil
	s.coh = nil
	s.err = nil

	o := overlord.Mock()
	s.d = daemon.NewWithOverlord(o)

	st := o.State()
	st.Lock()
	defer st.Unlock()
	snapstate.ReplaceStore(st, s)
	dirs.SetRootDir(c.MkDir())
}

func (s *cohortSuite) TestCreateCohort(c *check.C) {
	s.coh = map[string]string{
		"foo": "cohort for foo",
		"bar": "cohort for bar",
	}

	req, err := http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create", "snaps": ["foo","bar"]}]`))
	c.Assert(err, check.IsNil)

	rsp := daemon.CohortsCmd.POST(daemon.CohortsCmd, req, nil)
	c.Check(rsp, check.DeepEquals, &daemon.Resp{
		Status: 200,
		Type:   "sync",
		Result: s.coh,
	})
}

func (s *cohortSuite) TestCreateCohortNoSnaps(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create"}]`))
	c.Assert(err, check.IsNil)

	rsp := daemon.CohortsCmd.POST(daemon.CohortsCmd, req, nil)
	c.Check(rsp, check.DeepEquals, &daemon.Resp{
		Status: 200,
		Type:   "sync",
		Result: map[string]string{},
	})
}

func (s *cohortSuite) TestCreateCohortBadAction(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "pupate", "snaps": ["foo","bar"]}]`))
	c.Assert(err, check.IsNil)

	rsp := daemon.CohortsCmd.POST(daemon.CohortsCmd, req, nil)
	c.Check(rsp, check.DeepEquals, &daemon.Resp{
		Status: 400,
		Type:   "error",
		Result: &daemon.ErrorResult{Message: `unknown cohort action "pupate"`},
	})
}

func (s *cohortSuite) TestCreateCohortError(c *check.C) {
	s.err = errors.New("something went wrong")

	req, err := http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create", "snaps": ["foo","bar"]}]`))
	c.Assert(err, check.IsNil)

	rsp := daemon.CohortsCmd.POST(daemon.CohortsCmd, req, nil)
	c.Check(rsp, check.DeepEquals, &daemon.Resp{
		Status: 500,
		Type:   "error",
		Result: &daemon.ErrorResult{Message: `something went wrong`},
	})
}

func (s *cohortSuite) TestCreateBadBody1(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create", "snaps": ["foo","bar"]`))
	c.Assert(err, check.IsNil)

	rsp := daemon.CohortsCmd.POST(daemon.CohortsCmd, req, nil)
	c.Check(rsp, check.DeepEquals, &daemon.Resp{
		Status: 400,
		Type:   "error",
		Result: &daemon.ErrorResult{Message: `cannot decode request body into cohort instruction: unexpected EOF`},
	})
}

func (s *cohortSuite) TestCreateBadBody2(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create", "snaps": ["foo","bar"]}xx`))
	c.Assert(err, check.IsNil)

	rsp := daemon.CohortsCmd.POST(daemon.CohortsCmd, req, nil)
	c.Check(rsp, check.DeepEquals, &daemon.Resp{
		Status: 400,
		Type:   "error",
		Result: &daemon.ErrorResult{Message: `spurious content after cohort instruction`},
	})
}
