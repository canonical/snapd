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
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

func (stateSuite) testMarshalWarning(shown bool, c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.Warnf("hello")
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
	c.Check(v[0]["expire-after"], check.Equals, state.DefaultWarningExpireAfter.String())
	c.Check(v[0]["repeat-after"], check.Equals, state.DefaultWarningShowAfter.String())
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
}

func (s stateSuite) TestMarshalWarning(c *check.C) {
	s.testMarshalWarning(false, c)
}

func (s stateSuite) TestMarshalShownWarning(c *check.C) {
	s.testMarshalWarning(true, c)
}

func (s stateSuite) TestAddWarningWithBadMessages(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	c.Check(func() { st.Warnf("") }, check.PanicMatches,
		`internal error: cannot add warning notice with invalid key ""`,
	)

	c.Check(func() { st.Warnf(" ") }, check.PanicMatches,
		`internal error: cannot add warning notice with invalid key " "`,
	)
}

func (stateSuite) TestEmptyStateWarnings(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	ws := st.AllWarnings()
	c.Check(ws, check.HasLen, 0)
}

func (stateSuite) TestDeleteExpired(c *check.C) {
	const dt = 20 * time.Millisecond
	oldTime := time.Now().Add(dt - state.DefaultWarningExpireAfter)
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	st.Warnf("hello again") // adding this twice to trigger the swap in sort
	restore := state.MockNextNoticeTimestamp(oldTime)
	st.Warnf("hello")
	restore()
	st.Warnf("hello again")

	allWs := st.AllWarnings()
	c.Assert(allWs, check.HasLen, 2)

	time.Sleep(2 * dt)
	now := time.Now()

	c.Assert(allWs, check.HasLen, 2)
	c.Check(fmt.Sprintf("%q", allWs), check.Equals, `["hello" "hello again"]`)
	c.Check(allWs[0].Expired(now), check.Equals, true)
	c.Check(allWs[0].ShowAfter(now), check.Equals, true)
	c.Check(allWs[1].Expired(now), check.Equals, false)
	c.Check(allWs[1].ShowAfter(now), check.Equals, true)

	allWs = st.AllWarnings()
	c.Check(allWs, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", allWs), check.Equals, `["hello again"]`)
}

func (stateSuite) TestExpiredWarningLastShownNotPreserved(c *check.C) {
	restore := state.MockDefaultWarningExpireAfter(10 * time.Millisecond)
	defer restore()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	st.Warnf("hello")
	now := time.Now()
	st.OkayWarnings(now)

	allWs := st.AllWarnings()
	c.Assert(allWs, check.HasLen, 1)
	w := allWs[0]
	c.Check(w.Expired(now), check.Equals, false)

	lastShown1, err := w.LastShown()
	c.Check(err, check.IsNil)
	c.Check(lastShown1, check.Equals, now.UTC())
	time.Sleep(testutil.HostScaledTimeout(30 * time.Millisecond))
	now = time.Now()
	c.Check(w.Expired(now), check.Equals, true)

	st.Warnf("hello")
	allWs = st.AllWarnings()
	c.Assert(allWs, check.HasLen, 1)
	w = allWs[0]
	now = time.Now()
	c.Check(w.Expired(now), check.Equals, false)
	lastShown2, err := w.LastShown()
	c.Check(err, check.IsNil)
	c.Check(lastShown2, check.Not(check.Equals), lastShown1)
	var t time.Time
	c.Check(lastShown2, check.Equals, t)
}

func (stateSuite) TestOldRepeatedWarning(c *check.C) {
	now := time.Now()
	oldTime := now.UTC().Add(-2 * state.DefaultWarningExpireAfter)
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	restore := state.MockNextNoticeTimestamp(oldTime)
	st.Warnf("hello")
	restore()
	st.Warnf("hello")

	allWs := st.AllWarnings()
	c.Assert(allWs, check.HasLen, 1)
	w := allWs[0]
	c.Check(w.Expired(now), check.Equals, false)
	// firstAdded for the latest warning is not oldTime since
	// expired notices are not repeated.
	c.Check(w.ShowAfter(now), check.Equals, false)
	c.Check(w.BackingNotice().NoticeFirstOccurred().After(oldTime), check.Equals, true)
}

func (stateSuite) TestCheckpoint(c *check.C) {
	b := &fakeStateBackend{}
	st := state.New(b)
	st.Lock()
	st.Warnf("hello")
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

func (stateSuite) TestWarningsSummaryReturnsLastLastAdded(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	t0 := time.Now().Add(-100 * time.Hour)
	restore := state.MockNextNoticeTimestamp(t0)
	st.Warnf("hello")
	restore()
	n, t := st.WarningsSummary()
	c.Check(n, check.Equals, 1)
	c.Check(t.Equal(t0), check.Equals, true)
}

func (stateSuite) TestShowAndOkay(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	st.Warnf("number one")
	n, _ := st.WarningsSummary()
	c.Check(n, check.Equals, 1)
	ws1, t1 := st.PendingWarnings()
	c.Assert(ws1, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws1), check.Equals, `["number one"]`)

	st.Warnf("number two")
	ws2, t2 := st.PendingWarnings()
	c.Assert(ws2, check.HasLen, 2)
	c.Check(fmt.Sprintf("%q", ws2), check.Equals, `["number one" "number two"]`)
	c.Assert(t2.After(t1), check.Equals, true)

	n = st.OkayWarnings(t1)
	c.Check(n, check.Equals, 1)

	ws, _ := st.PendingWarnings()
	c.Assert(ws, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws), check.Equals, `["number two"]`)

	n = st.OkayWarnings(t2)
	c.Check(n, check.Equals, 1)

	ws, _ = st.PendingWarnings()
	c.Check(ws, check.HasLen, 0)

	st.UnshowAllWarnings()
	ws, _ = st.PendingWarnings()
	c.Check(ws, check.HasLen, 2)
}

