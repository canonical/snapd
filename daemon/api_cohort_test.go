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

	"github.com/ddkwork/golibrary/mylog"
	"gopkg.in/check.v1"
)

var _ = check.Suite(&cohortSuite{})

type cohortSuite struct {
	apiBaseSuite

	snaps []string
	coh   map[string]string
}

func (s *cohortSuite) CreateCohorts(_ context.Context, snaps []string) (map[string]string, error) {
	s.pokeStateLock()

	s.snaps = snaps[:]
	return s.coh, s.err
}

func (s *cohortSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.snaps = nil
	s.coh = nil

	s.daemonWithStore(c, s)
}

func (s *cohortSuite) TestCreateCohort(c *check.C) {
	s.coh = map[string]string{
		"foo": "cohort for foo",
		"bar": "cohort for bar",
	}

	req := mylog.Check2(http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create", "snaps": ["foo","bar"]}]`)))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, s.coh)
}

func (s *cohortSuite) TestCreateCohortNoSnaps(c *check.C) {
	req := mylog.Check2(http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create"}]`)))
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.DeepEquals, map[string]string{})
}

func (s *cohortSuite) TestCreateCohortBadAction(c *check.C) {
	req := mylog.Check2(http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "pupate", "snaps": ["foo","bar"]}]`)))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `unknown cohort action "pupate"`)
}

func (s *cohortSuite) TestCreateCohortError(c *check.C) {
	s.err = errors.New("something went wrong")

	req := mylog.Check2(http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create", "snaps": ["foo","bar"]}]`)))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 500)
	c.Check(rspe.Message, check.Equals, `something went wrong`)
}

func (s *cohortSuite) TestCreateBadBody1(c *check.C) {
	req := mylog.Check2(http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create", "snaps": ["foo","bar"]`)))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `cannot decode request body into cohort instruction: unexpected EOF`)
}

func (s *cohortSuite) TestCreateBadBody2(c *check.C) {
	req := mylog.Check2(http.NewRequest("POST", "/v2/cohorts", strings.NewReader(`{"action": "create", "snaps": ["foo","bar"]}xx`)))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `spurious content after cohort instruction`)
}
