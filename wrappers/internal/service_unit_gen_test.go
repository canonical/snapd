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
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/quantity"
	_ "github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/quota"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeout"
	"github.com/snapcore/snapd/wrappers/internal"
)

type serviceUnitGenSuite struct {
	testutil.BaseTest
}

func TestInternal(t *testing.T) { TestingT(t) }

var _ = Suite(&serviceUnitGenSuite{})

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

var mountUnitPrefix = strings.Replace(dirs.SnapMountDir[1:], "/", "-", -1)

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

func (s *serviceUnitGenSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *serviceUnitGenSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *serviceUnitGenSuite) TestWriteSnapServiceUnitFileOnClassic(c *C) {
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
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yamlText)))

	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, nil))

	c.Check(string(generatedWrapper), Equals, expectedAppService)
}

func (s *serviceUnitGenSuite) TestGenerateSnapServiceOnCore(c *C) {
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
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yamlText)))

	info.Revision = snap.R(44)
	app := info.Apps["app"]

	// we are on core
	restore := release.MockOnClassic(false)
	defer restore()
	restore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core"})
	defer restore()
	dirs.SetRootDir("/")

	opts := internal.SnapServicesUnitOptions{
		CoreMountedSnapdSnapDep: "",
	}
	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, &opts))

	c.Check(string(generatedWrapper), Equals, expectedAppServiceOnCore)

	// now with additional dependency on tooling
	opts = internal.SnapServicesUnitOptions{
		CoreMountedSnapdSnapDep: "usr-lib-snapd.mount",
	}
	generatedWrapper = mylog.Check2(internal.GenerateSnapServiceUnitFile(app, &opts))

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

func (s *serviceUnitGenSuite) TestWriteSnapServiceUnitFileWithStartTimeout(c *C) {
	yamlText := `
name: snap
version: 1.0
apps:
    app:
        command: bin/start
        start-timeout: 10m
        daemon: simple
`
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yamlText)))

	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, nil))

	c.Check(string(generatedWrapper), testutil.Contains, "\nTimeoutStartSec=600\n")
}

func (s *serviceUnitGenSuite) TestWriteSnapServiceUnitFileRestart(c *C) {
	yamlTextTemplate := `
name: snap
apps:
    app:
        daemon: simple
        restart-condition: %s
`
	for name, cond := range snap.RestartMap {
		yamlText := fmt.Sprintf(yamlTextTemplate, cond)

		info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yamlText)))

		info.Revision = snap.R(44)
		app := info.Apps["app"]

		generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, nil))

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

func (s *serviceUnitGenSuite) TestWriteSnapServiceUnitFileTypeForking(c *C) {
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

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, nil))

	c.Assert(string(generatedWrapper), Equals, expectedTypeForkingWrapper)
}

func (s *serviceUnitGenSuite) TestWriteSnapServiceUnitFileIllegalChars(c *C) {
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

	_ := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, nil))
	c.Assert(err, NotNil)
}

func (s *serviceUnitGenSuite) TestGenServiceFileWithBusName(c *C) {
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

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yamlText)))

	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, nil))


	c.Assert(string(generatedWrapper), Equals, expectedDbusService)
}

func (s *serviceUnitGenSuite) TestGenServiceFileWithBusNameOnly(c *C) {
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

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yamlText)))

	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, nil))


	expectedDbusService := fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix, "on-failure", "dbus\nBusName=foo.bar.baz", expectedInstallSection)
	c.Assert(string(generatedWrapper), Equals, expectedDbusService)
}

func (s *serviceUnitGenSuite) TestGenServiceFileWithBusNameFromSlot(c *C) {
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

	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yamlText)))

	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, nil))


	// Bus name defaults to the name from the last slot the daemon
	// activates on.
	c.Assert(string(generatedWrapper), Equals, expectedDbusService)
}

func (s *serviceUnitGenSuite) TestGenOneshotServiceFile(c *C) {
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

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, nil))


	c.Assert(string(generatedWrapper), Equals, expectedOneshotService)
}