func (stateSuite) TestShowAndOkayWithRepeats(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()
	const myShowAfter = 2 * time.Second
	st.Warnf("hello")
	ws, t1 := st.PendingWarnings()
	c.Assert(ws, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws), check.Equals, `["hello"]`)

	n := st.OkayWarnings(t1)
	c.Check(n, check.Equals, 1)

	st.AddWarning("hello", &state.AddWarningOptions{ShowAfter: myShowAfter})

	ws, _ = st.PendingWarnings()
	c.Check(ws, check.HasLen, 0) // not enough time has passed

	time.Sleep(myShowAfter)

	ws, _ = st.PendingWarnings()
	c.Check(ws, check.HasLen, 1)
	c.Check(fmt.Sprintf("%q", ws), check.Equals, `["hello"]`)
}

func (stateSuite) TestRemoveWarning(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	// cannot replace a non existing warning
	err := st.RemoveWarning("this warning does not exist")
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
	ws := st.AllWarnings()
	c.Check(ws, check.HasLen, 0)

	st.Warnf("this warning exists")
	ws = st.AllWarnings()
	c.Check(ws, check.HasLen, 1)
	c.Check(ws[0].String(), check.Equals, "this warning exists")

	// check warning is removed
	err = st.RemoveWarning("this warning exists")
	c.Assert(err, check.IsNil)
	ws = st.AllWarnings()
	c.Check(ws, check.HasLen, 0)
}

func (stateSuite) TestWarningsReadWithoutStateLock(c *check.C) {
	st := state.New(nil)

	ws := st.AllWarnings()
	c.Check(ws, check.HasLen, 0)
	ws, ts := st.PendingWarnings()
	c.Check(ws, check.HasLen, 0)
	c.Check(ts.IsZero(), check.Equals, false)
	count, ts := st.WarningsSummary()
	c.Check(count, check.Equals, 0)
	c.Check(ts.IsZero(), check.Equals, true)

	st.Lock()
	st.Warnf("this warning exists")
	st.Unlock()

	ws = st.AllWarnings()
	c.Check(ws, check.HasLen, 1)
	ws, ts = st.PendingWarnings()
	c.Check(ws, check.HasLen, 1)
	c.Check(ts.IsZero(), check.Equals, false)
	count, ts = st.WarningsSummary()
	c.Check(count, check.Equals, 1)
	c.Check(ts.IsZero(), check.Equals, false)
}

func (stateSuite) TestAddWarningBackingNotice(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	const showAfter = 2 * time.Hour
	st.AddWarning("hello", &state.AddWarningOptions{ShowAfter: showAfter})

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 1)
	n := notices[0]
	c.Check(n.Type(), check.Equals, state.WarningNotice)
	c.Check(n.Key(), check.Equals, "hello")
	c.Check(n.LastData()["show-after"], check.Equals, showAfter.String())
	_, ok := n.LastData()["last-shown"]
	c.Check(ok, check.Equals, false)
	m := noticeToMap(c, n)
	c.Check(m["expire-after"], check.Equals, state.DefaultWarningExpireAfter.String())
	c.Check(m["occurrences"], check.Equals, 1.0)

	id := n.ID()
	st.AddWarning("hello", &state.AddWarningOptions{ShowAfter: showAfter})
	notices = st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 1)
	n = notices[0]
	c.Check(n.ID(), check.Equals, id)
	c.Check(n.LastData()["show-after"], check.Equals, showAfter.String())
	_, ok = n.LastData()["last-shown"]
	c.Check(ok, check.Equals, false)
	c.Check(noticeToMap(c, n)["occurrences"], check.Equals, 2.0)

	st.AddWarning("no-show-after", nil)
	notices = st.Notices(&state.NoticeFilter{
		Types: []state.NoticeType{state.WarningNotice},
		Keys:  []string{"no-show-after"},
	})
	c.Assert(notices, check.HasLen, 1)
	c.Check(notices[0].LastData(), check.IsNil)
}

