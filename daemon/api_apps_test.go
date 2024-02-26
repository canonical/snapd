// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"net/http"
	"net/http/httptest"
	"os/user"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&appsSuite{})

type appsSuite struct {
	apiBaseSuite

	journalctlRestorer func()
	jctlSvcses         [][]string
	jctlNs             []int
	jctlFollows        []bool
	jctlNamespaces     []bool
	jctlRCs            []io.ReadCloser
	jctlErrs           []error

	serviceControlError error
	serviceControlCalls []serviceControlArgs

	infoA, infoB, infoC, infoD, infoE *snap.Info
}

func (s *appsSuite) journalctl(svcs []string, n int, follow, namespaces bool) (rc io.ReadCloser, err error) {
	s.jctlSvcses = append(s.jctlSvcses, svcs)
	s.jctlNs = append(s.jctlNs, n)
	s.jctlFollows = append(s.jctlFollows, follow)
	s.jctlNamespaces = append(s.jctlNamespaces, namespaces)

	if len(s.jctlErrs) > 0 {
		err, s.jctlErrs = s.jctlErrs[0], s.jctlErrs[1:]
	}
	if len(s.jctlRCs) > 0 {
		rc, s.jctlRCs = s.jctlRCs[0], s.jctlRCs[1:]
	}

	return rc, err
}

type serviceControlArgs struct {
	action  string
	options string
	names   []string
	scope   servicestate.ScopeSelector
	users   []string
}

func (s *appsSuite) fakeServiceControl(st *state.State, appInfos []*snap.AppInfo, inst *servicestate.Instruction, cu *user.User, flags *servicestate.Flags, context *hookstate.Context) ([]*state.TaskSet, error) {
	if flags != nil {
		panic("flags are not expected")
	}

	if s.serviceControlError != nil {
		return nil, s.serviceControlError
	}

	users, err := inst.Users.UserList(cu)
	if err != nil {
		return nil, err
	}

	serviceCommand := serviceControlArgs{action: inst.Action, scope: inst.Scope, users: users}
	if inst.RestartOptions.Reload {
		serviceCommand.options = "reload"
	}
	// only one flag should ever be set (depending on Action), but appending
	// them below acts as an extra validity check.
	if inst.StartOptions.Enable {
		serviceCommand.options += "enable"
	}
	if inst.StopOptions.Disable {
		serviceCommand.options += "disable"
	}
	for _, app := range appInfos {
		serviceCommand.names = append(serviceCommand.names, fmt.Sprintf("%s.%s", app.Snap.InstanceName(), app.Name))
	}
	s.serviceControlCalls = append(s.serviceControlCalls, serviceCommand)

	t := st.NewTask("sample", "")
	ts := state.NewTaskSet(t)
	return []*state.TaskSet{ts}, nil
}

func (s *appsSuite) SetUpSuite(c *check.C) {
	s.apiBaseSuite.SetUpSuite(c)
	s.journalctlRestorer = systemd.MockJournalctl(s.journalctl)
}

func (s *appsSuite) TearDownSuite(c *check.C) {
	s.journalctlRestorer()
	s.apiBaseSuite.TearDownSuite(c)
}

func (s *appsSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.jctlSvcses = nil
	s.jctlNs = nil
	s.jctlFollows = nil
	s.jctlNamespaces = nil
	s.jctlRCs = nil
	s.jctlErrs = nil

	d := s.daemon(c)

	s.serviceControlCalls = nil
	s.serviceControlError = nil
	restoreServicestateCtrl := daemon.MockServicestateControl(s.fakeServiceControl)
	s.AddCleanup(restoreServicestateCtrl)

	// turn off ensuring snap services which will call systemctl automatically
	r := servicestate.MockEnsuredSnapServices(s.d.Overlord().ServiceManager(), true)
	s.AddCleanup(r)
	s.AddCleanup(snapstate.MockEnsuredMountsUpdated(d.Overlord().SnapManager(), true))

	s.infoA = s.mkInstalledInState(c, s.d, "snap-a", "dev", "v1", snap.R(1), true, "apps: {svc1: {daemon: simple}, svc2: {daemon: simple, reload-command: x}}")
	s.infoB = s.mkInstalledInState(c, s.d, "snap-b", "dev", "v1", snap.R(1), false, "apps: {svc3: {daemon: simple}, cmd1: {}}")
	s.infoC = s.mkInstalledInState(c, s.d, "snap-c", "dev", "v1", snap.R(1), true, "")
	s.infoD = s.mkInstalledInState(c, s.d, "snap-d", "dev", "v1", snap.R(1), true, "apps: {cmd2: {}, cmd3: {}}")
	s.infoE = s.mkInstalledInState(c, s.d, "snap-e", "dev", "v1", snap.R(1), true, "apps: {svc4: {daemon: simple, daemon-scope: user}}")

	d.Overlord().Loop()
	s.AddCleanup(func() { d.Overlord().Stop() })
	s.AddCleanup(systemd.MockSystemdVersion(237, nil))
	s.expectAppsAccess()
}

