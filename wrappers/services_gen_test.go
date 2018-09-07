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

package wrappers_test

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/wrappers"
)

type servicesWrapperGenSuite struct {
	testutil.BaseTest
}

var _ = Suite(&servicesWrapperGenSuite{})

const expectedServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=%s
WorkingDirectory=/var/snap/snap/44
ExecStop=/usr/bin/snap run --command=stop snap.app
ExecReload=/usr/bin/snap run --command=reload snap.app
ExecStopPost=/usr/bin/snap run --command=post-stop snap.app
TimeoutStopSec=10
Type=%s

[Install]
WantedBy=multi-user.target
`

var (
	mountUnitPrefix = strings.Replace(dirs.SnapMountDir[1:], "/", "-", -1)
)

var (
	expectedAppService     = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "simple")
	expectedDbusService    = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "dbus\nBusName=foo.bar.baz")
	expectedOneshotService = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "no", "oneshot\nRemainAfterExit=yes")
)

var (
	expectedServiceWrapperFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application xkcd-webserver.xkcd-webserver
Requires=%s-xkcd\x2dwebserver-44.mount
Wants=network.target
After=%s-xkcd\x2dwebserver-44.mount network.target
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run xkcd-webserver
SyslogIdentifier=xkcd-webserver.xkcd-webserver
Restart=on-failure
WorkingDirectory=/var/snap/xkcd-webserver/44
ExecStop=/usr/bin/snap run --command=stop xkcd-webserver
ExecReload=/usr/bin/snap run --command=reload xkcd-webserver
ExecStopPost=/usr/bin/snap run --command=post-stop xkcd-webserver
TimeoutStopSec=30
Type=%s
%s`
	expectedTypeForkingWrapper = fmt.Sprintf(expectedServiceWrapperFmt, mountUnitPrefix, mountUnitPrefix, "forking", "\n[Install]\nWantedBy=multi-user.target\n")
)

func (s *servicesWrapperGenSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *servicesWrapperGenSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFile(c *C) {
	yamlText := `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        reload-command: bin/reload
        post-stop-command: bin/stop --post
        stop-timeout: 10s
        daemon: simple
`
	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app)
	c.Assert(err, IsNil)
	c.Check(string(generatedWrapper), Equals, expectedAppService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileRestart(c *C) {
	yamlTextTemplate := `
name: snap
apps:
    app:
        restart-condition: %s
`
	for name, cond := range snap.RestartMap {
		yamlText := fmt.Sprintf(yamlTextTemplate, cond)

		info, err := snap.InfoFromSnapYaml([]byte(yamlText))
		c.Assert(err, IsNil)
		info.Revision = snap.R(44)
		app := info.Apps["app"]

		generatedWrapper, err := wrappers.GenerateSnapServiceFile(app)
		c.Assert(err, IsNil)
		wrapperText := string(generatedWrapper)
		if cond == snap.RestartNever {
			c.Check(wrapperText, Matches,
				`(?ms).*^Restart=no$.*`, Commentf(name))
		} else {
			c.Check(wrapperText, Matches,
				`(?ms).*^Restart=`+name+`$.*`, Commentf(name))
		}
	}
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileTypeForking(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:            "xkcd-webserver",
		Command:         "bin/foo start",
		StopCommand:     "bin/foo stop",
		ReloadCommand:   "bin/foo reload",
		PostStopCommand: "bin/foo post-stop",
		StopTimeout:     timeout.DefaultTimeout,
		Daemon:          "forking",
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, IsNil)
	c.Assert(string(generatedWrapper), Equals, expectedTypeForkingWrapper)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileIllegalChars(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:            "xkcd-webserver",
		Command:         "bin/foo start\n",
		StopCommand:     "bin/foo stop",
		ReloadCommand:   "bin/foo reload",
		PostStopCommand: "bin/foo post-stop",
		StopTimeout:     timeout.DefaultTimeout,
		Daemon:          "simple",
	}

	_, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, NotNil)
}

func (s *servicesWrapperGenSuite) TestGenServiceFileWithBusName(c *C) {

	yamlText := `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        reload-command: bin/reload
        post-stop-command: bin/stop --post
        stop-timeout: 10s
        bus-name: foo.bar.baz
        daemon: dbus
`

	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app)
	c.Assert(err, IsNil)

	c.Assert(string(generatedWrapper), Equals, expectedDbusService)
}

func (s *servicesWrapperGenSuite) TestGenOneshotServiceFile(c *C) {

	info := snaptest.MockInfo(c, `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        reload-command: bin/reload
        post-stop-command: bin/stop --post
        stop-timeout: 10s
        daemon: oneshot
`, &snap.SideInfo{Revision: snap.R(44)})

	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app)
	c.Assert(err, IsNil)

	c.Assert(string(generatedWrapper), Equals, expectedOneshotService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceWithSockets(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "xkcd-webserver",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:    "xkcd-webserver",
		Command: "bin/foo start",
		Daemon:  "simple",
		Plugs:   map[string]*snap.PlugInfo{"network-bind": {}},
		Sockets: map[string]*snap.SocketInfo{
			"sock1": {
				Name:         "sock1",
				ListenStream: "$SNAP_DATA/sock1.socket",
				SocketMode:   0666,
			},
		},
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(generatedWrapper), "[Install]"), Equals, false)
	c.Assert(strings.Contains(string(generatedWrapper), "WantedBy=multi-user.target"), Equals, false)
}