func (stateSuite) TestWarningsAPIsIgnoreNonWarningNotices(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	addNotice(c, st, nil, state.ChangeUpdateNotice, "change", nil)
	addNotice(c, st, nil, state.RefreshInhibitNotice, "-", nil)
	addNotice(c, st, nil, state.InterfacesRequestsPromptNotice, "prompt", nil)
	st.Warnf("warning")

	ws := st.AllWarnings()
	c.Assert(ws, check.HasLen, 1)
	c.Check(ws[0].String(), check.Equals, "warning")

	pending, _ := st.PendingWarnings()
	c.Assert(pending, check.HasLen, 1)
	c.Check(pending[0].String(), check.Equals, "warning")

	n, _ := st.WarningsSummary()
	c.Check(n, check.Equals, 1)
}

func (stateSuite) TestWarningFilterCutoffTime(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	fixedTime := time.Now()
	restore := state.MockTime(fixedTime)
	defer restore()
	st.Warnf("one")
	st.Warnf("two")
	n, _ := st.WarningsSummary()
	// this is 1 when using only the wall clock without
	// accounting for possible lastNoticeTimestamps later than
	// the wall clock
	c.Check(n, check.Equals, 2)
}

func (stateSuite) TestOkayWarningsUpdatesBackingNoticeData(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	const showAfter = 2 * time.Hour
	st.AddWarning("hello", &state.AddWarningOptions{ShowAfter: showAfter})
	oldWs := st.AllWarnings()
	c.Assert(oldWs, check.HasLen, 1)
	oldW := oldWs[0]
	oldLastShown, err := oldW.LastShown()
	c.Check(err, check.IsNil)
	c.Check(oldLastShown.IsZero(), check.Equals, true)

	addNotice(c, st, nil, state.ChangeUpdateNotice, "change", &state.AddNoticeOptions{
		Data: map[string]string{"k": "v"},
	})

	_, t := st.PendingWarnings()
	c.Check(st.OkayWarnings(t), check.Equals, 1)

	ws := st.AllWarnings()
	c.Assert(ws, check.HasLen, 1)
	lastShown, err := ws[0].LastShown()
	c.Check(err, check.IsNil)
	c.Check(lastShown, check.Equals, t.UTC())
	c.Check(ws[0].BackingNotice().LastData()["show-after"], check.Equals, showAfter.String())

	// OkayWarnings replaces the backing notice rather than mutating the old one.
	oldLastShown, err = oldW.LastShown()
	c.Check(err, check.IsNil)
	c.Check(oldLastShown.IsZero(), check.Equals, true)

	changes := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}})
	c.Assert(changes, check.HasLen, 1)
	c.Check(changes[0].LastData(), check.DeepEquals, map[string]string{"k": "v"})
}

func (stateSuite) TestUnshowAllWarningsPreservesOtherData(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	const showAfter = 2 * time.Hour
	st.AddWarning("hello", &state.AddWarningOptions{ShowAfter: showAfter})
	addNotice(c, st, nil, state.ChangeUpdateNotice, "change", &state.AddNoticeOptions{
		Data: map[string]string{"k": "v"},
	})

	_, t := st.PendingWarnings()
	c.Assert(st.OkayWarnings(t), check.Equals, 1)
	ws := st.AllWarnings()
	c.Assert(ws, check.HasLen, 1)
	_, err := ws[0].LastShown()
	c.Check(err, check.IsNil)

	st.UnshowAllWarnings()

	ws = st.AllWarnings()
	c.Assert(ws, check.HasLen, 1)
	lastShown, err := ws[0].LastShown()
	c.Check(err, check.IsNil)
	c.Check(lastShown.IsZero(), check.Equals, true)
	c.Check(ws[0].BackingNotice().LastData()["show-after"], check.Equals, showAfter.String())
	_, ok := ws[0].BackingNotice().LastData()["last-shown"]
	c.Check(ok, check.Equals, false)

	changes := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}})
	c.Assert(changes, check.HasLen, 1)
	c.Check(changes[0].LastData(), check.DeepEquals, map[string]string{"k": "v"})
}

func (stateSuite) TestRemoveWarningIsTypeSpecific(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	st.Warnf("same-key")
	addNotice(c, st, nil, state.ChangeUpdateNotice, "same-key", nil)

	c.Assert(st.RemoveWarning("same-key"), check.IsNil)
	c.Check(st.AllWarnings(), check.HasLen, 0)

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.ChangeUpdateNotice}})
	c.Assert(notices, check.HasLen, 1)
	c.Check(notices[0].Key(), check.Equals, "same-key")

	c.Assert(st.RemoveWarning("same-key"), testutil.ErrorIs, state.ErrNoState)
}

