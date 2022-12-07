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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/release"
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
After=%s-snap-44.mount network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=%s
WorkingDirectory=/var/snap/snap/44
ExecStop=/usr/bin/snap run --command=stop snap.app
ExecReload=/usr/bin/snap run --command=reload snap.app
ExecStopPost=/usr/bin/snap run --command=post-stop snap.app
TimeoutStopSec=10
Type=%s
%s`

const expectedInstallSection = `
[Install]
WantedBy=multi-user.target
`

const expectedUserServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
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
WantedBy=default.target
`

var (
	mountUnitPrefix = strings.Replace(dirs.SnapMountDir[1:], "/", "-", -1)
)

var (
	expectedAppService     = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "simple", expectedInstallSection)
	expectedDbusService    = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "dbus\nBusName=foo.bar.baz", "")
	expectedOneshotService = fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "no", "oneshot\nRemainAfterExit=yes", expectedInstallSection)
	expectedUserAppService = fmt.Sprintf(expectedUserServiceFmt, "on-failure", "simple")
)

var (
	expectedServiceWrapperFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application xkcd-webserver.xkcd-webserver
Requires=%s-xkcd\x2dwebserver-44.mount
Wants=network.target
After=%s-xkcd\x2dwebserver-44.mount network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
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
	expectedTypeForkingWrapper = fmt.Sprintf(expectedServiceWrapperFmt, mountUnitPrefix, mountUnitPrefix, "forking", expectedInstallSection)
)

func (s *servicesWrapperGenSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *servicesWrapperGenSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileOnClassic(c *C) {
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

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, nil)
	c.Assert(err, IsNil)
	c.Check(string(generatedWrapper), Equals, expectedAppService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceOnCore(c *C) {
	defer func() { dirs.SetRootDir("/") }()

	expectedAppServiceOnCore := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application foo.app
Requires=snap-foo-44.mount
Wants=network.target
After=snap-foo-44.mount network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run foo.app
SyslogIdentifier=foo.app
Restart=on-failure
WorkingDirectory=/var/snap/foo/44
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`

	yamlText := `
name: foo
version: 1.0
apps:
    app:
        command: bin/start
        daemon: simple
`
	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	// we are on core
	restore := release.MockOnClassic(false)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core"})
	defer restore()
	dirs.SetRootDir("/")

	opts := wrappers.GenerateSnapServicesOptions{
		RequireMountedSnapdSnap: false,
	}
	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, &opts)
	c.Assert(err, IsNil)
	c.Check(string(generatedWrapper), Equals, expectedAppServiceOnCore)

	// now with additional dependency on tooling
	opts = wrappers.GenerateSnapServicesOptions{
		RequireMountedSnapdSnap: true,
	}
	generatedWrapper, err = wrappers.GenerateSnapServiceFile(app, &opts)
	c.Assert(err, IsNil)
	// we gain additional Requires= & After= on usr-lib-snapd.mount
	expectedAppServiceOnCoreWithSnapd := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application foo.app
Requires=snap-foo-44.mount
Wants=network.target
After=snap-foo-44.mount network.target snapd.apparmor.service
Wants=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run foo.app
SyslogIdentifier=foo.app
Restart=on-failure
WorkingDirectory=/var/snap/foo/44
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`

	c.Check(string(generatedWrapper), Equals, expectedAppServiceOnCoreWithSnapd)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileWithStartTimeout(c *C) {
	yamlText := `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        start-timeout: 10m
        daemon: simple
`
	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, nil)
	c.Assert(err, IsNil)
	c.Check(string(generatedWrapper), testutil.Contains, "\nTimeoutStartSec=600\n")
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceFileRestart(c *C) {
	yamlTextTemplate := `
name: snap
apps:
    app:
        daemon: simple
        restart-condition: %s
`
	for name, cond := range snap.RestartMap {
		yamlText := fmt.Sprintf(yamlTextTemplate, cond)

		info, err := snap.InfoFromSnapYaml([]byte(yamlText))
		c.Assert(err, IsNil)
		info.Revision = snap.R(44)
		app := info.Apps["app"]

		generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, nil)
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
		DaemonScope:     snap.SystemDaemon,
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service, nil)
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
		DaemonScope:     snap.SystemDaemon,
	}

	_, err := wrappers.GenerateSnapServiceFile(service, nil)
	c.Assert(err, NotNil)
}

func (s *servicesWrapperGenSuite) TestGenServiceFileWithBusName(c *C) {
	yamlText := `
name: snap
version: 1.0
slots:
    dbus-slot:
        interface: dbus
        bus: system
        name: org.example.Foo
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        reload-command: bin/reload
        post-stop-command: bin/stop --post
        stop-timeout: 10s
        bus-name: foo.bar.baz
        daemon: dbus
        activates-on: [dbus-slot]
`

	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, nil)
	c.Assert(err, IsNil)

	c.Assert(string(generatedWrapper), Equals, expectedDbusService)
}