func (s *appsSuite) expectAppsAccess() {
	s.expectOpenAccess()
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
}

func (s *appsSuite) TestSplitAppName(c *check.C) {
	type T struct {
		name string
		snap string
		app  string
	}

	for _, x := range []T{
		{name: "foo.bar", snap: "foo", app: "bar"},
		{name: "foo", snap: "foo", app: ""},
		{name: "foo.bar.baz", snap: "foo", app: "bar.baz"},
		{name: ".", snap: "", app: ""}, // SISO
	} {
		snap, app := daemon.SplitAppName(x.name)
		c.Check(x.snap, check.Equals, snap, check.Commentf(x.name))
		c.Check(x.app, check.Equals, app, check.Commentf(x.name))
	}
}

func (s *appsSuite) TestGetAppsInfo(c *check.C) {
	// System services from active snaps
	svcNames := []string{"snap-a.svc1", "snap-a.svc2"}
	for _, name := range svcNames {
		s.SysctlBufs = append(s.SysctlBufs, []byte(fmt.Sprintf(`
Id=snap.%s.service
Names=snap.%[1]s.service
Type=simple
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`[1:], name)))
	}
	// System services from inactive snaps
	svcNames = append(svcNames, "snap-b.svc3")
	// User services from active snaps
	svcNames = append(svcNames, "snap-e.svc4")
	s.SysctlBufs = append(s.SysctlBufs, []byte("enabled\n"))

	req, err := http.NewRequest("GET", "/v2/apps", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []client.AppInfo{})
	apps := rsp.Result.([]client.AppInfo)
	c.Assert(apps, check.HasLen, 7)

	for _, name := range svcNames {
		snapName, app := daemon.SplitAppName(name)
		needle := client.AppInfo{
			Snap:        snapName,
			Name:        app,
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		}
		if snapName != "snap-b" {
			// snap-b is not active (all the others are)
			needle.Active = true
			needle.Enabled = true
		}
		if snapName == "snap-e" {
			// snap-e contains user services
			needle.DaemonScope = snap.UserDaemon
			needle.Active = false
		}
		c.Check(apps, testutil.DeepContains, needle)
	}

	for _, name := range []string{"snap-b.cmd1", "snap-d.cmd2", "snap-d.cmd3"} {
		snap, app := daemon.SplitAppName(name)
		c.Check(apps, testutil.DeepContains, client.AppInfo{
			Snap: snap,
			Name: app,
		})
	}

	appNames := make([]string, len(apps))
	for i, app := range apps {
		appNames[i] = app.Snap + "." + app.Name
	}
	c.Check(sort.StringsAreSorted(appNames), check.Equals, true)
}

func (s *appsSuite) TestGetAppsInfoNames(c *check.C) {

	req, err := http.NewRequest("GET", "/v2/apps?names=snap-d", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []client.AppInfo{})
	apps := rsp.Result.([]client.AppInfo)
	c.Assert(apps, check.HasLen, 2)

	for _, name := range []string{"snap-d.cmd2", "snap-d.cmd3"} {
		snap, app := daemon.SplitAppName(name)
		c.Check(apps, testutil.DeepContains, client.AppInfo{
			Snap: snap,
			Name: app,
		})
	}

	appNames := make([]string, len(apps))
	for i, app := range apps {
		appNames[i] = app.Snap + "." + app.Name
	}
	c.Check(sort.StringsAreSorted(appNames), check.Equals, true)
}