func (stateSuite) TestLongWarningMessageAllowed(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	message := strings.Repeat("a", 300)
	st.Warnf(message)

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 1)
	c.Check(notices[0].Key(), check.Equals, message)

	_, err := st.AddNotice(nil, state.ChangeUpdateNotice, message, nil)
	c.Assert(err, check.ErrorMatches, `internal error: cannot add change-update notice with invalid key: key must be 256 bytes or less`)
}

func (stateSuite) TestPendingWarningsAndSummaryIgnoreExpired(c *check.C) {
	restoreExpire := state.MockDefaultWarningExpireAfter(10 * time.Millisecond)
	defer restoreExpire()

	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t0 := time.Now().UTC()
	restoreTime := state.MockTime(t0)
	st.Warnf("expired")
	restoreTime()

	t1 := t0.Add(20 * time.Millisecond)
	restoreTime = state.MockTime(t1)
	defer restoreTime()

	c.Check(st.AllWarnings(), check.HasLen, 0)
	pending, _ := st.PendingWarnings()
	c.Check(pending, check.HasLen, 0)
	n, _ := st.WarningsSummary()
	c.Check(n, check.Equals, 0)
}

func (stateSuite) TestWarningNoticeCheckpointData(c *check.C) {
	b := &fakeStateBackend{}
	st := state.New(b)
	st.Lock()
	const showAfter = 2 * time.Hour
	st.AddWarning("hello", &state.AddWarningOptions{ShowAfter: showAfter})
	_, t := st.PendingWarnings()
	c.Assert(st.OkayWarnings(t), check.Equals, 1)
	st.Unlock()
	c.Assert(b.checkpoints, check.HasLen, 1)

	st2, err := state.ReadState(nil, bytes.NewReader(b.checkpoints[0]))
	c.Assert(err, check.IsNil)
	st2.Lock()
	defer st2.Unlock()

	notices := st2.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 1)
	n := notices[0]
	c.Check(n.Type(), check.Equals, state.WarningNotice)
	c.Check(n.Key(), check.Equals, "hello")
	c.Check(n.LastData()["show-after"], check.Equals, showAfter.String())
	lastShown, err := time.Parse(time.RFC3339Nano, n.LastData()["last-shown"])
	c.Assert(err, check.IsNil)
	c.Check(lastShown.Equal(t.UTC()), check.Equals, true)
	c.Check(noticeToMap(c, n)["expire-after"], check.Equals, state.DefaultWarningExpireAfter.String())
}

func (stateSuite) TestWarningsSummaryMultipleShowable(c *check.C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	t0 := time.Now().Add(-time.Hour).UTC()
	restore := state.MockNextNoticeTimestamp(t0)
	st.Warnf("old")
	restore()

	t1 := time.Now().UTC()
	restore = state.MockNextNoticeTimestamp(t1)
	st.Warnf("new")
	restore()

	n, last := st.WarningsSummary()
	c.Check(n, check.Equals, 2)
	c.Check(last.Equal(t1), check.Equals, true)
}

type oldWarningJSON struct {
	Message     string     `json:"message"`
	FirstAdded  time.Time  `json:"first-added"`
	LastAdded   time.Time  `json:"last-added"`
	LastShown   *time.Time `json:"last-shown,omitempty"`
	ExpireAfter string     `json:"expire-after,omitempty"`
	RepeatAfter string     `json:"repeat-after,omitempty"`
}

func migrateWarningsState(c *check.C, warnings []oldWarningJSON, notices []any, lastNoticeID int, lastNoticeTimestamp time.Time) []byte {
	stateJSON, err := json.Marshal(map[string]any{
		"data":                  map[string]any{},
		"changes":               map[string]any{},
		"tasks":                 map[string]any{},
		"warnings":              warnings,
		"notices":               notices,
		"last-change-id":        0,
		"last-task-id":          0,
		"last-lane-id":          0,
		"last-notice-id":        lastNoticeID,
		"last-notice-timestamp": lastNoticeTimestamp,
	})
	c.Assert(err, check.IsNil)
	return stateJSON
}

type migrateWarningCase struct {
	message     string
	firstAdded  time.Time
	lastAdded   time.Time
	lastShown   *time.Time
	expireAfter string
	repeatAfter string

	skipped bool
	skipLog string

	wantExpireAfter string
	wantShowAfter   string
}

func warningsFromMigrateCases(cases []migrateWarningCase) []oldWarningJSON {
	warnings := make([]oldWarningJSON, len(cases))
	for i, tc := range cases {
		warnings[i] = oldWarningJSON{
			Message:     tc.message,
			FirstAdded:  tc.firstAdded,
			LastAdded:   tc.lastAdded,
			LastShown:   tc.lastShown,
			ExpireAfter: tc.expireAfter,
			RepeatAfter: tc.repeatAfter,
		}
	}
	return warnings
}

