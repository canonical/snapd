// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate_test

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/sequence"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"

	. "gopkg.in/check.v1"
)

func (s *snapMCPSuite) TestGetServiceLogsValidateInvalidRange(c *C) {
	_, err := (snapstate.GetServiceLogsTool{}).Call(context.Background(), state.New(nil), map[string]any{
		"service_name": "snap-a.svc1",
		"since":        "2026-01-11T00:00:00Z",
		"until":        "2026-01-10T00:00:00Z",
	})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "since must not be after until")
}

func (s *snapMCPSuite) TestGetServiceLogsCallMissingService(c *C) {
	result, callErr := (snapstate.GetServiceLogsTool{}).Call(context.Background(), state.New(nil), map[string]any{"service_name": "snap-a.svc1"})
	c.Check(result, IsNil)
	c.Assert(callErr, NotNil)
	c.Check(callErr.Error(), Equals, `cannot find service "snap-a.svc1"`)
}

func (s *snapMCPSuite) TestGetServiceLogsPassesSinceUntilLinesAndStderrOnly(c *C) {
	st := state.New(nil)

	info, err := snap.InfoFromSnapYaml([]byte(`name: svc-snap
version: 1
apps:
  daemon:
    command: bin/run
    daemon: simple
`))
	c.Assert(err, IsNil)

	restoreReadInfo := snapstate.MockSnapReadInfo(func(name string, _ *snap.SideInfo) (*snap.Info, error) {
		c.Assert(name, Equals, "svc-snap")
		return info, nil
	})
	defer restoreReadInfo()

	st.Lock()
	snapstate.Set(st, "svc-snap", &snapstate.SnapState{
		Sequence: sequence.SnapSequence{Revisions: []*sequence.RevisionSideState{
			sequence.NewRevisionSideState(&snap.SideInfo{RealName: "svc-snap", Revision: snap.R(1)}, nil),
		}},
		Current: snap.R(1),
		Active:  true,
	})
	st.Unlock()

	mockJournalctl := testutil.MockCommand(c, "journalctl", "")
	defer mockJournalctl.Restore()

	since := time.Date(2026, time.January, 1, 10, 0, 0, 0, time.UTC)
	until := time.Date(2026, time.January, 1, 11, 0, 0, 0, time.UTC)

	result, callErr := (snapstate.GetServiceLogsTool{}).Call(context.Background(), st, map[string]any{
		"service_name": "svc-snap.daemon",
		"lines":        5,
		"since":        since.Format(time.RFC3339),
		"until":        until.Format(time.RFC3339),
		"stderr_only":  true,
	})
	c.Assert(callErr, IsNil)

	out := resultToMap(c, result)
	c.Check(out["service_name"], Equals, "svc-snap.daemon")
	c.Check(out["logs"], DeepEquals, []any{})

	calls := mockJournalctl.Calls()
	c.Assert(calls, HasLen, 1)
	call := calls[0]
	c.Assert(call[0], Equals, "journalctl")

	joined := strings.Join(call, "\x00")
	c.Check(strings.Contains(joined, "\x00-o\x00json\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00--no-pager\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00-n\x005\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00--since\x00"+since.Format(time.RFC3339)+"\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00--until\x00"+until.Format(time.RFC3339)+"\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00-p\x000..3\x00"), Equals, true)
	unit := ""
	for i := 0; i+1 < len(call); i++ {
		if call[i] == "-u" {
			unit = call[i+1]
			break
		}
	}
	c.Assert(unit, Not(Equals), "")
	c.Check(strings.Contains(unit, "svc-snap"), Equals, true)
	c.Check(strings.Contains(unit, "daemon"), Equals, true)
	c.Check(strings.HasSuffix(unit, ".service"), Equals, true)
}

