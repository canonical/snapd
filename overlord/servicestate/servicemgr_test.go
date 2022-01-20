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
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/wrappers"
)

type baseServiceMgrTestSuite struct {
	testutil.BaseTest

	mgr *servicestate.ServiceManager

	o     *overlord.Overlord
	se    *overlord.StateEngine
	state *state.State

	restartRequests []restart.RestartType
	restartObserve  func()

	uc18Model *asserts.Model
	uc16Model *asserts.Model

	testSnapState    *snapstate.SnapState
	testSnapSideInfo *snap.SideInfo
}

func (s *baseServiceMgrTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.restartRequests = nil

	s.restartObserve = nil
	s.o = overlord.Mock()
	s.state = s.o.State()
	s.state.Lock()
	restart.Init(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(req restart.RestartType) {
		s.restartRequests = append(s.restartRequests, req)
		if s.restartObserve != nil {
			s.restartObserve()
		}
	}))
	s.state.Unlock()
	s.se = s.o.StateEngine()

	s.mgr = servicestate.Manager(s.state, s.o.TaskRunner())
	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())

	err := s.o.StartUp()
	c.Assert(err, IsNil)

	// by default we are seeded
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

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
}

type expectedSystemctl struct {
	expArgs []string
	output  []string
	err     error
}