func (stateSuite) TestMigrateWarnings(c *check.C) {
	now := time.Now().UTC().Truncate(time.Second)
	restore := state.MockTime(now)
	defer restore()

	logBuf, restoreLog := logger.MockLogger()
	defer restoreLog()

	firstAdded := now.Add(-10 * time.Minute)
	lastAdded := now.Add(-5 * time.Minute)
	lastShown := now.Add(-2 * time.Minute)
	customExpireAfter := 48 * time.Hour
	customShowAfter := 2 * time.Hour
	defaultExpireAfter := state.DefaultWarningExpireAfter.String()
	defaultShowAfter := state.DefaultWarningShowAfter.String()

	cases := []migrateWarningCase{
		{
			message:         "first test warning",
			firstAdded:      firstAdded,
			lastAdded:       lastAdded,
			lastShown:       &lastShown,
			expireAfter:     defaultExpireAfter,
			repeatAfter:     defaultShowAfter,
			wantExpireAfter: defaultExpireAfter,
			wantShowAfter:   defaultShowAfter,
		},
		{
			message:         "custom expiry warning",
			firstAdded:      firstAdded,
			lastAdded:       lastAdded.Add(time.Second),
			lastShown:       &lastShown,
			expireAfter:     customExpireAfter.String(),
			repeatAfter:     customShowAfter.String(),
			wantExpireAfter: customExpireAfter.String(),
			wantShowAfter:   customShowAfter.String(),
		},
		{
			message:         "missing repeat-after",
			firstAdded:      firstAdded,
			lastAdded:       lastAdded.Add(2 * time.Second),
			expireAfter:     defaultExpireAfter,
			wantExpireAfter: defaultExpireAfter,
			wantShowAfter:   defaultShowAfter,
		},
		{
			message:         "missing expire-after",
			firstAdded:      firstAdded,
			lastAdded:       lastAdded.Add(3 * time.Second),
			lastShown:       &lastShown,
			repeatAfter:     defaultShowAfter,
			wantExpireAfter: defaultExpireAfter,
			wantShowAfter:   defaultShowAfter,
		},
		{
			message:         "invalid expire-after",
			firstAdded:      firstAdded,
			lastAdded:       lastAdded.Add(4 * time.Second),
			expireAfter:     "24d",
			repeatAfter:     defaultShowAfter,
			wantExpireAfter: defaultExpireAfter,
			wantShowAfter:   defaultShowAfter,
		},
		{
			message:         "invalid repeat-after",
			firstAdded:      firstAdded,
			lastAdded:       lastAdded.Add(5 * time.Second),
			expireAfter:     defaultExpireAfter,
			repeatAfter:     "24d",
			wantExpireAfter: defaultExpireAfter,
			wantShowAfter:   defaultShowAfter,
		},
		{
			message:         "zero repeat-after",
			firstAdded:      firstAdded,
			lastAdded:       lastAdded.Add(6 * time.Second),
			expireAfter:     defaultExpireAfter,
			repeatAfter:     "0s",
			wantExpireAfter: defaultExpireAfter,
		},
	}

	st, err := state.ReadState(nil, bytes.NewReader(migrateWarningsState(c, warningsFromMigrateCases(cases), []any{}, 0, time.Time{})))
	c.Assert(err, check.IsNil)
	st.Lock()
	defer st.Unlock()

	logs := logBuf.String()
	c.Check(strings.Count(logs, "warning skipped in migration:"), check.Equals, 0)

	expected := make(map[string]migrateWarningCase)
	expectedIDs := make(map[string]string)
	nextID := 1
	var lastNoticeTimestamp time.Time
	for _, tc := range cases {
		expected[tc.message] = tc
		expectedIDs[tc.message] = fmt.Sprintf("%d", nextID)
		nextID++
		if tc.lastAdded.After(lastNoticeTimestamp) {
			lastNoticeTimestamp = tc.lastAdded
		}
	}

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, len(expected))
	c.Check(st.NumNotices(), check.Equals, len(expected))

	for _, notice := range notices {
		exp, ok := expected[notice.Key()]
		c.Assert(ok, check.Equals, true, check.Commentf("unexpected warning %q", notice.Key()))
		c.Check(notice.ID(), check.Equals, expectedIDs[notice.Key()])
		c.Check(notice.Type(), check.Equals, state.WarningNotice)
		c.Check(notice.NoticeFirstOccurred().Equal(exp.firstAdded), check.Equals, true)
		c.Check(notice.NoticeLastOccurred().Equal(exp.lastAdded), check.Equals, true)
		c.Check(notice.LastRepeated().Equal(exp.lastAdded), check.Equals, true)
		c.Check(noticeToMap(c, notice)["expire-after"], check.Equals, exp.wantExpireAfter)

		if exp.wantShowAfter == "" {
			_, ok := notice.LastData()["show-after"]
			c.Check(ok, check.Equals, false)
		} else {
			c.Check(notice.LastData()["show-after"], check.Equals, exp.wantShowAfter)
		}

		if exp.lastShown == nil {
			_, ok := notice.LastData()["last-shown"]
			c.Check(ok, check.Equals, false)
		} else {
			c.Check(notice.LastData()["last-shown"], check.Equals, exp.lastShown.Format(time.RFC3339Nano))
		}
	}

	c.Check(st.LastNoticeTimestamp().Equal(lastNoticeTimestamp), check.Equals, true)
}

