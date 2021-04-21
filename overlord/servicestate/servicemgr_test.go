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
	"github.com/snapcore/snapd/overlord/configstate/config"
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

	testSnapState    *snapstate.SnapState
	testSnapSideInfo *snap.SideInfo
}

var (
	unitTempl = `[Unit]
# Auto-generated, DO NOT EDIT
Description=Service for snap application test-snap.svc1
Requires=%[1]s
Wants=network.target
After=%[1]s network.target snapd.apparmor.service
%[3]sX-Snappy=yes

[Service]
EnvironmentFile=-/etc/environment
ExecStart=/usr/bin/snap run test-snap.svc1
SyslogIdentifier=test-snap.svc1
Restart=on-failure
WorkingDirectory=%[2]s/var/snap/test-snap/42
TimeoutStopSec=30
Type=simple
%[4]s
[Install]
WantedBy=multi-user.target
`

	testYaml = `name: test-snap
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`

	systemdTimeFormat = "Mon 2006-01-02 15:04:05 MST"
)

type unitOptions struct {
	usrLibSnapdOrderVerb string
	snapName             string
	snapRev              string
	oomScore             string
}

func mkUnitFile(c *C, opts *unitOptions) string {
	if opts == nil {
		opts = &unitOptions{}
	}
	usrLibSnapdSnippet := ""
	if opts.usrLibSnapdOrderVerb != "" {
		usrLibSnapdSnippet = fmt.Sprintf(`%[1]s=usr-lib-snapd.mount
After=usr-lib-snapd.mount
`,
			opts.usrLibSnapdOrderVerb)
	}
	oomScoreAdjust := ""
	if opts.oomScore != "" {
		oomScoreAdjust = fmt.Sprintf(`OOMScoreAdjust=%s
`,
			opts.oomScore,
		)
	}

	return fmt.Sprintf(unitTempl,
		systemd.EscapeUnitNamePath(filepath.Join(dirs.SnapMountDir, opts.snapName, opts.snapRev+".mount")),
		dirs.GlobalRootDir,
		usrLibSnapdSnippet,
		oomScoreAdjust,
	)
}

var _ = Suite(&ensureSnapServiceSuite{})