func (s *snapMCPSuite) TestGetServiceLogsMapsEntries(c *C) {
	st := state.New(nil)

	info, err := snap.InfoFromSnapYaml([]byte(`name: svc-snap
version: 1
apps:
  daemon:
    command: bin/run
    daemon: simple
`))
	c.Assert(err, IsNil)

	restoreReadInfo := snapstate.MockSnapReadInfo(func(name string, _ *snap.SideInfo) (*snap.Info, error) {
		c.Assert(name, Equals, "svc-snap")
		return info, nil
	})
	defer restoreReadInfo()

	st.Lock()
	snapstate.Set(st, "svc-snap", &snapstate.SnapState{
		Sequence: sequence.SnapSequence{Revisions: []*sequence.RevisionSideState{
			sequence.NewRevisionSideState(&snap.SideInfo{RealName: "svc-snap", Revision: snap.R(1)}, nil),
		}},
		Current: snap.R(1),
		Active:  true,
	})
	st.Unlock()

	timeFromString := "2026-02-03T10:11:12Z"
	parsed, err := time.Parse(time.RFC3339, "2026-02-03T11:12:13Z")
	c.Assert(err, IsNil)

	restoreReadLogs := snapstate.MockReadServiceLogs(func(serviceApp *snap.AppInfo, lines int, since, until string, stderrOnly bool) ([]map[string]any, error) {
		c.Assert(serviceApp, NotNil)
		c.Check(lines, Equals, 100)
		c.Check(since, Equals, "")
		c.Check(until, Equals, "")
		c.Check(stderrOnly, Equals, false)

		return []map[string]any{
			{
				"timestamp": timeFromString,
				"message":   "line one",
				"sid":       "sid-1",
				"pid":       "100",
				"priority":  float64(3),
			},
			{
				"timestamp": parsed,
				"message":   "line two",
				"sid":       "sid-2",
				"pid":       "101",
				"priority":  2,
			},
		}, nil
	})
	defer restoreReadLogs()

	result, callErr := (snapstate.GetServiceLogsTool{}).Call(context.Background(), st, map[string]any{
		"service_name": "svc-snap.daemon",
	})
	c.Assert(callErr, IsNil)

	out := resultToMap(c, result)
	c.Check(out["service_name"], Equals, "svc-snap.daemon")

	logs, ok := out["logs"].([]any)
	c.Assert(ok, Equals, true)
	c.Assert(logs, HasLen, 2)

	first, ok := logs[0].(map[string]any)
	c.Assert(ok, Equals, true)
	c.Check(first["timestamp"], Equals, timeFromString)
	c.Check(first["message"], Equals, "line one")
	c.Check(first["sid"], Equals, "sid-1")
	c.Check(first["pid"], Equals, "100")
	c.Check(first["priority"], Equals, float64(3))

	second, ok := logs[1].(map[string]any)
	c.Assert(ok, Equals, true)
	c.Check(second["timestamp"], Equals, parsed.Format(time.RFC3339))
	c.Check(second["message"], Equals, "line two")
	c.Check(second["sid"], Equals, "sid-2")
	c.Check(second["pid"], Equals, "101")
	c.Check(second["priority"], Equals, float64(2))
}

func (s *snapMCPSuite) TestReadServiceLogsRejectsNilService(c *C) {
	entries, err := snapstate.ReadServiceLogsForTest(nil, 10, "", "", false)
	c.Check(entries, IsNil)
	c.Assert(err, ErrorMatches, "service app must not be nil")
}

func (s *snapMCPSuite) TestReadServiceLogsNoTailAndNamespace(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: svc-snap
version: 1
apps:
  daemon:
    command: bin/run
    daemon: simple
`))
	c.Assert(err, IsNil)
	app := info.Apps["daemon"]
	c.Assert(app, NotNil)

	restoreSystemd := systemd.MockSystemdVersion(245, nil)
	defer restoreSystemd()

	mockJournalctl := testutil.MockCommand(c, "journalctl", "")
	defer mockJournalctl.Restore()

	entries, err := snapstate.ReadServiceLogsForTest(app, -1, "2026-01-01T10:00:00Z", "2026-01-01T11:00:00Z", true)
	c.Assert(err, IsNil)
	c.Check(entries, DeepEquals, []map[string]any{})

	calls := mockJournalctl.Calls()
	c.Assert(calls, HasLen, 1)
	call := calls[0]
	joined := strings.Join(call, "\x00")

	c.Check(strings.Contains(joined, "\x00-o\x00json\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00--no-pager\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00--no-tail\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00--since\x002026-01-01T10:00:00Z\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00--until\x002026-01-01T11:00:00Z\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00-p\x000..3\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00--namespace=*\x00"), Equals, true)
	c.Check(strings.Contains(joined, "\x00-u\x00"), Equals, true)
	c.Check(strings.Contains(joined, app.ServiceName()), Equals, true)
}

func (s *snapMCPSuite) TestReadServiceLogsSystemdVersionError(c *C) {
	info, err := snap.InfoFromSnapYaml([]byte(`name: svc-snap
version: 1
apps:
  daemon:
    command: bin/run
    daemon: simple
`))
	c.Assert(err, IsNil)
	app := info.Apps["daemon"]
	c.Assert(app, NotNil)

	restoreSystemd := systemd.MockSystemdVersion(0, errors.New("boom"))
	defer restoreSystemd()

	entries, err := snapstate.ReadServiceLogsForTest(app, 10, "", "", false)
	c.Check(entries, IsNil)
	c.Assert(err, ErrorMatches, "cannot get systemd version: boom")
}
