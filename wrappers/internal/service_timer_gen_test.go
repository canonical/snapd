// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package internal_test

import (
	"fmt"
	"os"
	"os/exec"

	. "gopkg.in/check.v1"

	_ "github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/wrappers/internal"
)

type serviceTimerUnitGenSuite struct {
	testutil.BaseTest
}

var _ = Suite(&serviceTimerUnitGenSuite{})

func (s *serviceTimerUnitGenSuite) TestServiceTimerUnit(c *C) {
	const expectedServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Timer app for snap application snap.app
Requires=%s-snap-44.mount
After=%s-snap-44.mount
X-Snappy=yes

[Timer]
Unit=snap.snap.app.service
OnCalendar=*-*-* 10:00
OnCalendar=*-*-* 11:00

[Install]
WantedBy=timers.target
`

	expectedService := fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix)
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		DaemonScope: snap.SystemDaemon,
		StopTimeout: timeout.DefaultTimeout,
		Timer: &snap.TimerInfo{
			Timer: "10:00-12:00/2",
		},
	}
	service.Timer.App = service

	generatedWrapper, err := internal.GenerateSnapServiceTimerUnitFile(service)
	c.Assert(err, IsNil)

	c.Logf("timer: \n%v\n", string(generatedWrapper))
	c.Assert(string(generatedWrapper), Equals, expectedService)
}

func (s *serviceTimerUnitGenSuite) TestServiceTimerUnitBadTimer(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		DaemonScope: snap.SystemDaemon,
		StopTimeout: timeout.DefaultTimeout,
		Timer: &snap.TimerInfo{
			Timer: "bad-timer",
		},
	}
	service.Timer.App = service

	generatedWrapper, err := internal.GenerateSnapServiceTimerUnitFile(service)
	c.Assert(err, ErrorMatches, `cannot parse "bad-timer": "bad" is not a valid weekday`)
	c.Assert(generatedWrapper, IsNil)
}

func (s *serviceTimerUnitGenSuite) TestServiceTimerServiceUnit(c *C) {
	const expectedServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run --timer="10:00-12:00,,mon,23:00~01:00/2" snap.app
SyslogIdentifier=snap.app
Restart=%s
WorkingDirectory=/var/snap/snap/44
TimeoutStopSec=30
Type=%s
`

	expectedService := fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "simple")
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		DaemonScope: snap.SystemDaemon,
		StopTimeout: timeout.DefaultTimeout,
		Timer: &snap.TimerInfo{
			Timer: "10:00-12:00,,mon,23:00~01:00/2",
		},
	}

	generatedWrapper, err := internal.GenerateSnapServiceUnitFile(service, nil)
	c.Assert(err, IsNil)

	c.Logf("service: \n%v\n", string(generatedWrapper))
	c.Assert(string(generatedWrapper), Equals, expectedService)
}