func (stateSuite) TestMigrateWarningsSkips(c *check.C) {
	now := time.Now().UTC().Truncate(time.Second)
	restore := state.MockTime(now)
	defer restore()

	logBuf, restoreLog := logger.MockLogger()
	defer restoreLog()

	firstAdded := now.Add(-10 * time.Minute)
	lastAdded := now.Add(-5 * time.Minute)
	defaultExpireAfter := state.DefaultWarningExpireAfter.String()
	defaultShowAfter := state.DefaultWarningShowAfter.String()

	cases := []migrateWarningCase{
		{
			message:     " ",
			firstAdded:  firstAdded,
			lastAdded:   lastAdded,
			expireAfter: defaultExpireAfter,
			repeatAfter: defaultShowAfter,
			skipped:     true,
			skipLog:     `warning skipped in migration: cannot add warning notice with invalid key " "`,
		},
		{
			message:     "",
			firstAdded:  firstAdded,
			lastAdded:   lastAdded,
			expireAfter: defaultExpireAfter,
			repeatAfter: defaultShowAfter,
			skipped:     true,
			skipLog:     `warning skipped in migration: cannot add warning notice with invalid key ""`,
		},
		{
			message:     "no first-added",
			lastAdded:   lastAdded,
			expireAfter: defaultExpireAfter,
			repeatAfter: defaultShowAfter,
			skipped:     true,
			skipLog:     "warning skipped in migration: empty first-added",
		},
		{
			message:     "no last-added",
			firstAdded:  firstAdded,
			expireAfter: defaultExpireAfter,
			repeatAfter: defaultShowAfter,
			skipped:     true,
			skipLog:     "warning skipped in migration: empty last-added",
		},
		{
			message:     "expired warning",
			firstAdded:  firstAdded,
			lastAdded:   now.Add(-2 * time.Hour),
			expireAfter: time.Hour.String(),
			repeatAfter: defaultShowAfter,
			skipped:     true,
		},
	}

	st, err := state.ReadState(nil, bytes.NewReader(migrateWarningsState(c, warningsFromMigrateCases(cases), []any{}, 0, time.Time{})))
	c.Assert(err, check.IsNil)
	st.Lock()
	defer st.Unlock()

	logs := logBuf.String()
	skipLogCount := 0
	for _, tc := range cases {
		if tc.skipLog != "" {
			skipLogCount++
			c.Check(strings.Contains(logs, tc.skipLog), check.Equals, true, check.Commentf("missing log %q", tc.skipLog))
		}
	}
	c.Check(strings.Count(logs, "warning skipped in migration:"), check.Equals, skipLogCount)

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Check(notices, check.HasLen, 0)
	c.Check(st.NumNotices(), check.Equals, 0)
}

func (stateSuite) TestMigrateWarningsUniqueLastRepeated(c *check.C) {
	now := time.Now().UTC().Truncate(time.Second)
	restore := state.MockTime(now)
	defer restore()

	firstAdded := now.Add(-10 * time.Minute)
	lastAdded := now.Add(-3 * time.Minute)
	defaultExpireAfter := state.DefaultWarningExpireAfter.String()

	warnings := []oldWarningJSON{
		{
			Message:     "warning one",
			FirstAdded:  firstAdded,
			LastAdded:   lastAdded,
			ExpireAfter: defaultExpireAfter,
		},
		{
			Message:     "warning two",
			FirstAdded:  firstAdded.Add(time.Minute),
			LastAdded:   lastAdded,
			ExpireAfter: defaultExpireAfter,
		},
	}

	st, err := state.ReadState(nil, bytes.NewReader(migrateWarningsState(c, warnings, []any{}, 0, time.Time{})))
	c.Assert(err, check.IsNil)
	st.Lock()
	defer st.Unlock()

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 2)
	c.Check(notices[0].LastRepeated().Equal(lastAdded), check.Equals, true)
	c.Check(notices[1].LastRepeated().Equal(lastAdded.Add(time.Nanosecond)), check.Equals, true)
	c.Check(st.LastNoticeTimestamp().Equal(notices[1].LastRepeated()), check.Equals, true)

	after := notices[0].LastRepeated()
	got := st.Notices(&state.NoticeFilter{After: after})
	c.Assert(got, check.HasLen, 1)
	c.Check(got[0].Key(), check.Equals, "warning two")
}

