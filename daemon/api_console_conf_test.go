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
	"bytes"
	"net/http"
	"sort"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
)

var _ = Suite(&consoleConfSuite{})

type consoleConfSuite struct {
	apiBaseSuite
}

func (s *consoleConfSuite) TestPostConsoleConfStartRoutine(c *C) {
	t0 := time.Now()
	d := s.daemonWithOverlordMock()
	snapMgr := mylog.Check2(snapstate.Manager(d.Overlord().State(), d.Overlord().TaskRunner()))

	d.Overlord().AddManager(snapMgr)

	st := d.Overlord().State()

	body := bytes.NewBuffer(nil)
	req := mylog.Check2(http.NewRequest("POST", "/v2/internal/console-conf-start", body))


	// no changes in state, no changes in response
	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Result, DeepEquals, &daemon.ConsoleConfStartRoutineResult{})

	// we did set the refresh.hold time back 20 minutes though
	st.Lock()
	defer st.Unlock()

	tr := config.NewTransaction(st)
	var t1 time.Time
	mylog.Check(tr.Get("core", "refresh.hold", &t1))


	c.Assert(t0.Add(20*time.Minute).After(t1), Equals, false)

	// if we add some changes to state that are in progress, then they are
	// returned

	// now make some auto-refresh changes to make sure we get those figured out
	chg0 := st.NewChange("auto-refresh", "auto-refresh-the-things")
	chg0.AddTask(st.NewTask("nop", "do nothing"))

	// make it in doing state
	chg0.SetStatus(state.DoingStatus)
	chg0.Set("snap-names", []string{"doing-snap"})

	// this one will be picked up too
	chg1 := st.NewChange("auto-refresh", "auto-refresh-the-things")
	chg1.AddTask(st.NewTask("nop", "do nothing"))
	chg1.SetStatus(state.DoStatus)
	chg1.Set("snap-names", []string{"do-snap"})

	// this one won't, it's Done
	chg2 := st.NewChange("auto-refresh", "auto-refresh-the-things")
	chg2.AddTask(st.NewTask("nop", "do nothing"))
	chg2.SetStatus(state.DoneStatus)
	chg2.Set("snap-names", []string{"done-snap"})

	// nor this one, it's Undone
	chg3 := st.NewChange("auto-refresh", "auto-refresh-the-things")
	chg3.AddTask(st.NewTask("nop", "do nothing"))
	chg3.SetStatus(state.UndoneStatus)
	chg3.Set("snap-names", []string{"undone-snap"})

	st.Unlock()
	defer st.Lock()

	req2 := mylog.Check2(http.NewRequest("POST", "/v2/internal/console-conf-start", body))

	rsp2 := s.syncReq(c, req2, nil)
	c.Assert(rsp2.Result, FitsTypeOf, &daemon.ConsoleConfStartRoutineResult{})
	res := rsp2.Result.(*daemon.ConsoleConfStartRoutineResult)
	sort.Strings(res.ActiveAutoRefreshChanges)
	sort.Strings(res.ActiveAutoRefreshSnaps)
	expChgs := []string{chg0.ID(), chg1.ID()}
	sort.Strings(expChgs)
	c.Assert(res, DeepEquals, &daemon.ConsoleConfStartRoutineResult{
		ActiveAutoRefreshChanges: expChgs,
		ActiveAutoRefreshSnaps:   []string{"do-snap", "doing-snap"},
	})
}