func (s *ensureSnapServiceSuite) mockSystemctlCalls(c *C, expCalls []expectedSystemctl) (restore func()) {
	systemctlCalls := 0
	r := systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		if systemctlCalls < len(expCalls) {
			res := expCalls[systemctlCalls]
			c.Assert(args, DeepEquals, res.expArgs)
			systemctlCalls++
			return []byte(res.output), res.err
		}
		c.Errorf("unexpected and unhandled systemctl command: %+v", args)
		return nil, fmt.Errorf("broken test")
	})

	return func() {
		r()
		// double-check at the end of the test that we got as many systemctl calls
		// as were mocked and that we didn't get less, then re-set it for the next
		// test
		c.Assert(systemctlCalls, Equals, len(expCalls))
	}
}

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

	// setup a test-snap with a service that can be easily injected into
	// snapstate to be setup as needed
	s.testSnapSideInfo = &snap.SideInfo{RealName: "test-snap", Revision: snap.R(42)}
	s.testSnapState = &snapstate.SnapState{
		Sequence: []*snap.SideInfo{s.testSnapSideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: "app",
	}

	// by default we are seeded
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesNoSnapsDoesNothing(c *C) {
	// don't mock any snaps in snapstate
	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we didn't write any services
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileAbsent)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesNotSeeded(c *C) {
	s.state.Lock()
	// we are not seeded but we do have a service which needs to be generated
	s.state.Set("seeded", false)
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	s.state.Unlock()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we didn't write any services
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileAbsent)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleWritesServicesFilesUC16(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// mock the device context as uc16
	s.AddCleanup(snapstatetest.MockDeviceModel(s.uc16Model))

	s.state.Unlock()

	// don't add a usr-lib-snapd.mount unit since we won't read it, since we are
	// on uc16

	// we will only trigger a daemon-reload once after generating the service
	// file
	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	})
	defer r()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote a service unit file
	content := mkUnitFile(c, &unitOptions{
		snapName: "test-snap",
		snapRev:  "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSkipsSnapdSnap(c *C) {
	s.state.Lock()
	// add an unexpected snapd snap which has services in it, but we
	// specifically skip the snapd snap when considering services to add since
	// it is special
	sideInfo := &snap.SideInfo{RealName: "snapd", Revision: snap.R(42)}
	snapstate.Set(s.state, "snapd", &snapstate.SnapState{
		Sequence: []*snap.SideInfo{sideInfo},
		Current:  snap.R(42),
		Active:   true,
		SnapType: string(snap.TypeSnapd),
	})
	snaptest.MockSnapCurrent(c, `name: snapd
type: snapd
version: v1
apps:
  svc1:
    command: bin.sh
    daemon: simple
`, sideInfo)

	s.state.Unlock()

	// don't need to mock usr-lib-snapd.mount since we will skip before that
	// with snapd as the only snap

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we didn't write a snap service file for snapd
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.snapd.svc1.service"), testutil.FileAbsent)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesUC18(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		{
			// usr-lib-snapd.mount has not been stopped this boot "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  "InactiveEnterTimestamp=",
		},
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesVitalityRankUC18(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// also set vitality-hint for this snap
	t := config.NewTransaction(s.state)
	err := t.Set("core", "resilience.vitality-hint", "bar,test-snap")
	c.Assert(err, IsNil)
	t.Commit()

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err = os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		{
			// usr-lib-snapd.mount has not been stopped this boot "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  "InactiveEnterTimestamp=",
		},
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
		oomScore:             "-898",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndRestarts(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	err = os.Chtimes(usrLibSnapdMountFile, now, now)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
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
			// future (hence before the usr-lib-snapd.mount unit was stopped and
			// after usr-lib-snapd.mount file was modified)
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
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

type systemctlDisabledServicError struct{}

func (s systemctlDisabledServicError) Msg() []byte   { return []byte("disabled") }
func (s systemctlDisabledServicError) ExitCode() int { return 1 }
func (s systemctlDisabledServicError) Error() string { return "disabled service" }

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesButDoesNotRestartDisabledServices(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	err = os.Chtimes(usrLibSnapdMountFile, now, now)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
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
			// future (hence before the usr-lib-snapd.mount unit was stopped and
			// after usr-lib-snapd.mount file was modified)
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture),
		},
		// the service is disabled
		{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  "disabled",
			err:     systemctlDisabledServicError{},
		},
		// then we don't restart the service even though it was killed
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesKilledBeforeSnapdRefresh(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	err = os.Chtimes(usrLibSnapdMountFile, now, now)
	c.Assert(err, IsNil)

	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)
	thePast := now.Add(-30 * time.Minute).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
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
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesKilledAfterSnapdRefresh(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	err = os.Chtimes(usrLibSnapdMountFile, now, now)
	c.Assert(err, IsNil)

	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)
	thePast := now.Add(-30 * time.Minute).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
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
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsUC18(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	err = os.Chtimes(usrLibSnapdMountFile, now, now)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
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
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// the file was rewritten to use Wants instead now
	wantsContent := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, wantsContent)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesNoChangeServiceFileDoesNothingUC18(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	err = os.Chtimes(usrLibSnapdMountFile, now, now)
	c.Assert(err, IsNil)

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	// add the initial state of the service file using Wants
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(content), 0644)
	c.Assert(err, IsNil)

	// we don't use systemctl at all because we didn't change anything
	// s.systemctlReturns = []expectedSystemctl{}

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// the file was not modified
	c.Assert(svcFile, testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)

}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesWhenUsrLibSnapdWasInactive(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	os.Chtimes(usrLibSnapdMountFile, now, now)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		{
			// usr-lib-snapd.mount has never been stopped this boot, thus has
			// always been active
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  "InactiveEnterTimestamp=",
		},
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndRestartsButThenFallsbackToReboot(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	err = os.Chtimes(usrLibSnapdMountFile, now, now)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
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
			// future (hence before the usr-lib-snapd.mount unit was stopped and
			// after usr-lib-snapd.mount file was modified)
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture),
		},
		{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  "enabled",
		},
		{
			expArgs: []string{"start", "snap.test-snap.svc1.service"},
			err:     fmt.Errorf("this service is having a bad day"),
		},
		{
			expArgs: []string{"stop", "snap.test-snap.svc1.service"},
			err:     fmt.Errorf("this service is still having a bad day"),
		},
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "error trying to restart killed services, immediately rebooting: this service is having a bad day")

	// we did write the service unit file
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we requested a restart
	c.Assert(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndTriesRestartButFailsButThenFallsbackToReboot(c *C) {
	s.state.Lock()
	// there is a snap in snap state that needs a service generated for it
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	s.state.Unlock()

	// add the usr-lib-snapd.mount unit
	err := os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	usrLibSnapdMountFile := filepath.Join(dirs.SnapServicesDir, wrappers.SnapdToolingMountUnit)
	err = ioutil.WriteFile(usrLibSnapdMountFile, nil, 0644)
	c.Assert(err, IsNil)

	now := time.Now()
	err = os.Chtimes(usrLibSnapdMountFile, now, now)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, []expectedSystemctl{
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
			// future (hence before the usr-lib-snapd.mount unit was stopped and
			// after usr-lib-snapd.mount file was modified)
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture),
		},
		{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			err:     fmt.Errorf("systemd is having a bad day"),
		},
	})
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "error trying to restart killed services, immediately rebooting: systemd is having a bad day")

	// we did write the service unit file
	content := mkUnitFile(c, &unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we requested a restart
	c.Assert(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
}
