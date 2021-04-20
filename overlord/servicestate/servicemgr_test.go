// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package servicestate_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

type expectedSystemctl struct {
	expArgs []string
	output  string
	err     error
}

type ensureSnapServiceSuite struct {
	testutil.BaseTest

	mgr *servicestate.ServiceManager

	o     *overlord.Overlord
	se    *overlord.StateEngine
	state *state.State

	restartRequests []state.RestartType
	restartObserve  func()

	uc18Model *asserts.Model
	uc16Model *asserts.Model

	systemctlCalls   int
	systemctlReturns []expectedSystemctl
}

var _ = Suite(&ensureSnapServiceSuite{})

func (s *ensureSnapServiceSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.restartRequests = nil

	s.restartObserve = nil
	s.o = overlord.MockWithStateAndRestartHandler(nil, func(req state.RestartType) {
		s.restartRequests = append(s.restartRequests, req)
		if s.restartObserve != nil {
			s.restartObserve()
		}
	})

	s.state = s.o.State()
	s.state.Lock()
	s.state.VerifyReboot("boot-id-0")
	s.state.Unlock()
	s.se = s.o.StateEngine()

	s.mgr = servicestate.Manager(s.state, s.o.TaskRunner())
	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())

	err := s.o.StartUp()
	c.Assert(err, IsNil)

	s.uc18Model = assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "canonical",
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
		"base":         "core18",
	}).(*asserts.Model)

	s.uc16Model = assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "canonical",
		"series":       "16",
		"brand-id":     "canonical",
		"model":        "pc",
		"gadget":       "pc",
		"kernel":       "kernel",
		"architecture": "amd64",
		// no base
	}).(*asserts.Model)

	// by default mock that we are uc18
	s.AddCleanup(snapstatetest.MockDeviceModel(s.uc18Model))

	// by default we are seeded
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	r := systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		if s.systemctlCalls < len(s.systemctlReturns) {
			res := s.systemctlReturns[s.systemctlCalls]
			c.Assert(args, DeepEquals, res.expArgs)
			s.systemctlCalls++
			return []byte(res.output), res.err
		}
		c.Errorf("unexpected and unhandled systemctl command: %+v", args)
		return nil, fmt.Errorf("broken test")
	})
	s.AddCleanup(r)

	// double-check at the end of the test that we got as many systemctl calls
	// as were mocked and that we didn't get less, then re-set it for the next
	// test
	s.AddCleanup(func() {
		c.Check(s.systemctlReturns, HasLen, s.systemctlCalls)
		s.systemctlReturns = nil
		s.systemctlCalls = 0
	})
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesNoSnapsDoesNothing(c *C) {
	// don't mock any snaps in snapstate
	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we didn't write any services
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileAbsent)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesNotSeeded(c *C) {
	s.state.Lock()
	// we are not seeded
	s.state.Set("seeded", false)

	// but there is a snap in snap state that needs a service generated for
	// it
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapCurrent(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple`, sideInfo)

	s.state.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we didn't write any services
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileAbsent)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleWritesServicesFilesUC16(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapCurrent(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, sideInfo)

	// mock the device context as uc16
	s.AddCleanup(snapstatetest.MockDeviceModel(s.uc16Model))

	s.state.Unlock()

	// don't add a usr-lib-snapd.mount unit since we won't read it, since we are
	// on uc16

	// we will only trigger a daemon-reload once after generating the service
	// file
	s.systemctlReturns = []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	}

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
	))
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleWritesServicesFilesUC18(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapCurrent(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, sideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	os.Chtimes(usrLibSnapdMountFile, now, now)

	slightFuture := now.Add(30 * time.Minute).Format("Mon 2006-01-02 15:04:05 MST")
	theFuture := now.Add(1 * time.Hour).Format("Mon 2006-01-02 15:04:05 MST")

	s.systemctlReturns = []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture),
		},
		{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture),
		},
		{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  "enabled",
		},
		{
			expArgs: []string{"start", "snap.test-snap.svc1.service"},
		},
	}

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Wants",
	))
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesKilledBeforeSnapdRefresh(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapCurrent(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, sideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	os.Chtimes(usrLibSnapdMountFile, now, now)

	theFuture := now.Add(1 * time.Hour).Format("Mon 2006-01-02 15:04:05 MST")
	thePast := now.Add(-30 * time.Minute).Format("Mon 2006-01-02 15:04:05 MST")

	s.systemctlReturns = []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture),
		},
		{
			// but the snap.test-snap.svc1 was stopped before that, so it isn't
			// restarted
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", thePast),
		},
	}

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Wants",
	))
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesKilledAfterSnapdRefresh(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapCurrent(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, sideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	os.Chtimes(usrLibSnapdMountFile, now, now)

	theFuture := now.Add(1 * time.Hour).Format("Mon 2006-01-02 15:04:05 MST")
	thePast := now.Add(-30 * time.Minute).Format("Mon 2006-01-02 15:04:05 MST")

	s.systemctlReturns = []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		{
			// usr-lib-snapd.mount was stopped in the past
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", thePast),
		},
		{
			// but the snap.test-snap.svc1 was stopped after that, so it isn't
			// restarted
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture),
		},
	}

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Wants",
	))
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleRewritesServicesFilesUC18(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapCurrent(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, sideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	os.Chtimes(usrLibSnapdMountFile, now, now)

	slightFuture := now.Add(30 * time.Minute).Format("Mon 2006-01-02 15:04:05 MST")
	theFuture := now.Add(1 * time.Hour).Format("Mon 2006-01-02 15:04:05 MST")

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	templ := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`

	// add the initial state of the service file using Requires
	err = ioutil.WriteFile(svcFile, []byte(fmt.Sprintf(templ,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Requires",
	)), 0644)
	c.Assert(err, IsNil)

	s.systemctlReturns = []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture),
		},
		{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture),
		},
		{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  "enabled",
		},
		{
			expArgs: []string{"start", "snap.test-snap.svc1.service"},
		},
	}

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// the file was rewritten to use Wants instead now
	c.Assert(svcFile, testutil.FileEquals, fmt.Sprintf(templ,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Wants",
	))
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesNoChangeServiceFileDoesNothingUC18(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapCurrent(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, sideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	os.Chtimes(usrLibSnapdMountFile, now, now)

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	templ := `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`

	// add the initial state of the service file using Wants
	initial := fmt.Sprintf(templ,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Wants",
	)
	err = ioutil.WriteFile(svcFile, []byte(initial), 0644)
	c.Assert(err, IsNil)

	// we don't use systemctl at all because we didn't change anything
	s.systemctlReturns = []expectedSystemctl{}

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// the file was rewritten to use Wants instead now
	c.Assert(svcFile, testutil.FileEquals, initial)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesOnlyWhenUsrLibSnapdWasInactive(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	sideInfo := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	snapstate.Set(s.state, "test-snap", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	})
	snaptest.MockSnapCurrent(c, `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, sideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	os.Chtimes(usrLibSnapdMountFile, now, now)

	s.systemctlReturns = []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		{
			// usr-lib-snapd.mount has never been stopped this boot, thus has
			// always been active
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  "InactiveEnterTimestamp=",
		},
	}

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, fmt.Sprintf(`[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
X-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple

[Install]
WantedBy=multi-user.target
`,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, "test-snap", "42.mount")),
		dirs.GlobalRootDir,
		"Wants",
	))
}