func (stateSuite) TestMigrateWarningsUniqueLastRepeatedWithExistingNotice(c *check.C) {
	now := time.Now().UTC().Truncate(time.Second)
	restore := state.MockTime(now)
	defer restore()

	timestamp := now.Add(-3 * time.Minute)
	existingNotice := map[string]any{
		"id":             "1",
		"type":           string(state.ChangeUpdateNotice),
		"key":            "existing notice",
		"first-occurred": timestamp,
		"last-occurred":  timestamp,
		"last-repeated":  timestamp,
		"occurrences":    1,
		"expire-after":   state.DefaultWarningExpireAfter.String(),
	}
	warnings := []oldWarningJSON{{
		Message:     "warning",
		FirstAdded:  now.Add(-10 * time.Minute),
		LastAdded:   timestamp,
		ExpireAfter: state.DefaultWarningExpireAfter.String(),
	}}

	// Deliberately use a stale last-notice-timestamp to ensure migration scans
	// existing notices rather than trusting the saved timestamp.
	st, err := state.ReadState(nil, bytes.NewReader(migrateWarningsState(c, warnings, []any{existingNotice}, 1, timestamp.Add(-time.Hour))))
	c.Assert(err, check.IsNil)
	st.Lock()
	defer st.Unlock()

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 1)
	c.Check(notices[0].LastRepeated().Equal(timestamp.Add(time.Nanosecond)), check.Equals, true)

	got := st.Notices(&state.NoticeFilter{After: timestamp})
	c.Assert(got, check.HasLen, 1)
	c.Check(got[0].Type(), check.Equals, state.WarningNotice)
	c.Check(got[0].Key(), check.Equals, "warning")
}

func (stateSuite) TestMigrateWarningsExistingNotice(c *check.C) {
	now := time.Now().UTC().Truncate(time.Second)
	restore := state.MockTime(now)
	defer restore()

	noticeFirst := now.Add(-30 * time.Minute)
	noticeLast := now.Add(-20 * time.Minute)
	noticeShown := now.Add(-15 * time.Minute)
	warningFirst := now.Add(-10 * time.Minute)
	warningLast := now.Add(-5 * time.Minute)
	warningShown := now.Add(-1 * time.Minute)
	defaultExpireAfter := state.DefaultWarningExpireAfter.String()

	existingNotice := map[string]any{
		"id":             "1",
		"type":           string(state.WarningNotice),
		"key":            "same warning",
		"first-occurred": noticeFirst,
		"last-occurred":  noticeLast,
		"last-repeated":  noticeLast,
		"occurrences":    1,
		"expire-after":   defaultExpireAfter,
		"last-data": map[string]string{
			"show-after": "1h0m0s",
			"last-shown": noticeShown.Format(time.RFC3339Nano),
		},
	}
	warnings := []oldWarningJSON{{
		Message:     "same warning",
		FirstAdded:  warningFirst,
		LastAdded:   warningLast,
		ExpireAfter: defaultExpireAfter,
		RepeatAfter: "2h0m0s",
		LastShown:   &warningShown,
	}}

	st, err := state.ReadState(nil, bytes.NewReader(migrateWarningsState(c, warnings, []any{existingNotice}, 1, noticeLast)))
	c.Assert(err, check.IsNil)
	st.Lock()
	defer st.Unlock()

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 1)
	notice := notices[0]
	c.Check(notice.ID(), check.Equals, "1")
	c.Check(notice.Key(), check.Equals, "same warning")
	c.Check(notice.NoticeFirstOccurred().Equal(noticeFirst), check.Equals, true)
	c.Check(notice.NoticeLastOccurred().Equal(warningLast), check.Equals, true)
	c.Check(notice.LastRepeated().Equal(warningLast), check.Equals, true)
	c.Check(noticeToMap(c, notice)["occurrences"], check.Equals, 2.0)
	c.Check(notice.LastData()["show-after"], check.Equals, "2h0m0s")
	c.Check(notice.LastData()["last-shown"], check.Equals, warningShown.Format(time.RFC3339Nano))
}