func (s *servicesWrapperGenSuite) TestServiceAfterBefore(c *C) {
	const expectedServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target snap.snap.bar.service snap.snap.zed.service
Before=snap.snap.foo.service
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=%s
WorkingDirectory=/var/snap/snap/44
TimeoutStopSec=30
Type=%s

[Install]
WantedBy=multi-user.target
`

	expectedService := fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "simple")
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
			Apps: map[string]*snap.AppInfo{
				"foo": {
					Name:   "foo",
					Snap:   &snap.Info{SuggestedName: "snap"},
					Daemon: "forking",
				},
				"bar": {
					Name:   "bar",
					Snap:   &snap.Info{SuggestedName: "snap"},
					Daemon: "forking",
				},
				"zed": {
					Name:   "zed",
					Snap:   &snap.Info{SuggestedName: "snap"},
					Daemon: "forking",
				},
			},
		},
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		Before:      []string{"foo"},
		After:       []string{"bar", "zed"},
		StopTimeout: timeout.DefaultTimeout,
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, IsNil)

	c.Logf("service: \n%v\n", string(generatedWrapper))
	c.Assert(string(generatedWrapper), Equals, expectedService)
}

func (s *servicesWrapperGenSuite) TestServiceTimerUnit(c *C) {
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
		StopTimeout: timeout.DefaultTimeout,
		Timer: &snap.TimerInfo{
			Timer: "10:00-12:00/2",
		},
	}
	service.Timer.App = service

	generatedWrapper, err := wrappers.GenerateSnapTimerFile(service)
	c.Assert(err, IsNil)

	c.Logf("timer: \n%v\n", string(generatedWrapper))
	c.Assert(string(generatedWrapper), Equals, expectedService)
}

func (s *servicesWrapperGenSuite) TestServiceTimerUnitBadTimer(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		StopTimeout: timeout.DefaultTimeout,
		Timer: &snap.TimerInfo{
			Timer: "bad-timer",
		},
	}
	service.Timer.App = service

	generatedWrapper, err := wrappers.GenerateSnapTimerFile(service)
	c.Assert(err, ErrorMatches, `cannot parse "bad-timer": "bad" is not a valid weekday`)
	c.Assert(generatedWrapper, IsNil)
}

func (s *servicesWrapperGenSuite) TestServiceTimerServiceUnit(c *C) {
	const expectedServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run --timer="10:00-12:00,,mon,23:00~01:00/2" snap.app
SyslogIdentifier=snap.app
Restart=%s
WorkingDirectory=/var/snap/snap/44
TimeoutStopSec=30
Type=%s

[Install]
WantedBy=multi-user.target
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
		StopTimeout: timeout.DefaultTimeout,
		Timer: &snap.TimerInfo{
			Timer: "10:00-12:00,,mon,23:00~01:00/2",
		},
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
	c.Assert(err, IsNil)

	c.Logf("service: \n%v\n", string(generatedWrapper))
	c.Assert(string(generatedWrapper), Equals, expectedService)
}

func (s *servicesWrapperGenSuite) TestTimerGenerateSchedules(c *C) {
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
		expected: []string{"Mon *-*~7/1 10:00"},
	}, {
		in:       "mon2,10:00",
		expected: []string{"Mon *-*-8..14/1 10:00"},
	}, {
		in:       "mon2,mon1,10:00",
		expected: []string{"Mon *-*-8..14/1 10:00", "Mon *-*-1..7/1 10:00"},
	}, {
		// NOTE: non-representable, assumes that service runner does the
		// filtering of when to run the timer
		in:       "mon1-mon3,10:00",
		expected: []string{"*-*-1..21/1 10:00"},
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
	}} {
		c.Logf("trying %+v", t)

		schedule, err := timeutil.ParseSchedule(t.in)
		c.Check(err, IsNil)

		timer := wrappers.GenerateOnCalendarSchedules(schedule)
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

func (s *servicesWrapperGenSuite) TestKillModeSig(c *C) {
	for _, rm := range []string{"sigterm", "sighup", "sigusr1", "sigusr2"} {
		service := &snap.AppInfo{
			Snap: &snap.Info{
				SuggestedName: "snap",
				Version:       "0.3.4",
				SideInfo:      snap.SideInfo{Revision: snap.R(44)},
			},
			Name:     "app",
			Command:  "bin/foo start",
			Daemon:   "simple",
			StopMode: snap.StopModeType(rm),
		}

		generatedWrapper, err := wrappers.GenerateSnapServiceFile(service)
		c.Assert(err, IsNil)

		c.Check(string(generatedWrapper), Equals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target
X-Snappy=yes

[Service]
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=on-failure
WorkingDirectory=/var/snap/snap/44
TimeoutStopSec=30
Type=simple
KillMode=process
KillSignal=%s

[Install]
WantedBy=multi-user.target
`, mountUnitPrefix, mountUnitPrefix, strings.ToUpper(rm)))
	}
}