type ensureSnapServiceSuite struct {
	baseServiceMgrTestSuite
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
	testYaml2 = `name: test-snap2
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

func mkUnitFile(opts *unitOptions) string {
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

func (s *baseServiceMgrTestSuite) mockSystemctlCalls(c *C, expCalls []expectedSystemctl) (restore func()) {
	allSystemctlCalls := [][]string{}
	r := systemd.MockSystemctl(func(args ...string) ([]byte, error) {
		systemctlCalls := len(allSystemctlCalls)
		if systemctlCalls < len(expCalls) {
			res := expCalls[systemctlCalls]
			// if we have show command, unit order should not matter
			var expectedArgs []string
			var output []string
			if args[0] == "show" && len(args) > 3 && len(args) == len(res.expArgs) {
				// we have more than one unit, reorder expected reply array
				// as unit order should not be dictated for show command
				expectedArgs = append(expectedArgs, args[0], args[1])
				i := 2
				// handle InactiveEnterTimestamp case, which is passed as extra element in array
				if args[2] == "InactiveEnterTimestamp" {
					expectedArgs = append(expectedArgs, args[2])
					i++
				}
				offset := i
				for ; i < len(args); i++ {
					// take passed args as order to obey for the reply string
					for ii := offset; ii < len(res.expArgs); ii++ {
						if args[i] == res.expArgs[ii] {
							expectedArgs = append(expectedArgs, res.expArgs[ii])
							if ii-offset < len(res.output) {
								output = append(output, res.output[ii-offset])
							}
							continue
						}
					}
				}
			} else {
				expectedArgs = res.expArgs
				output = res.output
			}
			c.Check(args, DeepEquals, expectedArgs)
			// use expected order for final confirmation, we just validated this step
			allSystemctlCalls = append(allSystemctlCalls, res.expArgs)
			// join output strings, add empty line in between for "show" command
			if args[0] == "show" {
				return []byte(strings.Join(output[:], "\n")), res.err
			} else {
				return []byte(strings.Join(output[:], "")), res.err
			}
		} else {
			allSystemctlCalls = append(allSystemctlCalls, args)
		}
		c.Errorf("unexpected and unhandled systemctl command: %+v", args)
		return nil, fmt.Errorf("broken test")
	})

	return func() {
		r()
		// double-check at the end of the test that we got as many systemctl calls
		// as were mocked and that we didn't get less, then re-set it for the next
		// test
		expArgCalls := make([][]string, 0, len(expCalls))
		for _, call := range expCalls {
			expArgCalls = append(expArgCalls, call.expArgs)
		}
		c.Assert(allSystemctlCalls, DeepEquals, expArgCalls)
	}
}

func (s *ensureSnapServiceSuite) SetUpTest(c *C) {
	s.baseServiceMgrTestSuite.SetUpTest(c)
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

func (s *ensureSnapServiceSuite) testEnsureSnapServicesSimpleWritesServicesFilesUC16(c *C, expectedCalls []expectedSystemctl) {
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
	r := s.mockSystemctlCalls(c, expectedCalls)
	defer r()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote a service unit file
	content := mkUnitFile(&unitOptions{
		snapName: "test-snap",
		snapRev:  "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleWritesServicesFilesUC16OldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesSimpleWritesServicesFilesUC16(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		// ActiveState=inactive was passed, daemon-reload is not needed)
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleWritesServicesFilesUC16SmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesSimpleWritesServicesFilesUC16(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
		// ActiveState=inactive was passed, daemon-reload is not needed)
	},
	)
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

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesUC18(c *C, expectedCalls []expectedSystemctl) {
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

	r := s.mockSystemctlCalls(c, expectedCalls)
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesUC18OldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesUC18(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesUC1SmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesUC18(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesUC18DaemonReload(c *C, expectedCalls []expectedSystemctl) {
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

	r := s.mockSystemctlCalls(c, expectedCalls)
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesUC18DaemonReloadOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesUC18DaemonReload(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
			output:  []string{""},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesUC18DaemonReloadSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesUC18DaemonReload(c, []expectedSystemctl{
		{
			// ActiveState=inactive was passed, daemon-reload is not needed
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesVitalityRankUC18(c *C, expectedCalls []expectedSystemctl) {
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

	r := s.mockSystemctlCalls(c, expectedCalls)
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
		oomScore:             "-898",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesVitalityRankUC18OldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesVitalityRankUC18(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
			output:  []string{""},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesVitalityRankUC18SmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesVitalityRankUC18(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesVitalityRankUC18DaemonReload(c *C, expectedCalls []expectedSystemctl) {
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

	r := s.mockSystemctlCalls(c, expectedCalls)
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
		oomScore:             "-898",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesVitalityRankUC18DaemonReloadOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesVitalityRankUC18DaemonReload(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
		// ActiveState=inactive was passed, daemon-reload is not needed
	})
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesVitalityRankUC18DaemonReloadSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesVitalityRankUC18DaemonReload(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
		// ActiveState=inactive was passed, daemon-reload is not needed
	})
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesAndDoesNotRestartIfBootTimeAfterModTime(c *C, expectedCalls []expectedSystemctl) {
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

	logbuf, r := logger.MockLogger()
	defer r()

	// TZ's are important for the boot time specifically, we need to output the
	// UTC time from the uptime script below, otherwise using local time here
	// but not elsewhere leads to errors
	future := now.Add(30 * time.Minute).UTC()

	// we won't try to start services if the current boot time is ahead of the
	// modification time

	// mock the uptime command
	cmd := testutil.MockCommand(c, "uptime", fmt.Sprintf(`
#!/bin/sh

if [ "$TZ" != "UTC" ]; then
	echo "unexpected TZ env value: $TZ (expected UTC)"
	exit 1
fi

if [ "$*" != "-s" ]; then
	echo "arguments $* were unexpected"
	exit 1
fi

echo %[1]q
`, future.Format("2006-01-02 15:04:05")))
	defer cmd.Restore()

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	r = s.mockSystemctlCalls(c, expectedCalls)
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"uptime", "-s"},
	})

	c.Assert(logbuf.String(), Equals, "")

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndDoesNotRestartIfBootTimeAfterModTimeOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndDoesNotRestartIfBootTimeAfterModTime(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	})
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndDoesNotRestartIfBootTimeAfterModTimeSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndDoesNotRestartIfBootTimeAfterModTime(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
		{
			expArgs: []string{"daemon-reload"},
		},
	})
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesAndIgnoresBootTimeErrors(c *C, expectedCalls []expectedSystemctl) {
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

	// if the boot time can't be determined, we log a message and continue on
	// considering whether or not the service should be restarted based on when
	// it exited
	cmd := testutil.MockCommand(c, "uptime", `
#!/bin/sh
echo "boot time broken"
exit 1
`)
	defer cmd.Restore()

	logbuf, r := logger.MockLogger()
	defer r()

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	r = s.mockSystemctlCalls(c, append(expectedCalls,
		// ActiveState=inactive was passed, daemon-reload is not needed
		expectedSystemctl{
			// usr-lib-snapd.mount has never been stopped though so we skip out
			// anyways
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{"InactiveEnterTimestamp="},
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"uptime", "-s"},
	})

	c.Assert(logbuf.String(), Matches, ".*error getting boot time: boot time broken\n")

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndIgnoresBootTimeErrorsOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndIgnoresBootTimeErrors(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndIgnoresBootTimeErrorsSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndIgnoresBootTimeErrors(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesAndRestarts(c *C, expectedCalls []expectedSystemctl) {
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

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future (hence before the usr-lib-snapd.mount unit was stopped and
			// after usr-lib-snapd.mount file was modified)
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture)},
		},
		expectedSystemctl{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  []string{"enabled"},
		},
		expectedSystemctl{
			expArgs: []string{"start", "snap.test-snap.svc1.service"},
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndRestartsOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndRestarts(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndRestartsSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndRestarts(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}

type systemctlDisabledServiceError struct{}

func (s systemctlDisabledServiceError) Msg() []byte   { return []byte("disabled") }
func (s systemctlDisabledServiceError) ExitCode() int { return 1 }
func (s systemctlDisabledServiceError) Error() string { return "disabled service" }

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesButDoesNotRestartDisabledServices(c *C, expectedCalls []expectedSystemctl) {
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

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		// ActiveState=inactive was passed, daemon-reload is not needed
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future (hence before the usr-lib-snapd.mount unit was stopped and
			// after usr-lib-snapd.mount file was modified)
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture)},
		},
		// the service is disabled
		expectedSystemctl{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  []string{"disabled"},
			err:     systemctlDisabledServiceError{},
		},
		// then we don't restart the service even though it was killed
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesButDoesNotRestartDisabledServicesOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesButDoesNotRestartDisabledServices(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesButDoesNotRestartDisabledServicesSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesButDoesNotRestartDisabledServices(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesDoesNotRestartServicesKilledBeforeSnapdRefresh(c *C, expectedCalls []expectedSystemctl) {
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

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)
	thePast := now.Add(-30 * time.Minute).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped before that, so it isn't
			// restarted
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", thePast)},
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesKilledBeforeSnapdRefreshOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesDoesNotRestartServicesKilledBeforeSnapdRefresh(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
			output:  []string{""},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesKilledBeforeSnapdRefreshSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesDoesNotRestartServicesKilledBeforeSnapdRefresh(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesDoesNotRestartServicesKilledAfterSnapdRefresh(c *C, expectedCalls []expectedSystemctl) {
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

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)
	thePast := now.Add(-30 * time.Minute).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped in the past
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", thePast)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped after that, so it isn't
			// restarted
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture)},
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// we wrote the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesKilledAfterSnapdRefreshOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesDoesNotRestartServicesKilledAfterSnapdRefresh(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
			output:  []string{""},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesKilledAfterSnapdRefreshSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesDoesNotRestartServicesKilledAfterSnapdRefresh(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nType=simple\nActiveState=inactive\nUnitFileState=enabled\nNeedDaemonReload=no\n"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionSilly(c *C, expectedCalls []expectedSystemctl) {
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

	// this test is about the specific scenario we have now when using systemctl
	// show --property where the time precision of the InactiveEnterTimestamp's
	// is much lower than that of the modification file time, so we need to
	// set the inactive enter time for both the usr-lib-snapd.mount and the snap
	// service to be the same time, which is actually _in the past_ compared to
	// the file modification time

	// truncate the current time and add 500 milliseconds
	t0 := time.Now().Truncate(time.Second).Add(500 * time.Millisecond)
	err = os.Chtimes(usrLibSnapdMountFile, t0, t0)
	c.Assert(err, IsNil)

	// drop the milliseconds
	t1 := t0.Truncate(time.Second)
	t1Str := t1.Format(systemdTimeFormat)

	// double check our math for the times is correct
	c.Assert(t1.Before(t0), Equals, true)
	c.Assert(t0.After(t1), Equals, true)
	c.Assert(t1.Equal(t0), Equals, false)

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		// ActiveState=inactive was passed, daemon-reload is not needed
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", t1Str)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", t1Str)},
		},
		expectedSystemctl{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  []string{"enabled"},
		},
		expectedSystemctl{
			expArgs: []string{"start", "snap.test-snap.svc1.service"},
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// the file was rewritten to use Wants instead now
	wantsContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, wantsContent)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionSillyOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionSilly(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
			output:  []string{""},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionSillySmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionSilly(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"ActiveState=inactive\nId=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionMoreSilly(c *C, expectedCalls []expectedSystemctl) {
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

	// this test is like TestEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionSilly,
	// but more extreme, in that we don't have precision problems of less than a
	// second, we have some more critical error where the lower timestamp range
	// is somehow way in the future and we want our system to act rationally and
	// pick the upper time bound as the lower time bound when the initially
	// identified lower time bound is nonsensical

	// truncate the current time and add 500 minutes
	now := time.Now().Truncate(time.Second)
	t0 := now.Add(500 * time.Minute)
	err = os.Chtimes(usrLibSnapdMountFile, t0, t0)
	c.Assert(err, IsNil)

	t1 := now
	t1Str := t1.Format(systemdTimeFormat)

	// double check our math for the times is correct
	c.Assert(t1.Before(t0), Equals, true)
	c.Assert(t0.After(t1), Equals, true)
	c.Assert(t1.Equal(t0), Equals, false)

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		// ActiveState=inactive was passed, daemon-reload is not needed
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", t1Str)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", t1Str)},
		},
		expectedSystemctl{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  []string{"enabled"},
		},
		expectedSystemctl{
			expArgs: []string{"start", "snap.test-snap.svc1.service"},
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// the file was rewritten to use Wants instead now
	wantsContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, wantsContent)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionMoreSillyOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionMoreSilly(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
			output:  []string{""},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionMoreSillySmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsTimePrecisionMoreSilly(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsUC18(c *C, expectedCalls []expectedSystemctl) {
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
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture)},
		},
		expectedSystemctl{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  []string{"enabled"},
		},
		expectedSystemctl{
			expArgs: []string{"start", "snap.test-snap.svc1.service"},
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	// the file was rewritten to use Wants instead now
	wantsContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service"), testutil.FileEquals, wantsContent)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsUC18OldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsUC18(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsUC18SmartSystem(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesSimpleRewritesServicesFilesAndRestartsUC18(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
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
	content := mkUnitFile(&unitOptions{
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

func (s *ensureSnapServiceSuite) testEnsureSnapServicesDoesNotRestartServicesWhenUsrLibSnapdWasNeverInactive(c *C, expectedCalls []expectedSystemctl) {
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

	svcFile := filepath.Join(dirs.GlobalRootDir, "/etc/systemd/system/snap.test-snap.svc1.service")

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		// ActiveState=inactive was passed, daemon-reload is not needed
		expectedSystemctl{
			// usr-lib-snapd.mount has never been stopped this boot, thus has
			// always been active
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{"InactiveEnterTimestamp="},
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	// we did not request a restart
	c.Assert(s.restartRequests, HasLen, 0)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesWhenUsrLibSnapdWasNeverInactiveOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesDoesNotRestartServicesWhenUsrLibSnapdWasNeverInactive(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
			output:  []string{""},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesDoesNotRestartServicesWhenUsrLibSnapdWasNeverInactiveSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesDoesNotRestartServicesWhenUsrLibSnapdWasNeverInactive(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesAndRestartsButThenFallsbackToReboot(c *C, expectedCalls []expectedSystemctl) {
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

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		// ActiveState=inactive was passed, daemon-reload is not needed
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future (hence before the usr-lib-snapd.mount unit was stopped and
			// after usr-lib-snapd.mount file was modified)
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture)},
		},
		expectedSystemctl{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			output:  []string{"enabled"},
		},
		expectedSystemctl{
			expArgs: []string{"start", "snap.test-snap.svc1.service"},
			err:     fmt.Errorf("this service is having a bad day"),
		},
		expectedSystemctl{
			expArgs: []string{"stop", "snap.test-snap.svc1.service"},
			err:     fmt.Errorf("this service is still having a bad day"),
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "error trying to restart killed services, immediately rebooting: this service is having a bad day")

	// we did write the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	// we requested a restart
	c.Assert(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndRestartsButThenFallsbackToRebootOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndRestartsButThenFallsbackToReboot(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndRestartsButThenFallsbackToRebootSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndRestartsButThenFallsbackToReboot(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) testEnsureSnapServicesWritesServicesFilesAndTriesRestartButFailsButThenFallsbackToReboot(c *C, expectedCalls []expectedSystemctl) {
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

	// add the initial state of the service file using Requires
	requiresContent := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Requires",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	err = ioutil.WriteFile(svcFile, []byte(requiresContent), 0644)
	c.Assert(err, IsNil)

	slightFuture := now.Add(30 * time.Minute).Format(systemdTimeFormat)
	theFuture := now.Add(1 * time.Hour).Format(systemdTimeFormat)

	r := s.mockSystemctlCalls(c, append(expectedCalls,
		// ActiveState=inactive was passed, daemon-reload is not needed
		expectedSystemctl{
			// usr-lib-snapd.mount was stopped "far in the future"
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "usr-lib-snapd.mount"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", theFuture)},
		},
		expectedSystemctl{
			// but the snap.test-snap.svc1 was stopped only slightly in the
			// future (hence before the usr-lib-snapd.mount unit was stopped and
			// after usr-lib-snapd.mount file was modified)
			expArgs: []string{"show", "--property", "InactiveEnterTimestamp", "snap.test-snap.svc1.service"},
			output:  []string{fmt.Sprintf("InactiveEnterTimestamp=%s", slightFuture)},
		},
		expectedSystemctl{
			expArgs: []string{"is-enabled", "snap.test-snap.svc1.service"},
			err:     fmt.Errorf("systemd is having a bad day"),
		},
	))
	defer r()

	err = s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "error trying to restart killed services, immediately rebooting: systemd is having a bad day")

	// we did write the service unit file
	content := mkUnitFile(&unitOptions{
		usrLibSnapdOrderVerb: "Wants",
		snapName:             "test-snap",
		snapRev:              "42",
	})
	c.Assert(svcFile, testutil.FileEquals, content)

	// we requested a restart
	c.Assert(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndTriesRestartButFailsButThenFallsbackToRebootOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndTriesRestartButFailsButThenFallsbackToReboot(c, []expectedSystemctl{
		{
			expArgs: []string{"daemon-reload"},
			output:  []string{""},
		},
	},
	)
}

func (s *ensureSnapServiceSuite) TestEnsureSnapServicesWritesServicesFilesAndTriesRestartButFailsButThenFallsbackToRebootSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testEnsureSnapServicesWritesServicesFilesAndTriesRestartButFailsButThenFallsbackToReboot(c, []expectedSystemctl{
		{
			expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
			output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
		},
	},
	)
}