func (s *serviceUnitGenSuite) TestGenerateSnapUserServiceFile(c *C) {
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
	info := mylog.Check2(snap.InfoFromSnapYaml([]byte(yamlText)))

	info.Revision = snap.R(44)
	app := info.Apps["app"]

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(app, nil))

	c.Check(string(generatedWrapper), Equals, expectedUserAppService)
}

func (s *serviceUnitGenSuite) TestServiceAfterBefore(c *C) {
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
	}{
		{
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
		generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, nil))


		expectedService := fmt.Sprintf(expectedServiceFmt, mountUnitPrefix, mountUnitPrefix,
			tc.generatedAfter, tc.generatedBefore, "on-failure", "simple")
		c.Assert(string(generatedWrapper), Equals, expectedService)
	}
}

func (s *serviceUnitGenSuite) TestKillModeSig(c *C) {
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

		generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, nil))


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

func (s *serviceUnitGenSuite) TestRestartDelay(c *C) {
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

	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, nil))


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

func (s *serviceUnitGenSuite) TestVitalityScore(c *C) {
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

	opts := &internal.SnapServicesUnitOptions{VitalityRank: 1}
	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, opts))


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

func (s *serviceUnitGenSuite) TestQuotaGroupSlice(c *C) {
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
	}

	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build()))


	opts := &internal.SnapServicesUnitOptions{QuotaGroup: grp}
	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, opts))


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
Slice=snap.foo.slice

[Install]
WantedBy=multi-user.target
`, mountUnitPrefix, mountUnitPrefix))
}

func (s *serviceUnitGenSuite) TestQuotaGroupLogNamespace(c *C) {
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
	}

	grp := mylog.Check2(quota.NewGroup("foo", quota.NewResourcesBuilder().WithJournalNamespace().Build()))


	opts := &internal.SnapServicesUnitOptions{QuotaGroup: grp}
	generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, opts))


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
Slice=snap.foo.slice
LogNamespace=snap-foo

[Install]
WantedBy=multi-user.target
`, mountUnitPrefix, mountUnitPrefix))
}

func (s *serviceUnitGenSuite) TestQuotaGroupLogNamespaceInheritParent(c *C) {
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
	}

	testCases := []struct {
		topResources quota.Resources
		subResources quota.Resources
		expectedLog  string
		description  string
	}{
		{
			topResources: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
			subResources: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB / 2).Build(),
			expectedLog:  "snap-foo",
			description:  "Setting a namespace on parent, and none on service sub-group, must inherit parent",
		},
		{
			topResources: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
			subResources: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
			expectedLog:  "snap-foo",
			description:  "Setting a namespace on both groups, it should select parent",
		},
		{
			topResources: quota.NewResourcesBuilder().WithMemoryLimit(quantity.SizeGiB).Build(),
			subResources: quota.NewResourcesBuilder().WithJournalNamespace().Build(),
			expectedLog:  "",
			description:  "Setting a namespace on only sub-group, no namespace should be selected",
		},
	}

	for _, t := range testCases {
		grp := mylog.Check2(quota.NewGroup("foo", t.topResources))

		sub := mylog.Check2(grp.NewSubGroup("foosub", t.subResources))


		// if this is not set, then it won't be considered
		sub.Services = []string{"snap.app"}

		opts := &internal.SnapServicesUnitOptions{QuotaGroup: sub}
		generatedWrapper := mylog.Check2(internal.GenerateSnapServiceUnitFile(service, opts))

		c.Check(string(generatedWrapper), testutil.Contains, "Slice=snap.foo-foosub.slice", Commentf("test failed: %s", t.description))
		if t.expectedLog != "" {
			c.Check(string(generatedWrapper), testutil.Contains, fmt.Sprintf("LogNamespace=%s", t.expectedLog), Commentf("test failed: %s", t.description))
		} else {
			// no negative check? :(
			found := strings.Contains(string(generatedWrapper), fmt.Sprintf("LogNamespace=%s", t.expectedLog))
			c.Check(found, Equals, false, Commentf("test failed: %s", t.description))
		}
	}
}
