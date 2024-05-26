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

package daemon_test

import (
	"net/http"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/state"
)

var _ = Suite(&seedingDebugSuite{})

type seedingDebugSuite struct {
	apiBaseSuite
}

func (s *seedingDebugSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)
	s.daemonWithOverlordMock()
}

func (s *seedingDebugSuite) getSeedingDebug(c *C) interface{} {
	req := mylog.Check2(http.NewRequest("GET", "/v2/debug?aspect=seeding", nil))


	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Type, Equals, daemon.ResponseTypeSync)
	return rsp.Result
}

func (s *seedingDebugSuite) TestNoData(c *C) {
	data := s.getSeedingDebug(c)
	c.Check(data, NotNil)
	c.Check(data, DeepEquals, &daemon.SeedingInfo{})
}

func (s *seedingDebugSuite) TestSeedingDebug(c *C) {
	seeded := true
	preseeded := true
	key1 := "foo"
	key2 := "bar"

	preseedStartTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:00Z"))

	preseedTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:01Z"))

	seedRestartTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:03Z"))

	seedTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:07Z"))


	st := s.d.Overlord().State()
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
	c.Check(data, DeepEquals, &daemon.SeedingInfo{
		Seeded:               true,
		Preseeded:            true,
		PreseedSystemKey:     "foo",
		SeedRestartSystemKey: "bar",
		PreseedStartTime:     &preseedStartTime,
		PreseedTime:          &preseedTime,
		SeedRestartTime:      &seedRestartTime,
		SeedTime:             &seedTime,
	})
}

func (s *seedingDebugSuite) TestSeedingDebugSeededNoTimes(c *C) {
	seedTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:07Z"))


	st := s.d.Overlord().State()
	st.Lock()

	// only set seed-time and seeded
	st.Set("seed-time", seedTime)
	st.Set("seeded", true)

	st.Unlock()

	data := s.getSeedingDebug(c)
	c.Check(data, DeepEquals, &daemon.SeedingInfo{
		Seeded:   true,
		SeedTime: &seedTime,
	})
}

func (s *seedingDebugSuite) TestSeedingDebugPreseededStillSeeding(c *C) {
	preseedStartTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:00Z"))

	preseedTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:01Z"))

	seedRestartTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:03Z"))


	st := s.d.Overlord().State()
	st.Lock()

	st.Set("preseeded", true)
	st.Set("seeded", false)

	st.Set("preseed-system-key", "foo")
	st.Set("seed-restart-system-key", "bar")

	st.Set("preseed-start-time", preseedStartTime)
	st.Set("seed-restart-time", seedRestartTime)

	st.Set("preseed-time", preseedTime)

	st.Unlock()

	data := s.getSeedingDebug(c)
	c.Check(data, DeepEquals, &daemon.SeedingInfo{
		Seeded:               false,
		Preseeded:            true,
		PreseedSystemKey:     "foo",
		SeedRestartSystemKey: "bar",
		PreseedStartTime:     &preseedStartTime,
		PreseedTime:          &preseedTime,
		SeedRestartTime:      &seedRestartTime,
	})
}

func (s *seedingDebugSuite) TestSeedingDebugPreseededSeedError(c *C) {
	preseedStartTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:00Z"))

	preseedTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:01Z"))

	seedRestartTime := mylog.Check2(time.Parse(time.RFC3339, "2020-01-01T10:00:03Z"))


	st := s.d.Overlord().State()
	st.Lock()

	st.Set("preseeded", true)
	st.Set("seeded", false)

	st.Set("preseed-system-key", "foo")
	st.Set("seed-restart-system-key", "bar")

	st.Set("preseed-start-time", preseedStartTime)
	st.Set("seed-restart-time", seedRestartTime)

	st.Set("preseed-time", preseedTime)

	chg1 := st.NewChange("seed", "tentative 1")
	t11 := st.NewTask("seed task", "t11")
	t12 := st.NewTask("seed task", "t12")
	chg1.AddTask(t11)
	chg1.AddTask(t12)
	t11.SetStatus(state.UndoneStatus)
	t11.Errorf("t11: undone")
	t12.SetStatus(state.ErrorStatus)
	t12.Errorf("t12: fail")

	// ensure different spawn time
	time.Sleep(50 * time.Millisecond)
	chg2 := st.NewChange("seed", "tentative 2")
	t21 := st.NewTask("seed task", "t21")
	chg2.AddTask(t21)
	t21.SetStatus(state.ErrorStatus)
	t21.Errorf("t21: error")

	chg3 := st.NewChange("seed", "tentative 3")
	t31 := st.NewTask("seed task", "t31")
	chg3.AddTask(t31)
	t31.SetStatus(state.DoingStatus)

	st.Unlock()

	data := s.getSeedingDebug(c)
	c.Check(data, DeepEquals, &daemon.SeedingInfo{
		Seeded:               false,
		Preseeded:            true,
		PreseedSystemKey:     "foo",
		SeedRestartSystemKey: "bar",
		PreseedStartTime:     &preseedStartTime,
		PreseedTime:          &preseedTime,
		SeedRestartTime:      &seedRestartTime,
		SeedError: `cannot perform the following tasks:
- t12 (t12: fail)`,
	})
}