func (s *appsSuite) TestGetAppsInfoServices(c *check.C) {
	// System services from active snaps
	svcNames := []string{"snap-a.svc1", "snap-a.svc2"}
	for _, name := range svcNames {
		s.SysctlBufs = append(s.SysctlBufs, []byte(fmt.Sprintf(`
Id=snap.%s.service
Names=snap.%[1]s.service
Type=simple
ActiveState=active
UnitFileState=enabled
NeedDaemonReload=no
`[1:], name)))
	}
	// System services from inactive snaps
	svcNames = append(svcNames, "snap-b.svc3")
	// User services from active snaps
	svcNames = append(svcNames, "snap-e.svc4")
	s.SysctlBufs = append(s.SysctlBufs, []byte("enabled\n"))

	req, err := http.NewRequest("GET", "/v2/apps?select=service", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, check.Equals, 200)
	c.Assert(rsp.Result, check.FitsTypeOf, []client.AppInfo{})
	svcs := rsp.Result.([]client.AppInfo)
	c.Assert(svcs, check.HasLen, 4)

	for _, name := range svcNames {
		snapName, app := daemon.SplitAppName(name)
		needle := client.AppInfo{
			Snap:        snapName,
			Name:        app,
			Daemon:      "simple",
			DaemonScope: snap.SystemDaemon,
		}
		if snapName != "snap-b" {
			// snap-b is not active (all the others are)
			needle.Active = true
			needle.Enabled = true
		}
		if snapName == "snap-e" {
			// snap-e contains user services
			needle.DaemonScope = snap.UserDaemon
			needle.Active = false
		}
		c.Check(svcs, testutil.DeepContains, needle)
	}

	appNames := make([]string, len(svcs))
	for i, svc := range svcs {
		appNames[i] = svc.Snap + "." + svc.Name
	}
	c.Check(sort.StringsAreSorted(appNames), check.Equals, true)
}

func (s *appsSuite) TestGetAppsInfoBadSelect(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/apps?select=potato", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 400)
}

func (s *appsSuite) TestGetAppsInfoBadName(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/apps?names=potato", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 404)
}

func (s *appsSuite) TestAppInfosForOne(c *check.C) {
	st := s.d.Overlord().State()
	appInfos, rspe := daemon.AppInfosFor(st, []string{"snap-a.svc1"}, daemon.AppInfoServiceTrue)
	c.Assert(rspe, check.IsNil)
	c.Assert(appInfos, check.HasLen, 1)
	c.Check(appInfos[0].Snap, check.DeepEquals, s.infoA)
	c.Check(appInfos[0].Name, check.Equals, "svc1")
}

func (s *appsSuite) TestAppInfosForAll(c *check.C) {
	type T struct {
		opts  daemon.AppInfoOptions
		snaps []*snap.Info
		names []string
	}

	for _, t := range []T{
		{
			opts:  daemon.AppInfoServiceTrue,
			names: []string{"svc1", "svc2", "svc3", "svc4"},
			snaps: []*snap.Info{s.infoA, s.infoA, s.infoB, s.infoE},
		},
		{
			opts:  daemon.AppInfoServiceFalse,
			names: []string{"svc1", "svc2", "cmd1", "svc3", "cmd2", "cmd3", "svc4"},
			snaps: []*snap.Info{s.infoA, s.infoA, s.infoB, s.infoB, s.infoD, s.infoD, s.infoE},
		},
	} {
		c.Assert(len(t.names), check.Equals, len(t.snaps), check.Commentf("%s", t.opts))

		st := s.d.Overlord().State()
		appInfos, rspe := daemon.AppInfosFor(st, nil, t.opts)
		c.Assert(rspe, check.IsNil, check.Commentf("%s", t.opts))
		names := make([]string, len(appInfos))
		for i, appInfo := range appInfos {
			names[i] = appInfo.Name
		}
		c.Assert(names, check.DeepEquals, t.names, check.Commentf("%s", t.opts))

		for i := range appInfos {
			c.Check(appInfos[i].Snap, check.DeepEquals, t.snaps[i], check.Commentf("%s: %s", t.opts, t.names[i]))
		}
	}
}