func (stateSuite) TestMigrateWarningsExistingNoticePreservesLastShown(c *check.C) {
	now := time.Now().UTC().Truncate(time.Second)
	restore := state.MockTime(now)
	defer restore()

	noticeFirst := now.Add(-30 * time.Minute)
	noticeLast := now.Add(-20 * time.Minute)
	noticeShown := now.Add(-15 * time.Minute)
	warningFirst := now.Add(-10 * time.Minute)
	warningLast := now.Add(-5 * time.Minute)
	defaultExpireAfter := state.DefaultWarningExpireAfter.String()

	existingNotice := map[string]any{
		"id":             "1",
		"type":           string(state.WarningNotice),
		"key":            "same warning",
		"first-occurred": noticeFirst,
		"last-occurred":  noticeLast,
		"last-repeated":  noticeLast,
		"occurrences":    1,
		"expire-after":   defaultExpireAfter,
		"last-data": map[string]string{
			"show-after": "1h0m0s",
			"last-shown": noticeShown.Format(time.RFC3339Nano),
		},
	}
	warnings := []oldWarningJSON{{
		Message:     "same warning",
		FirstAdded:  warningFirst,
		LastAdded:   warningLast,
		ExpireAfter: defaultExpireAfter,
		RepeatAfter: "2h0m0s",
	}}

	st, err := state.ReadState(nil, bytes.NewReader(migrateWarningsState(c, warnings, []any{existingNotice}, 1, noticeLast)))
	c.Assert(err, check.IsNil)
	st.Lock()
	defer st.Unlock()

	notices := st.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 1)
	notice := notices[0]
	c.Check(notice.NoticeFirstOccurred().Equal(noticeFirst), check.Equals, true)
	c.Check(notice.LastRepeated().Equal(warningLast), check.Equals, true)
	c.Check(notice.LastData()["show-after"], check.Equals, "2h0m0s")
	c.Check(notice.LastData()["last-shown"], check.Equals, noticeShown.Format(time.RFC3339Nano))
}

func (stateSuite) TestMigrateWarningsCheckpoint(c *check.C) {
	now := time.Now().UTC().Truncate(time.Second)
	restore := state.MockTime(now)
	defer restore()

	firstAdded := now.Add(-10 * time.Minute)
	lastAdded := now.Add(-5 * time.Minute)
	warnings := []oldWarningJSON{{
		Message:     "hello",
		FirstAdded:  firstAdded,
		LastAdded:   lastAdded,
		ExpireAfter: state.DefaultWarningExpireAfter.String(),
		RepeatAfter: state.DefaultWarningShowAfter.String(),
	}}

	backend := &fakeStateBackend{}
	st := state.New(backend)
	st.Lock()
	err := json.Unmarshal(migrateWarningsState(c, warnings, []any{}, 0, time.Time{}), st)
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Assert(backend.checkpoints, check.HasLen, 1)
	c.Check(bytes.Contains(backend.checkpoints[0], []byte(`"warnings"`)), check.Equals, false)

	st2, err := state.ReadState(nil, bytes.NewReader(backend.checkpoints[0]))
	c.Assert(err, check.IsNil)
	st2.Lock()
	defer st2.Unlock()
	notices := st2.Notices(&state.NoticeFilter{Types: []state.NoticeType{state.WarningNotice}})
	c.Assert(notices, check.HasLen, 1)
	c.Check(notices[0].Key(), check.Equals, "hello")
	c.Check(notices[0].NoticeFirstOccurred().Equal(firstAdded), check.Equals, true)
	c.Check(notices[0].LastRepeated().Equal(lastAdded), check.Equals, true)
}

func (stateSuite) TestMigrateWarningsStateReadableByOldVersion(c *check.C) {
	now := time.Now().UTC().Truncate(time.Second)
	restore := state.MockTime(now)
	defer restore()

	firstAdded := now.Add(-10 * time.Minute)
	lastAdded := now.Add(-5 * time.Minute)
	warnings := []oldWarningJSON{{
		Message:     "hello",
		FirstAdded:  firstAdded,
		LastAdded:   lastAdded,
		ExpireAfter: state.DefaultWarningExpireAfter.String(),
		RepeatAfter: state.DefaultWarningShowAfter.String(),
	}}

	st, err := state.ReadState(nil, bytes.NewReader(migrateWarningsState(c, warnings, []any{}, 0, time.Time{})))
	c.Assert(err, check.IsNil)

	st.Lock()
	newState, err := json.Marshal(st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(bytes.Contains(newState, []byte(`"warnings"`)), check.Equals, false)

	// Simulate the previous state format, which understood both the legacy
	// warnings field and notices. The new state must still be readable, with
	// the warning available as a notice rather than in the warnings field.
	var oldState struct {
		Warnings []json.RawMessage `json:"warnings"`
		Notices  []*state.Notice   `json:"notices"`
	}
	c.Assert(json.Unmarshal(newState, &oldState), check.IsNil)
	c.Check(oldState.Warnings, check.HasLen, 0)
	c.Assert(oldState.Notices, check.HasLen, 1)
	c.Check(oldState.Notices[0].Type(), check.Equals, state.WarningNotice)
	c.Check(oldState.Notices[0].Key(), check.Equals, "hello")
	c.Check(oldState.Notices[0].NoticeFirstOccurred().Equal(firstAdded), check.Equals, true)
	c.Check(oldState.Notices[0].LastRepeated().Equal(lastAdded), check.Equals, true)
}
