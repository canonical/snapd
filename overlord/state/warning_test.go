// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package state_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/state"
)

var never time.Time

func (stateSuite) testMarshalWarning(shown bool, c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.AddWarning("hello")
	now := time.Now()

	expectedNumKeys := 5
	if shown {
		expectedNumKeys++ // last-shown
		st.OkayWarnings(now)
	}

	ws := st.AllWarnings()
	c.Assert(ws, check.HasLen, 1)
	c.Check(ws[0].String(), check.Equals, "hello")
	buf, err := json.Marshal(ws)
	c.Assert(err, check.IsNil)

	var v []map[string]string
	c.Assert(json.Unmarshal(buf, &v), check.IsNil)
	c.Assert(v, check.HasLen, 1)
	c.Check(v[0], check.HasLen, expectedNumKeys)
	c.Check(v[0]["message"], check.DeepEquals, "hello")
	c.Check(v[0]["delete-after"], check.Equals, state.DefaultDeleteAfter.String())
	c.Check(v[0]["repeat-after"], check.Equals, state.DefaultRepeatAfter.String())
	c.Check(v[0]["first-added"], check.Equals, v[0]["last-added"])
	t, err := time.Parse(time.RFC3339, v[0]["first-added"])
	c.Assert(err, check.IsNil)
	dt := t.Sub(now)
	// 'now' was just *after* creating the warning
	c.Check(dt <= 0, check.Equals, true)
	c.Check(-time.Minute < dt, check.Equals, true)
	if shown {
		t, err := time.Parse(time.RFC3339, v[0]["last-shown"])
		c.Assert(err, check.IsNil)
		dt := t.Sub(now)
		// 'now' was just *before* marking the warning as shown
		c.Check(0 <= dt, check.Equals, true)
		c.Check(dt < time.Minute, check.Equals, true)
	}

	var ws2 []*state.Warning
	c.Assert(json.Unmarshal(buf, &ws2), check.IsNil)
	c.Assert(ws2, check.HasLen, 1)
	c.Check(ws2[0], check.DeepEquals, ws[0])
}

func (s stateSuite) TestMarshalWarning(c *check.C) {
	s.testMarshalWarning(false, c)
}

func (s stateSuite) TestMarshalShownWarning(c *check.C) {
	s.testMarshalWarning(true, c)
}

func (stateSuite) TestEmptyStateWarnings(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	ws := st.AllWarnings()
	c.Check(ws, check.HasLen, 0)
}

func (stateSuite) TestOldWarning(c *check.C) {
	oldTime := time.Now().UTC().Add(-2 * state.DefaultDeleteAfter)
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	st.AddWarningFull("hello", oldTime, never, state.DefaultDeleteAfter, state.DefaultRepeatAfter)
	st.AddWarning("hello again")
	now := time.Now()

	allWs := st.AllWarnings()
	c.Assert(allWs, check.HasLen, 2)
	c.Check(fmt.Sprintf("%q", allWs), check.Equals, `["hello" "hello again"]`)
	c.Check(allWs[0].IsDeletable(now), check.Equals, true)
	c.Check(allWs[0].IsShowable(now), check.Equals, true)
	c.Check(allWs[1].IsDeletable(now), check.Equals, false)
	c.Check(allWs[1].IsShowable(now), check.Equals, true)

	c.Check(st.DeleteOldWarnings(), check.Equals, 1)
	c.Check(fmt.Sprintf("%q", st.AllWarnings()), check.Equals, `["hello again"]`)
}

func (stateSuite) TestOldRepeatedWarning(c *check.C) {
	now := time.Now()
	oldTime := now.UTC().Add(-2 * state.DefaultDeleteAfter)
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	st.AddWarningFull("hello", oldTime, never, state.DefaultDeleteAfter, state.DefaultRepeatAfter)
	st.AddWarning("hello")

	allWs := st.AllWarnings()
	c.Assert(allWs, check.HasLen, 1)
	w := allWs[0]
	c.Check(w.IsDeletable(now), check.Equals, false)
	c.Check(w.IsShowable(now), check.Equals, true)
}

func (stateSuite) TestCheckpoint(c *check.C) {
	b := &fakeStateBackend{}
	st := state.New(b)
	st.Lock()
	st.AddWarning("hello")
	st.Unlock()
	c.Assert(b.checkpoints, check.HasLen, 1)

	st2, err := state.ReadState(nil, bytes.NewReader(b.checkpoints[0]))
	c.Assert(err, check.IsNil)
	st2.Lock()
	defer st2.Unlock()
	ws := st2.AllWarnings()
	c.Assert(ws, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws), check.Equals, `["hello"]`)
}

func (stateSuite) TestShowAndOkay(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	st.AddWarning("number one")
	n, _ := st.WarningsSummary()
	c.Check(n, check.Equals, 1)
	ws1, t1 := st.WarningsToShow()
	c.Assert(ws1, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws1), check.Equals, `["number one"]`)

	st.AddWarning("number two")
	ws2, t2 := st.WarningsToShow()
	c.Assert(ws2, check.HasLen, 2)
	c.Check(fmt.Sprintf("%q", ws2), check.Equals, `["number one" "number two"]`)
	c.Assert(t2.After(t1), check.Equals, true)

	n = st.OkayWarnings(t1)
	c.Check(n, check.Equals, 1)

	ws, _ := st.WarningsToShow()
	c.Assert(ws, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws), check.Equals, `["number two"]`)

	n = st.OkayWarnings(t2)
	c.Check(n, check.Equals, 1)

	ws, _ = st.WarningsToShow()
	c.Check(ws, check.HasLen, 0)
}

func (stateSuite) TestShowAndOkayWithRepeats(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	const myRepeatAfter = 2 * time.Second
	t0 := time.Now()
	st.AddWarningFull("hello", t0, never, state.DefaultDeleteAfter, myRepeatAfter)
	ws, t1 := st.WarningsToShow()
	c.Assert(ws, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws), check.Equals, `["hello"]`)

	n := st.OkayWarnings(t1)
	c.Check(n, check.Equals, 1)

	st.AddWarning("hello")

	ws, _ = st.WarningsToShow()
	c.Check(ws, check.HasLen, 0) // not enough time has passed

	time.Sleep(myRepeatAfter)

	ws, _ = st.WarningsToShow()
	c.Check(ws, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws), check.Equals, `["hello"]`)
}