func (s *appsSuite) TestAppInfosForOneSnap(c *check.C) {
	st := s.d.Overlord().State()
	appInfos, rspe := daemon.AppInfosFor(st, []string{"snap-a"}, daemon.AppInfoServiceTrue)
	c.Assert(rspe, check.IsNil)
	c.Assert(appInfos, check.HasLen, 2)
	sort.Sort(snap.AppInfoBySnapApp(appInfos))

	c.Check(appInfos[0].Snap, check.DeepEquals, s.infoA)
	c.Check(appInfos[0].Name, check.Equals, "svc1")
	c.Check(appInfos[1].Snap, check.DeepEquals, s.infoA)
	c.Check(appInfos[1].Name, check.Equals, "svc2")
}

func (s *appsSuite) TestAppInfosForMixedArgs(c *check.C) {
	st := s.d.Overlord().State()
	appInfos, rspe := daemon.AppInfosFor(st, []string{"snap-a", "snap-a.svc1"}, daemon.AppInfoServiceTrue)
	c.Assert(rspe, check.IsNil)
	c.Assert(appInfos, check.HasLen, 2)
	sort.Sort(snap.AppInfoBySnapApp(appInfos))

	c.Check(appInfos[0].Snap, check.DeepEquals, s.infoA)
	c.Check(appInfos[0].Name, check.Equals, "svc1")
	c.Check(appInfos[1].Snap, check.DeepEquals, s.infoA)
	c.Check(appInfos[1].Name, check.Equals, "svc2")
}

func (s *appsSuite) TestAppInfosCleanupAndSorted(c *check.C) {
	st := s.d.Overlord().State()
	appInfos, rspe := daemon.AppInfosFor(st, []string{
		"snap-b.svc3",
		"snap-a.svc2",
		"snap-a.svc1",
		"snap-a.svc2",
		"snap-b.svc3",
		"snap-a.svc1",
		"snap-b",
		"snap-a",
	}, daemon.AppInfoServiceTrue)
	c.Assert(rspe, check.IsNil)
	c.Assert(appInfos, check.HasLen, 3)
	sort.Sort(snap.AppInfoBySnapApp(appInfos))

	c.Check(appInfos[0].Snap, check.DeepEquals, s.infoA)
	c.Check(appInfos[0].Name, check.Equals, "svc1")
	c.Check(appInfos[1].Snap, check.DeepEquals, s.infoA)
	c.Check(appInfos[1].Name, check.Equals, "svc2")
	c.Check(appInfos[2].Snap, check.DeepEquals, s.infoB)
	c.Check(appInfos[2].Name, check.Equals, "svc3")
}

func (s *appsSuite) TestAppInfosForAppless(c *check.C) {
	st := s.d.Overlord().State()
	appInfos, rspe := daemon.AppInfosFor(st, []string{"snap-c"}, daemon.AppInfoServiceTrue)
	c.Assert(rspe, check.NotNil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindAppNotFound)
	c.Assert(appInfos, check.IsNil)
}

func (s *appsSuite) TestAppInfosForMissingApp(c *check.C) {
	st := s.d.Overlord().State()
	appInfos, rspe := daemon.AppInfosFor(st, []string{"snap-c.whatever"}, daemon.AppInfoServiceTrue)
	c.Assert(rspe, check.NotNil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindAppNotFound)
	c.Assert(appInfos, check.IsNil)
}

func (s *appsSuite) TestAppInfosForMissingSnap(c *check.C) {
	st := s.d.Overlord().State()
	appInfos, rspe := daemon.AppInfosFor(st, []string{"snap-x"}, daemon.AppInfoServiceTrue)
	c.Assert(rspe, check.NotNil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Kind, check.Equals, client.ErrorKindSnapNotFound)
	c.Assert(appInfos, check.IsNil)
}

func (s *appsSuite) testPostApps(c *check.C, inst servicestate.Instruction, servicecmds []serviceControlArgs) *state.Change {
	postBody, err := json.Marshal(inst)
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBuffer(postBody))
	c.Assert(err, check.IsNil)

	rsp := s.asyncReq(c, req, s.authUser)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Check(rsp.Change, check.Matches, `[0-9]+`)

	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Tasks(), check.HasLen, len(servicecmds))

	st.Unlock()
	<-chg.Ready()
	st.Lock()

	c.Check(s.serviceControlCalls, check.DeepEquals, servicecmds)
	return chg
}