func (s *serviceTimerUnitGenSuite) TestTimerGenerateSchedules(c *C) {
	systemdAnalyzePath, _ := exec.LookPath("systemd-analyze")
	if systemdAnalyzePath != "" {
		// systemd-analyze is in the path, but it will fail if the
		// daemon is not running (as it happens in LP builds) and writes
		// the following to stderr:
		//   Failed to create bus connection: No such file or directory
		cmd := exec.Command(systemdAnalyzePath, "calendar", "12:00")
		err := cmd.Run()
		if err != nil {
			// turns out it's not usable, disable extra verification
			fmt.Fprintln(os.Stderr, `WARNING: systemd-analyze not usable, cannot validate a known schedule "12:00"`)
			systemdAnalyzePath = ""
		}
	}

	if systemdAnalyzePath == "" {
		fmt.Fprintln(os.Stderr, "WARNING: generated schedules will not be validated by systemd-analyze")
	}

	for _, t := range []struct {
		in         string
		expected   []string
		randomized bool
	}{{
		in:       "9:00-11:00,,20:00-22:00",
		expected: []string{"*-*-* 09:00", "*-*-* 20:00"},
	}, {
		in:       "9:00-11:00/2,,20:00",
		expected: []string{"*-*-* 09:00", "*-*-* 10:00", "*-*-* 20:00"},
	}, {
		in:         "9:00~11:00/2,,20:00",
		expected:   []string{`\*-\*-\* 09:[0-5][0-9]`, `\*-\*-\* 10:[0-5][0-9]`, `\*-\*-\* 20:00`},
		randomized: true,
	}, {
		in:       "mon,10:00,,fri,15:00",
		expected: []string{"Mon *-*-* 10:00", "Fri *-*-* 15:00"},
	}, {
		in:       "mon-fri,10:00-11:00",
		expected: []string{"Mon,Tue,Wed,Thu,Fri *-*-* 10:00"},
	}, {
		in:       "fri-mon,10:00-11:00",
		expected: []string{"Fri,Sat,Sun,Mon *-*-* 10:00"},
	}, {
		in:       "mon5,10:00",
		expected: []string{"Mon *-*-22,23,24,25,26,27,28,29,30,31 10:00"},
	}, {
		in:       "mon2,10:00",
		expected: []string{"Mon *-*-8,9,10,11,12,13,14 10:00"},
	}, {
		in:       "mon2,mon1,10:00",
		expected: []string{"Mon *-*-8,9,10,11,12,13,14 10:00", "Mon *-*-1,2,3,4,5,6,7 10:00"},
	}, {
		// (deprecated syntax, reduced to mon1-mon)
		// NOTE: non-representable, assumes that service runner does the
		// filtering of when to run the timer
		in:       "mon1-mon3,10:00",
		expected: []string{"*-*-8,9,10,11,12,13,14 10:00", "*-*-1,2,3,4,5,6,7 10:00"},
	}, {
		in:         "mon,10:00~12:00,,fri,15:00",
		expected:   []string{`Mon \*-\*-\* 1[01]:[0-5][0-9]`, `Fri \*-\*-\* 15:00`},
		randomized: true,
	}, {
		in:         "23:00~24:00/4",
		expected:   []string{`\*-\*-\* 23:[01][0-9]`, `\*-\*-\* 23:[12][0-9]`, `\*-\*-\* 23:[34][0-9]`, `*-*-* 23:[45][0-9]`},
		randomized: true,
	}, {
		in:         "23:00~01:00/4",
		expected:   []string{`\*-\*-\* 23:[0-2][0-9]`, `\*-\*-\* 23:[3-5][0-9]`, `\*-\*-\* 00:[0-2][0-9]`, `\*-\*-\* 00:[3-5][0-9]`},
		randomized: true,
	}, {
		in:       "23:00-01:00/4",
		expected: []string{`*-*-* 23:00`, `*-*-* 23:30`, `*-*-* 00:00`, `*-*-* 00:30`},
	}, {
		in:       "24:00",
		expected: []string{`*-*-* 00:00`},
	}, {
		// NOTE: non-representable, assumes that service runner does the
		// filtering of when to run the timer
		in:       "fri-mon1,10:00",
		expected: []string{"*-*-22,23,24,25,26,27,28,29,30,31 10:00", "*-*-1,2,3,4,5,6,7 10:00"},
	}, {
		// NOTE: non-representable, assumes that service runner does the
		// filtering of when to run the timer
		in:       "mon5-fri,10:00",
		expected: []string{"*-*-29,30,31 10:00", "*-*-1,2,3,4,5,6,7 10:00", "*-*-22,23,24,25,26,27,28 10:00"},
	}, {
		// NOTE: non-representable, assumes that service runner does the
		// filtering of when to run the timer
		in:       "mon4-fri,10:00",
		expected: []string{"*-*-29,30,31 10:00", "*-*-1,2,3,4,5,6,7 10:00", "*-*-22,23,24,25,26,27,28 10:00"},
	}, {
		// NOTE: non-representable, assumes that service runner does the
		// filtering of when to run the timer
		in:       "mon-fri2,10:00",
		expected: []string{"*-*-1,2,3,4,5,6,7 10:00", "*-*-8,9,10,11,12,13,14 10:00"},
	}, {
		// NOTE: non-representable, assumes that service runner does the
		// filtering of when to run the timer
		in:       "mon-fri5,10:00",
		expected: []string{"*-*-29,30,31 10:00", "*-*-22,23,24,25,26,27,28 10:00"},
	}, {
		// NOTE: non-representable, assumes that service runner does the
		// filtering of when to run the timer
		in:       "mon1-mon,10:00",
		expected: []string{"*-*-8,9,10,11,12,13,14 10:00", "*-*-1,2,3,4,5,6,7 10:00"},
	}, {
		in:       "mon",
		expected: []string{"Mon *-*-*"},
	}, {
		in:       "mon,fri",
		expected: []string{"Mon *-*-*", "Fri *-*-*"},
	}, {
		in:       "mon2,mon1",
		expected: []string{"Mon *-*-8,9,10,11,12,13,14", "Mon *-*-1,2,3,4,5,6,7"},
	}} {
		c.Logf("trying %+v", t)

		schedule, err := timeutil.ParseSchedule(t.in)
		c.Check(err, IsNil)

		timer := internal.GenerateOnCalendarSchedules(schedule)
		c.Check(timer, Not(IsNil))
		if !t.randomized {
			c.Check(timer, DeepEquals, t.expected)
		} else {
			c.Assert(timer, HasLen, len(t.expected))
			for i := range timer {
				c.Check(timer[i], Matches, t.expected[i])
			}
		}

		if systemdAnalyzePath != "" {
			cmd := exec.Command(systemdAnalyzePath, append([]string{"calendar"}, timer...)...)
			out, err := cmd.CombinedOutput()
			c.Check(err, IsNil, Commentf("systemd-analyze failed with output:\n%s", string(out)))
		}
	}
}
