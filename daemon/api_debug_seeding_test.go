// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"net/http"
	"time"

	. "gopkg.in/check.v1"
)

var _ = Suite(&seedingDebugSuite{})

type seedingDebugSuite struct {
	apiBaseSuite
}

func (s *seedingDebugSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)
	s.daemonWithOverlordMock(c)
}

func (s *seedingDebugSuite) getSeedingDebug(c *C) interface{} {
	req, err := http.NewRequest("GET", "/v2/debug?aspect=seeding", nil)
	c.Assert(err, IsNil)

	rsp := getDebug(debugCmd, req, nil).(*resp)
	c.Assert(rsp.Type, Equals, ResponseTypeSync)
	return rsp.Result
}

func (s *seedingDebugSuite) TestNoData(c *C) {
	data := s.getSeedingDebug(c)
	c.Check(data, NotNil)
	c.Check(data, DeepEquals, &seedingInfo{})
}

func (s *seedingDebugSuite) TestSeedingDebug(c *C) {
	seeded := true
	preseeded := true
	key1 := "foo"
	key2 := "bar"

	preseedStartTime, err := time.Parse(time.RFC3339, "2020-01-01T10:00:00Z")
	c.Assert(err, IsNil)
	preseedTime, err := time.Parse(time.RFC3339, "2020-01-01T10:00:01Z")
	c.Assert(err, IsNil)
	seedRestartTime, err := time.Parse(time.RFC3339, "2020-01-01T10:00:03Z")
	c.Assert(err, IsNil)
	seedTime, err := time.Parse(time.RFC3339, "2020-01-01T10:00:07Z")
	c.Assert(err, IsNil)

	st := s.d.overlord.State()
	st.Lock()

	st.Set("preseeded", preseeded)
	st.Set("seeded", seeded)

	st.Set("preseed-system-key", key1)
	st.Set("seed-restart-system-key", key2)

	st.Set("preseed-start-time", preseedStartTime)
	st.Set("seed-restart-time", seedRestartTime)

	st.Set("preseed-time", preseedTime)
	st.Set("seed-time", seedTime)

	st.Unlock()

	data := s.getSeedingDebug(c)
	c.Check(data, DeepEquals, &seedingInfo{
		Seeded:               true,
		Preseeded:            true,
		PreseedSystemKey:     "foo",
		SeedRestartSystemKey: "bar",
		PreseedStartTime:     preseedStartTime,
		PreseedTime:          preseedTime,
		SeedRestartTime:      seedRestartTime,
		SeedTime:             seedTime,
	})
}