func (s *appsSuite) testPostAppsUser(c *check.C, inst servicestate.Instruction, servicecmds []serviceControlArgs, expectedErr string) *state.Change {
	postBody, err := json.Marshal(inst)
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBuffer(postBody))
	c.Assert(err, check.IsNil)
	s.asUserAuth(c, req)

	if expectedErr != "" {
		rspe := s.errorReq(c, req, s.authUser)
		c.Check(rspe.Status, check.Equals, 400)
		c.Check(rspe.Message, check.Matches, expectedErr)
		return nil
	}

	rsp := s.asyncReq(c, req, s.authUser)
	c.Assert(rsp.Status, check.Equals, 202)
	c.Check(rsp.Change, check.Matches, `[0-9]+`)

	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(chg.Tasks(), check.HasLen, len(servicecmds))

	st.Unlock()
	<-chg.Ready()
	st.Lock()

	c.Check(s.serviceControlCalls, check.DeepEquals, servicecmds)
	return chg
}

func (s *appsSuite) TestPostAppsStartOne(c *check.C) {
	inst := servicestate.Instruction{Action: "start", Names: []string{"snap-a.svc2"}}
	expected := []serviceControlArgs{
		{action: "start", names: []string{"snap-a.svc2"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	chg := s.testPostApps(c, inst, expected)
	chg.State().Lock()
	defer chg.State().Unlock()

	var names []string
	err := chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Assert(names, check.DeepEquals, []string{"snap-a"})
}

func (s *appsSuite) TestPostAppsStartTwo(c *check.C) {
	inst := servicestate.Instruction{Action: "start", Names: []string{"snap-a"}}
	expected := []serviceControlArgs{
		{action: "start", names: []string{"snap-a.svc1", "snap-a.svc2"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	chg := s.testPostApps(c, inst, expected)

	chg.State().Lock()
	defer chg.State().Unlock()

	var names []string
	err := chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Assert(names, check.DeepEquals, []string{"snap-a"})
}

func (s *appsSuite) TestPostAppsStartThree(c *check.C) {
	inst := servicestate.Instruction{Action: "start", Names: []string{"snap-a", "snap-b"}}
	expected := []serviceControlArgs{
		{action: "start", names: []string{"snap-a.svc1", "snap-a.svc2", "snap-b.svc3"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	chg := s.testPostApps(c, inst, expected)
	// check the summary expands the snap into actual apps
	c.Check(chg.Summary(), check.Equals, "Running service command")
	chg.State().Lock()
	defer chg.State().Unlock()

	var names []string
	err := chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Assert(names, check.DeepEquals, []string{"snap-a", "snap-b"})
}

func (s *appsSuite) TestPostAppsStop(c *check.C) {
	inst := servicestate.Instruction{Action: "stop", Names: []string{"snap-a.svc2"}}
	expected := []serviceControlArgs{
		{action: "stop", names: []string{"snap-a.svc2"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsRestart(c *check.C) {
	inst := servicestate.Instruction{Action: "restart", Names: []string{"snap-a.svc2"}}
	expected := []serviceControlArgs{
		{action: "restart", names: []string{"snap-a.svc2"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsReload(c *check.C) {
	inst := servicestate.Instruction{Action: "restart", Names: []string{"snap-a.svc2"}}
	inst.Reload = true
	expected := []serviceControlArgs{
		{action: "restart", options: "reload", names: []string{"snap-a.svc2"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsEnableNow(c *check.C) {
	inst := servicestate.Instruction{Action: "start", Names: []string{"snap-a.svc2"}}
	inst.Enable = true
	expected := []serviceControlArgs{
		{action: "start", options: "enable", names: []string{"snap-a.svc2"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsDisableNow(c *check.C) {
	inst := servicestate.Instruction{Action: "stop", Names: []string{"snap-a.svc2"}}
	inst.Disable = true
	expected := []serviceControlArgs{
		{action: "stop", options: "disable", names: []string{"snap-a.svc2"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsFailedToGetUser(c *check.C) {
	r := daemon.MockSystemUserFromRequest(func(r *http.Request) (*user.User, error) {
		return nil, fmt.Errorf("failed")
	})
	defer r()

	req, err := http.NewRequest("POST", "/v2/apps", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, "cannot perform operation on services: failed")
}

func (s *appsSuite) TestPostAppsScopesSelfAsRootNotAllowed(c *check.C) {
	inst := servicestate.Instruction{
		Action: "start",
		Names:  []string{"snap-a.svc1"},
		Scope:  servicestate.ScopeSelector{"user"},
		Users: servicestate.UserSelector{
			Selector: servicestate.UserSelectionSelf,
		},
	}
	postBody, err := json.Marshal(inst)
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBuffer(postBody))
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, s.authUser)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, `cannot use "self" for root user`)
}

func (s *appsSuite) TestPostAppsAllUsersAsRootHappy(c *check.C) {
	inst := servicestate.Instruction{
		Action: "start",
		Names:  []string{"snap-a.svc1"},
		Scope:  servicestate.ScopeSelector{"user"},
		Users: servicestate.UserSelector{
			Selector: servicestate.UserSelectionAll,
		},
	}
	expected := []serviceControlArgs{
		// Expect no user to appear as we are not logged in
		{action: "start", names: []string{"snap-a.svc1"}, scope: servicestate.ScopeSelector{"user"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsScopesNotSpecifiedForRoot(c *check.C) {
	inst := servicestate.Instruction{Action: "start", Names: []string{"snap-e.svc4"}}
	expected := []serviceControlArgs{
		{action: "start", names: []string{"snap-e.svc4"}, scope: servicestate.ScopeSelector{"system", "user"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsUsersAsUserHappy(c *check.C) {
	inst := servicestate.Instruction{
		Action: "start",
		Names:  []string{"snap-a.svc1"},
		Scope:  servicestate.ScopeSelector{"user"},
		Users: servicestate.UserSelector{
			Selector: servicestate.UserSelectionAll,
		},
	}
	expected := []serviceControlArgs{
		{action: "start", names: []string{"snap-a.svc1"}, scope: servicestate.ScopeSelector{"user"}},
	}
	s.testPostAppsUser(c, inst, expected, "")
}

func (s *appsSuite) TestPostAppsScopesNotSpecifiedForUser(c *check.C) {
	inst := servicestate.Instruction{
		Action: "start",
		Names:  []string{"snap-e.svc4"},
		Users: servicestate.UserSelector{
			Selector: servicestate.UserSelectionSelf,
		},
	}
	s.testPostAppsUser(c, inst, nil, "cannot perform operation on services: non-root users must specify service scope when targeting user services")
}

func (s *appsSuite) TestPostAppsUsersUser(c *check.C) {
	inst := servicestate.Instruction{
		Action: "start",
		Names:  []string{"snap-a.svc1"},
		Users: servicestate.UserSelector{
			Selector: servicestate.UserSelectionSelf,
		},
	}
	expected := []serviceControlArgs{
		{action: "start", names: []string{"snap-a.svc1"}, scope: servicestate.ScopeSelector{"system"}, users: []string{"username"}},
	}
	s.testPostAppsUser(c, inst, expected, "")
}

func (s *appsSuite) TestPostAppsUsersWithUsernames(c *check.C) {
	inst := servicestate.Instruction{
		Action: "start",
		Names:  []string{"snap-a.svc1"},
		Users: servicestate.UserSelector{
			Names: []string{"my-user", "other-user"},
		},
	}
	expected := []serviceControlArgs{
		// Expect no user to appear as we are not logged in
		{action: "start", names: []string{"snap-a.svc1"}, scope: servicestate.ScopeSelector{"system", "user"}, users: []string{"my-user", "other-user"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsUserNotSpecifiedForRoot(c *check.C) {
	inst := servicestate.Instruction{Action: "start", Names: []string{"snap-e.svc4"}, Scope: servicestate.ScopeSelector{"system"}}
	expected := []serviceControlArgs{
		{action: "start", names: []string{"snap-e.svc4"}, scope: servicestate.ScopeSelector{"system"}},
	}
	s.testPostApps(c, inst, expected)
}

func (s *appsSuite) TestPostAppsUserNotSpecifiedForUser(c *check.C) {
	inst := servicestate.Instruction{Action: "start", Names: []string{"snap-e.svc4"}, Scope: servicestate.ScopeSelector{"user"}}
	s.testPostAppsUser(c, inst, nil, "cannot perform operation on services: non-root users must specify users when targeting user services")
}

func (s *appsSuite) TestPostAppsBadJSON(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBufferString(`'junk`))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, ".*cannot decode request body.*")
}

func (s *appsSuite) TestPostAppsBadOp(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBufferString(`{"random": "json"}`))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Matches, ".*cannot perform operation on services without a list of services.*")
}

func (s *appsSuite) TestPostAppsBadSnap(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBufferString(`{"action": "stop", "names": ["snap-c"]}`))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Message, check.Equals, `snap "snap-c" has no services`)
}

func (s *appsSuite) TestPostAppsBadApp(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBufferString(`{"action": "stop", "names": ["snap-a.what"]}`))
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Message, check.Equals, `snap "snap-a" has no service "what"`)
}

func (s *appsSuite) TestPostAppsServiceControlError(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBufferString(`{"action": "start", "names": ["snap-a.svc1"]}`))
	c.Assert(err, check.IsNil)
	s.serviceControlError = fmt.Errorf("total failure")
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `total failure`)
}

func (s *appsSuite) TestPostAppsConflict(c *check.C) {
	req, err := http.NewRequest("POST", "/v2/apps", bytes.NewBufferString(`{"action": "start", "names": ["snap-a.svc1"]}`))
	c.Assert(err, check.IsNil)
	s.serviceControlError = &snapstate.ChangeConflictError{Snap: "snap-a", ChangeKind: "enable"}
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Equals, `snap "snap-a" has "enable" change in progress`)
}

func (s *appsSuite) expectLogsAccess() {
	s.expectReadAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
}

func (s *appsSuite) TestLogs(c *check.C) {
	s.expectLogsAccess()

	s.jctlRCs = []io.ReadCloser{ioutil.NopCloser(strings.NewReader(`
{"MESSAGE": "hello1", "SYSLOG_IDENTIFIER": "xyzzy", "_PID": "42", "__REALTIME_TIMESTAMP": "42"}
{"MESSAGE": "hello2", "SYSLOG_IDENTIFIER": "xyzzy", "_PID": "42", "__REALTIME_TIMESTAMP": "44"}
{"MESSAGE": "hello3", "SYSLOG_IDENTIFIER": "xyzzy", "_PID": "42", "__REALTIME_TIMESTAMP": "46"}
{"MESSAGE": "hello4", "SYSLOG_IDENTIFIER": "xyzzy", "_PID": "42", "__REALTIME_TIMESTAMP": "48"}
{"MESSAGE": "hello5", "SYSLOG_IDENTIFIER": "xyzzy", "_PID": "42", "__REALTIME_TIMESTAMP": "50"}
	`))}

	req, err := http.NewRequest("GET", "/v2/logs?names=snap-a.svc2&n=42&follow=false", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	c.Check(s.jctlSvcses, check.DeepEquals, [][]string{{"snap.snap-a.svc2.service"}})
	c.Check(s.jctlNs, check.DeepEquals, []int{42})
	c.Check(s.jctlFollows, check.DeepEquals, []bool{false})
	c.Check(s.jctlNamespaces, check.DeepEquals, []bool{false})

	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json-seq")
	c.Check(rec.Body.String(), check.Equals, `
{"timestamp":"1970-01-01T00:00:00.000042Z","message":"hello1","sid":"xyzzy","pid":"42"}
{"timestamp":"1970-01-01T00:00:00.000044Z","message":"hello2","sid":"xyzzy","pid":"42"}
{"timestamp":"1970-01-01T00:00:00.000046Z","message":"hello3","sid":"xyzzy","pid":"42"}
{"timestamp":"1970-01-01T00:00:00.000048Z","message":"hello4","sid":"xyzzy","pid":"42"}
{"timestamp":"1970-01-01T00:00:00.00005Z","message":"hello5","sid":"xyzzy","pid":"42"}
`[1:])
}

func (s *appsSuite) TestLogsNoNamespaceOption(c *check.C) {
	restore := systemd.MockSystemdVersion(237, nil)
	defer restore()

	s.expectLogsAccess()

	s.jctlRCs = []io.ReadCloser{ioutil.NopCloser(strings.NewReader(""))}

	req, err := http.NewRequest("GET", "/v2/logs?names=snap-a.svc2&n=42&follow=false", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	c.Check(s.jctlSvcses, check.DeepEquals, [][]string{{"snap.snap-a.svc2.service"}})
	c.Check(s.jctlNs, check.DeepEquals, []int{42})
	c.Check(s.jctlFollows, check.DeepEquals, []bool{false})
	c.Check(s.jctlNamespaces, check.DeepEquals, []bool{false})

	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json-seq")
	c.Check(rec.Body.String(), check.Equals, "")
}

func (s *appsSuite) TestLogsWithNamespaceOption(c *check.C) {
	restore := systemd.MockSystemdVersion(245, nil)
	defer restore()

	s.expectLogsAccess()

	s.jctlRCs = []io.ReadCloser{ioutil.NopCloser(strings.NewReader(""))}

	req, err := http.NewRequest("GET", "/v2/logs?names=snap-a.svc2&n=42&follow=false", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, req, nil).ServeHTTP(rec, req)

	c.Check(s.jctlSvcses, check.DeepEquals, [][]string{{"snap.snap-a.svc2.service"}})
	c.Check(s.jctlNs, check.DeepEquals, []int{42})
	c.Check(s.jctlFollows, check.DeepEquals, []bool{false})
	c.Check(s.jctlNamespaces, check.DeepEquals, []bool{true})

	c.Check(rec.Code, check.Equals, 200)
	c.Check(rec.Header().Get("Content-Type"), check.Equals, "application/json-seq")
	c.Check(rec.Body.String(), check.Equals, "")
}

func (s *appsSuite) TestLogsN(c *check.C) {
	s.expectLogsAccess()

	type T struct {
		in  string
		out int
	}

	for _, t := range []T{
		{in: "", out: 10},
		{in: "0", out: 0},
		{in: "-1", out: -1},
		{in: strconv.Itoa(math.MinInt32), out: math.MinInt32},
		{in: strconv.Itoa(math.MaxInt32), out: math.MaxInt32},
	} {

		s.jctlRCs = []io.ReadCloser{ioutil.NopCloser(strings.NewReader(""))}
		s.jctlNs = nil

		req, err := http.NewRequest("GET", "/v2/logs?n="+t.in, nil)
		c.Assert(err, check.IsNil)

		rec := httptest.NewRecorder()
		s.req(c, req, nil).ServeHTTP(rec, req)

		c.Check(s.jctlNs, check.DeepEquals, []int{t.out})
	}
}

func (s *appsSuite) TestLogsBadN(c *check.C) {
	s.expectLogsAccess()

	req, err := http.NewRequest("GET", "/v2/logs?n=hello", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 400)
}

func (s *appsSuite) TestLogsFollow(c *check.C) {
	s.expectLogsAccess()

	s.jctlRCs = []io.ReadCloser{
		ioutil.NopCloser(strings.NewReader("")),
		ioutil.NopCloser(strings.NewReader("")),
		ioutil.NopCloser(strings.NewReader("")),
	}

	reqT, err := http.NewRequest("GET", "/v2/logs?follow=true", nil)
	c.Assert(err, check.IsNil)
	reqF, err := http.NewRequest("GET", "/v2/logs?follow=false", nil)
	c.Assert(err, check.IsNil)
	reqN, err := http.NewRequest("GET", "/v2/logs", nil)
	c.Assert(err, check.IsNil)

	rec := httptest.NewRecorder()
	s.req(c, reqT, nil).ServeHTTP(rec, reqT)
	s.req(c, reqF, nil).ServeHTTP(rec, reqF)
	s.req(c, reqN, nil).ServeHTTP(rec, reqN)

	c.Check(s.jctlFollows, check.DeepEquals, []bool{true, false, false})
}

func (s *appsSuite) TestLogsBadFollow(c *check.C) {
	s.expectLogsAccess()

	req, err := http.NewRequest("GET", "/v2/logs?follow=hello", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 400)
}

func (s *appsSuite) TestLogsBadName(c *check.C) {
	s.expectLogsAccess()

	req, err := http.NewRequest("GET", "/v2/logs?names=hello", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 404)
}

func (s *appsSuite) TestLogsSad(c *check.C) {
	s.expectLogsAccess()

	s.jctlErrs = []error{errors.New("potato")}
	req, err := http.NewRequest("GET", "/v2/logs", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 500)
}

func (s *appsSuite) TestLogsNoServices(c *check.C) {
	s.expectLogsAccess()

	// no installed snaps with services
	st := s.d.Overlord().State()
	st.Lock()
	st.Set("snaps", nil)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/logs", nil)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Assert(rspe.Status, check.Equals, 404)
}