func (s *servicesWrapperGenSuite) TestGenServiceFileWithBusNameOnly(c *C) {

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

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, nil)
	c.Assert(err, IsNil)

	expectedDbusService := fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "dbus\nBusName=foo.bar.baz", expectedInstallSection)
	c.Assert(string(generatedWrapper), Equals, expectedDbusService)
}

func (s *servicesWrapperGenSuite) TestGenServiceFileWithBusNameFromSlot(c *C) {

	yamlText := `
name: snap
version: 1.0
slots:
    dbus-slot1:
        interface: dbus
        bus: system
        name: org.example.Foo
    dbus-slot2:
        interface: dbus
        bus: system
        name: foo.bar.baz
apps:
    app:
        command: bin/start
        stop-command: bin/stop
        reload-command: bin/reload
        post-stop-command: bin/stop --post
        stop-timeout: 10s
        daemon: dbus
        activates-on: [dbus-slot1, dbus-slot2]
`

	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, nil)
	c.Assert(err, IsNil)

	// Bus name defaults to the name from the last slot the daemon
	// activates on.
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

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, nil)
	c.Assert(err, IsNil)

	c.Assert(string(generatedWrapper), Equals, expectedOneshotService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapUserServiceFile(c *C) {
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
        daemon-scope: user
`
	info, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, IsNil)
	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(app, nil)
	c.Assert(err, IsNil)
	c.Check(string(generatedWrapper), Equals, expectedUserAppService)
}

func (s *servicesWrapperGenSuite) TestGenerateSnapServiceWithSockets(c *C) {
	const sock1ExpectedFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Socket sock1 for snap application some-snap.app
Requires=%s-some\x2dsnap-44.mount
After=%s-some\x2dsnap-44.mount
X-Snappy=yes

[Socket]
Service=snap.some-snap.app.service
FileDescriptorName=sock1
ListenStream=%s/sock1.socket
SocketMode=0666

[Install]
WantedBy=sockets.target
`
	const sock2ExpectedFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Socket sock2 for snap application some-snap.app
Requires=%s-some\x2dsnap-44.mount
After=%s-some\x2dsnap-44.mount
X-Snappy=yes

[Socket]
Service=snap.some-snap.app.service
FileDescriptorName=sock2
ListenStream=%s/sock2.socket

[Install]
WantedBy=sockets.target
`

	si := &snap.Info{
		SuggestedName: "some-snap",
		Version:       "1.0",
		SideInfo:      snap.SideInfo{Revision: snap.R(44)},
	}
	service := &snap.AppInfo{
		Snap:        si,
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		DaemonScope: snap.SystemDaemon,
		Plugs:       map[string]*snap.PlugInfo{"network-bind": {Interface: "network-bind"}},
		Sockets: map[string]*snap.SocketInfo{
			"sock1": {
				Name:         "sock1",
				ListenStream: "$SNAP_DATA/sock1.socket",
				SocketMode:   0666,
			},
			"sock2": {
				Name:         "sock2",
				ListenStream: "$SNAP_DATA/sock2.socket",
			},
		},
	}
	service.Sockets["sock1"].App = service
	service.Sockets["sock2"].App = service

	sock1Expected := fmt.Sprintf(sock1ExpectedFmt, mountUnitPrefix, mountUnitPrefix, si.DataDir())
	sock2Expected := fmt.Sprintf(sock2ExpectedFmt, mountUnitPrefix, mountUnitPrefix, si.DataDir())

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service, nil)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(generatedWrapper), "[Install]"), Equals, false)
	c.Assert(strings.Contains(string(generatedWrapper), "WantedBy=multi-user.target"), Equals, false)

	generatedSockets, err := wrappers.GenerateSnapSocketFiles(service)
	c.Assert(err, IsNil)
	c.Assert(generatedSockets, HasLen, 2)
	c.Assert(generatedSockets, DeepEquals, map[string][]byte{
		"sock1": []byte(sock1Expected),
		"sock2": []byte(sock2Expected),
	})
}

func (s *servicesWrapperGenSuite) TestServiceAfterBefore(c *C) {
	const expectedServiceFmt = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target %s snapd.apparmor.service
Before=%s
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=%s
WorkingDirectory=/var/snap/snap/44
TimeoutStopSec=30
Type=%s

[Install]
WantedBy=multi-user.target
`

	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
			Apps: map[string]*snap.AppInfo{
				"foo": {
					Name:        "foo",
					Snap:        &snap.Info{SuggestedName: "snap"},
					Daemon:      "forking",
					DaemonScope: snap.SystemDaemon,
				},
				"bar": {
					Name:        "bar",
					Snap:        &snap.Info{SuggestedName: "snap"},
					Daemon:      "forking",
					DaemonScope: snap.SystemDaemon,
				},
				"zed": {
					Name:        "zed",
					Snap:        &snap.Info{SuggestedName: "snap"},
					Daemon:      "forking",
					DaemonScope: snap.SystemDaemon,
				},
				"baz": {
					Name:        "baz",
					Snap:        &snap.Info{SuggestedName: "snap"},
					Daemon:      "forking",
					DaemonScope: snap.SystemDaemon,
				},
			},
		},
		Name:        "app",
		Command:     "bin/foo start",
		Daemon:      "simple",
		DaemonScope: snap.SystemDaemon,
		StopTimeout: timeout.DefaultTimeout,
	}

	for _, tc := range []struct {
		after           []string
		before          []string
		generatedAfter  string
		generatedBefore string
	}{{
		after:           []string{"bar", "zed"},
		generatedAfter:  "snap.snap.bar.service snap.snap.zed.service",
		before:          []string{"foo", "baz"},
		generatedBefore: "snap.snap.foo.service snap.snap.baz.service",
	}, {
		after:           []string{"bar"},
		generatedAfter:  "snap.snap.bar.service",
		before:          []string{"foo"},
		generatedBefore: "snap.snap.foo.service",
	},
	} {
		c.Logf("tc: %v", tc)
		service.After = tc.after
		service.Before = tc.before
		generatedWrapper, err := wrappers.GenerateSnapServiceFile(service, nil)
		c.Assert(err, IsNil)

		expectedService := fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix,
			tc.generatedAfter, tc.generatedBefore, "on-failure", "simple")
		c.Assert(string(generatedWrapper), Equals, expectedService)
	}
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
		DaemonScope: snap.SystemDaemon,
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
		DaemonScope: snap.SystemDaemon,
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

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service, nil)
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
	for _, rm := range []string{"sigterm", "sighup", "sigusr1", "sigusr2", "sigint"} {
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
			StopMode:    snap.StopModeType(rm),
		}

		generatedWrapper, err := wrappers.GenerateSnapServiceFile(service, nil)
		c.Assert(err, IsNil)

		c.Check(string(generatedWrapper), Equals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
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

func (s *servicesWrapperGenSuite) TestRestartDelay(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:         "app",
		Command:      "bin/foo start",
		Daemon:       "simple",
		DaemonScope:  snap.SystemDaemon,
		RestartDelay: timeout.Timeout(20 * time.Second),
	}

	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service, nil)
	c.Assert(err, IsNil)

	c.Check(string(generatedWrapper), Equals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=on-failure
RestartSec=20
WorkingDirectory=/var/snap/snap/44
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`, mountUnitPrefix, mountUnitPrefix))
}

func (s *servicesWrapperGenSuite) TestVitalityScore(c *C) {
	service := &snap.AppInfo{
		Snap: &snap.Info{
			SuggestedName: "snap",
			Version:       "0.3.4",
			SideInfo:      snap.SideInfo{Revision: snap.R(44)},
		},
		Name:         "app",
		Command:      "bin/foo start",
		Daemon:       "simple",
		DaemonScope:  snap.SystemDaemon,
		RestartDelay: timeout.Timeout(20 * time.Second),
	}

	opts := &wrappers.GenerateSnapServicesOptions{VitalityRank: 1}
	generatedWrapper, err := wrappers.GenerateSnapServiceFile(service, opts)
	c.Assert(err, IsNil)

	c.Check(string(generatedWrapper), Equals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application snap.app
Requires=%s-snap-44.mount
Wants=network.target
After=%s-snap-44.mount network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run snap.app
SyslogIdentifier=snap.app
Restart=on-failure
RestartSec=20
WorkingDirectory=/var/snap/snap/44
TimeoutStopSec=30
Type=simple
OOMScoreAdjust=-899

[Install]
WantedBy=multi-user.target
`, mountUnitPrefix, mountUnitPrefix))
}
