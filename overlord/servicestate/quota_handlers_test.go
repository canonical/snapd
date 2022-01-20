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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/servicestate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/systemd"
)

type quotaHandlersSuite struct {
	baseServiceMgrTestSuite
}

var _ = Suite(&quotaHandlersSuite{})

func (s *quotaHandlersSuite) SetUpTest(c *C) {
	s.baseServiceMgrTestSuite.SetUpTest(c)

	// we don't need the EnsureSnapServices ensure loop to run by default
	servicestate.MockEnsuredSnapServices(s.mgr, true)

	// we enable quota-groups by default
	s.state.Lock()
	defer s.state.Unlock()
	tr := config.NewTransaction(s.state)
	tr.Set("core", "experimental.quota-groups", true)
	tr.Commit()

	// mock that we have a new enough version of systemd by default
	r := systemd.MockSystemdVersion(248, nil)
	s.AddCleanup(r)
}

func (s *quotaHandlersSuite) testDoQuotaControlCreate(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// doQuotaControl handler to create the group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// make a fake task
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "create",
			QuotaName:   "foo-group",
			MemoryLimit: quantity.SizeGiB,
			AddSnaps:    []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()

	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo-group": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})
}
func (s *quotaHandlersSuite) testDoQuotaControlCreateRestartOK(c *C) {
	// test a situation where because of restart the task is reentered
	r := s.mockSystemctlCalls(c, join(
		// doQuotaControl handler to create the group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),
		// after task restart
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// make a fake task
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "create",
			QuotaName:   "foo-group",
			MemoryLimit: quantity.SizeGiB,
			AddSnaps:    []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)

	expectedQuotaState := map[string]quotaGroupState{
		"foo-group": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	}

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, expectedQuotaState)

	t.SetStatus(state.DoingStatus)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, expectedQuotaState)
}

func (s *quotaHandlersSuite) TestDoQuotaControlCreateRestartOKOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testDoQuotaControlCreateRestartOK(c)
}

func (s *quotaHandlersSuite) TestDoQuotaControlCreateRestartOKSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testDoQuotaControlCreateRestartOK(c)
}

func (s *quotaHandlersSuite) testQuotaStateAlreadyUpdatedBehavior(c *C) {
	// test a situation where because of restart the task is reentered
	r := s.mockSystemctlCalls(c, join(
		// doQuotaControl handler to create the group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// make a fake task
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "create",
			QuotaName:   "foo-group",
			MemoryLimit: quantity.SizeGiB,
			AddSnaps:    []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(t.Status(), Equals, state.DoneStatus)
	t.SetStatus(state.DoingStatus)

	updated, appsToRestart, err := servicestate.QuotaStateAlreadyUpdated(t)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	c.Assert(appsToRestart, HasLen, 1)
	for info, apps := range appsToRestart {
		c.Check(info.InstanceName(), Equals, "test-snap")
		c.Assert(apps, HasLen, 1)
		c.Check(apps[0], Equals, info.Apps["svc1"])
	}

	// rebooted
	r = servicestate.MockOsutilBootID("other-boot")
	defer r()

	updated, appsToRestart, err = servicestate.QuotaStateAlreadyUpdated(t)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	c.Check(appsToRestart, HasLen, 0)
	r()

	// restored
	_, appsToRestart, err = servicestate.QuotaStateAlreadyUpdated(t)
	c.Assert(err, IsNil)
	c.Check(appsToRestart, HasLen, 1)

	// snap went missing
	snapstate.Set(s.state, "test-snap", nil)
	updated, appsToRestart, err = servicestate.QuotaStateAlreadyUpdated(t)
	c.Assert(err, IsNil)
	c.Check(updated, Equals, true)
	c.Check(appsToRestart, HasLen, 0)
}

func (s *quotaHandlersSuite) TestQuotaStateAlreadyUpdatedBehaviorOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testQuotaStateAlreadyUpdatedBehavior(c)
}

func (s *quotaHandlersSuite) TestQuotaStateAlreadyUpdatedBehaviorSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testQuotaStateAlreadyUpdatedBehavior(c)
}

func (s *quotaHandlersSuite) TestDoQuotaControlCreateOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testDoQuotaControlCreate(c)
}

func (s *quotaHandlersSuite) TestDoQuotaControlCreateSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testDoQuotaControlCreate(c)
}

func (s *quotaHandlersSuite) testDoQuotaControlUpdate(c *C) {
	var expectedSdCalls []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.foo\\x2dgroup.slice"},
				output:  []string{"Id=snap.foo\\x2dgroup.slice\nNames=snap.foo\\x2dgroup.slice\nActiveState=active\nUnitFileState=\nType=\nNeedDaemonReload=no\n"},
			},
		}
	} else {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo-group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),

		// doQuotaControl handler which updates the group
		expectedSdCalls,
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "create",
			QuotaName:   "foo-group",
			MemoryLimit: quantity.SizeGiB,
			AddSnaps:    []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	// create a task for updating the quota group
	t = st.NewTask("update-quota", "...")

	// update the memory limit to be double
	qcs = []servicestate.QuotaControlAction{
		{
			Action:      "update",
			QuotaName:   "foo-group",
			MemoryLimit: 2 * quantity.SizeGiB,
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()

	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo-group": {
			MemoryLimit: 2 * quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestDoQuotaControlUpdateOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testDoQuotaControlUpdate(c)
}

func (s *quotaHandlersSuite) TestDoQuotaControlUpdateSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testDoQuotaControlUpdate(c)
}

func (s *quotaHandlersSuite) testDoQuotaControlUpdateRestartOK(c *C) {
	var expectedSdCalls []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.foo\\x2dgroup.slice"},
				output:  []string{"Id=snap.foo\\x2dgroup.slice\nNames=snap.foo\\x2dgroup.slice\nActiveState=inactive\nUnitFileState=Type=\nNeedDaemonReload=no\n"},
			},
		}
	} else {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}
	// test a situation where because of restart the task is reentered
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo-group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),

		// doQuotaControl handler which updates the group
		expectedSdCalls,
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "create",
			QuotaName:   "foo-group",
			MemoryLimit: quantity.SizeGiB,
			AddSnaps:    []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	// create a task for updating the quota group
	t = st.NewTask("update-quota", "...")

	// update the memory limit to be double
	qcs = []servicestate.QuotaControlAction{
		{
			Action:      "update",
			QuotaName:   "foo-group",
			MemoryLimit: 2 * quantity.SizeGiB,
		},
	}

	t.Set("quota-control-actions", &qcs)

	expectedQuotaState := map[string]quotaGroupState{
		"foo-group": {
			MemoryLimit: 2 * quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	}

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, expectedQuotaState)

	t.SetStatus(state.DoingStatus)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, expectedQuotaState)
}

func (s *quotaHandlersSuite) TestDoQuotaControlUpdateRestartOKOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testDoQuotaControlUpdateRestartOK(c)
}

func (s *quotaHandlersSuite) TestDoQuotaControlUpdateRestartOKSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testDoQuotaControlUpdateRestartOK(c)
}

func (s *quotaHandlersSuite) testDoQuotaControlRemove(c *C) {
	var expectedSdCalls1st []expectedSystemctl
	var expectedSdCalls2nd []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls1st = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
				output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=active\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
			},
		}
		expectedSdCalls2nd = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.foo\\x2dgroup.slice"},
				output:  []string{"Id=snap.foo\\x2dgroup.slice\nNames=snap.foo\\x2dgroup.slice\nActiveState=active\nUnitFileState=\nType=\nNeedDaemonReload=no\n"},
			},
		}
	} else {
		expectedSdCalls1st = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
		expectedSdCalls2nd = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}

	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo-group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),

		// doQuotaControl handler which removes the group
		expectedSdCalls1st,
		systemctlCallsForSliceStop("foo-group"),
		expectedSdCalls2nd,
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "create",
			QuotaName:   "foo-group",
			MemoryLimit: quantity.SizeGiB,
			AddSnaps:    []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	// create a task for removing the quota group
	t = st.NewTask("remove-quota", "...")

	// remove quota group
	qcs = []servicestate.QuotaControlAction{
		{
			Action:    "remove",
			QuotaName: "foo-group",
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()

	c.Assert(err, IsNil)
	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, nil)
}

func (s *quotaHandlersSuite) TestDoQuotaControlRemoveOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testDoQuotaControlRemove(c)
}

func (s *quotaHandlersSuite) TestDoQuotaControlRemoveSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testDoQuotaControlRemove(c)
}

func (s *quotaHandlersSuite) testDoQuotaControlRemoveRestartOK(c *C) {
	var expectedSdCalls1st []expectedSystemctl
	var expectedSdCalls2nd []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls1st = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
				output: []string{
					"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n",
				},
			},
		}
		expectedSdCalls2nd = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.foo\\x2dgroup.slice"},
				output:  []string{"Id=snap.foo\\x2dgroup.slice\nNames=snap.foo\\x2dgroup.slice\nActiveState=active\nUnitFileState=\nType=\nNeedDaemonReload=no\n"},
			},
		}
	} else {
		// doQuotaControl handler which removes the group
		expectedSdCalls1st = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
		expectedSdCalls2nd = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}
	// test a situation where because of restart the task is reentered
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo-group
		systemctlCallsForCreateQuota("foo-group", "test-snap"),

		// doQuotaControl handler which removes the group
		expectedSdCalls1st,
		systemctlCallsForSliceStop("foo-group"),
		expectedSdCalls2nd,
		systemctlCallsForServiceRestart("test-snap"),
		// after task restart
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	t := st.NewTask("create-quota", "...")

	qcs := []servicestate.QuotaControlAction{
		{
			Action:      "create",
			QuotaName:   "foo-group",
			MemoryLimit: quantity.SizeGiB,
			AddSnaps:    []string{"test-snap"},
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	// create a task for removing the quota group
	t = st.NewTask("remove-quota", "...")

	// remove quota group
	qcs = []servicestate.QuotaControlAction{
		{
			Action:    "remove",
			QuotaName: "foo-group",
		},
	}

	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, nil)

	t.SetStatus(state.DoingStatus)

	st.Unlock()
	err = s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()
	c.Assert(err, IsNil)

	c.Assert(t.Status(), Equals, state.DoneStatus)

	checkQuotaState(c, st, nil)
}

func (s *quotaHandlersSuite) TestDoQuotaControlRemoveRestartOKOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testDoQuotaControlRemoveRestartOK(c)
}

func (s *quotaHandlersSuite) TestDoQuotaControlRemoveRestartOKSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testDoQuotaControlRemoveRestartOK(c)
}

func (s *quotaHandlersSuite) callDoQuotaControl(action *servicestate.QuotaControlAction) error {
	st := s.state
	qcs := []*servicestate.QuotaControlAction{action}
	t := st.NewTask("quota-task", "...")
	t.Set("quota-control-actions", &qcs)

	st.Unlock()
	err := s.o.ServiceManager().DoQuotaControl(t, nil)
	st.Lock()

	return err
}

func (s *quotaHandlersSuite) TestQuotaCreatePreseeding(c *C) {
	// should be no systemctl calls since we are preseeding
	r := snapdenv.MockPreseeding(true)
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// now we can create the quota group
	qc := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: quantity.SizeGiB,
		AddSnaps:    []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) testQuotaCreate(c *C) {
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for non-installed snap - fails

		// CreateQuota for foo - success
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// CreateQuota for foo2 with overlapping snap already in foo

		// CreateQuota for foo again - fails
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// trying to create a quota with a snap that doesn't exist fails
	qc := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: quantity.SizeGiB,
		AddSnaps:    []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, ErrorMatches, `cannot use snap "test-snap" in group "foo": snap "test-snap" is not installed`)

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	qc2 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: 4 * quantity.SizeKiB,
		AddSnaps:    []string{"test-snap"},
	}

	// trying to create a quota with too low of a memory limit fails
	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, ErrorMatches, `memory limit for group "foo" is too small: size must be larger than 4KB`)

	// but with an adequately sized memory limit, and a snap that exists, we can
	// create it
	qc3 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: 4*quantity.SizeKiB + 1,
		AddSnaps:    []string{"test-snap"},
	}
	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: 4*quantity.SizeKiB + 1,
			Snaps:       []string{"test-snap"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaCreateOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testQuotaCreate(c)
}

func (s *quotaHandlersSuite) TestQuotaCreateSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testQuotaCreate(c)
}

func (s *quotaHandlersSuite) testDoCreateSubGroupQuota(c *C) {

	var expectedSdCalls []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service", "snap.foo\\x2dgroup.slice", "snap.foo\\x2dgroup-foo2.slice"},
				output: []string{
					"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=inactive\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n",
					"Id=snap.foo\\x2dgroup.slice\nNames=snap.foo\\x2dgroup.slice\nActiveState=inactive\nUnitFileState=Type=\nNeedDaemonReload=no\n",
					"Id=snap.foo\\x2dgroup-foo2.slice\nNames=snap.foo\\x2dgroup-foo2.slice\nActiveState=inactive\nUnitFileState=Type=\nNeedDaemonReload=no\n",
				},
			},
		}
	} else {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}
	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo - no systemctl calls since no snaps in it

		// CreateQuota for foo2 - fails thus no systemctl calls

		// CreateQuota for foo2 - we don't write anything for the first quota
		// since there are no snaps in the quota to track
		expectedSdCalls,
		systemctlCallsForSliceStart("foo-group", "foo-group/foo2"),
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group with no snaps to be the parent
	qc := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo-group",
		MemoryLimit: quantity.SizeGiB,
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// trying to create a quota group with a non-existent parent group fails
	qc2 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo2",
		MemoryLimit: quantity.SizeGiB,
		ParentName:  "foo-non-real",
		AddSnaps:    []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, ErrorMatches, `cannot create group under non-existent parent group "foo-non-real"`)

	// trying to create a quota group with too big of a limit to fit inside the
	// parent fails
	qc3 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo2",
		MemoryLimit: 2 * quantity.SizeGiB,
		ParentName:  "foo-group",
		AddSnaps:    []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, ErrorMatches, `sub-group memory limit of 2 GiB is too large to fit inside remaining quota space 1 GiB for parent group foo-group`)

	// now we can create a sub-quota
	qc4 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo2",
		MemoryLimit: quantity.SizeGiB,
		ParentName:  "foo-group",
		AddSnaps:    []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc4)
	c.Assert(err, IsNil)

	// check that the quota groups were created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo-group": {
			MemoryLimit: quantity.SizeGiB,
			SubGroups:   []string{"foo2"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
			ParentGroup: "foo-group",
		},
	})

	// foo-group exists as a slice too, but has no snap services in the slice
	checkSliceState(c, systemd.EscapeUnitNamePath("foo-group"), quantity.SizeGiB)
}

func (s *quotaHandlersSuite) TestDoCreateSubGroupQuotaOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testDoCreateSubGroupQuota(c)

}

func (s *quotaHandlersSuite) TestDoCreateSubGroupQuotaSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testDoCreateSubGroupQuota(c)
}

func (s *quotaHandlersSuite) testDoQuotaRemove(c *C) {
	var expectedSdCalls1st []expectedSystemctl
	var expectedSdCalls2nd []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls1st = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap.svc1.service"},
				output:  []string{"Id=snap.test-snap.svc1.service\nNames=snap.test-snap.svc1.service\nActiveState=active\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
			},
		}
		expectedSdCalls2nd = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.foo.slice"},
				output:  []string{"Id=snap.foo.slice\nNames=snap.foo.slice\nActiveState=active\nUnitFileState=\nType=\nNeedDaemonReload=no\n"},
			},
		}
	} else {
		expectedSdCalls1st = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
		expectedSdCalls2nd = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}

	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// for CreateQuota foo2 - no systemctl calls since there are no snaps

		// for CreateQuota foo3 - no systemctl calls since there are no snaps

		// RemoveQuota for foo2 - no daemon reload initially because
		// we didn't modify anything, as there are no snaps in foo2 so we don't
		// create that group on disk
		// TODO: is this bit correct in practice? we are in effect calling
		// systemctl stop <non-existing-slice> ?
		systemctlCallsForSliceStop("foo/foo3"),

		systemctlCallsForSliceStop("foo/foo2"),

		// RemoveQuota for foo
		expectedSdCalls1st,
		systemctlCallsForSliceStop("foo"),
		expectedSdCalls2nd,
		systemctlCallsForServiceRestart("test-snap"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// trying to remove a group that does not exist fails
	qc := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "not-exists",
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, ErrorMatches, `cannot remove non-existent quota group "not-exists"`)

	qc2 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: quantity.SizeGiB,
		AddSnaps:    []string{"test-snap"},
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// create 2 quota sub-groups too
	qc3 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo2",
		MemoryLimit: quantity.SizeGiB / 2,
		ParentName:  "foo",
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, IsNil)

	qc4 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo3",
		MemoryLimit: quantity.SizeGiB / 2,
		ParentName:  "foo",
	}

	err = s.callDoQuotaControl(&qc4)
	c.Assert(err, IsNil)

	// check that the quota groups was created in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
			SubGroups:   []string{"foo2", "foo3"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB / 2,
			ParentGroup: "foo",
		},
		"foo3": {
			MemoryLimit: quantity.SizeGiB / 2,
			ParentGroup: "foo",
		},
	})

	// try removing the parent and it fails since it still has a sub-group
	// under it
	qc5 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo",
	}

	err = s.callDoQuotaControl(&qc5)
	c.Assert(err, ErrorMatches, "cannot remove quota group with sub-groups, remove the sub-groups first")

	// but we can remove the sub-group successfully first
	qc6 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo3",
	}

	err = s.callDoQuotaControl(&qc6)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
			SubGroups:   []string{"foo2"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB / 2,
			ParentGroup: "foo",
		},
	})

	// and we can remove the other sub-group
	qc7 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo2",
	}

	err = s.callDoQuotaControl(&qc7)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// now we can remove the quota from the state
	qc8 := servicestate.QuotaControlAction{
		Action:    "remove",
		QuotaName: "foo",
	}

	err = s.callDoQuotaControl(&qc8)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, nil)

	// foo is not mentioned in the service and doesn't exist
	checkSvcAndSliceState(c, "test-snap.svc1", "foo", 0)
}

func (s *quotaHandlersSuite) TestDoQuotaRemoveOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testDoQuotaRemove(c)
}

func (s *quotaHandlersSuite) TestQuotaRemoveSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testDoQuotaRemove(c)
}

func (s *quotaHandlersSuite) TestQuotaUpdateGroupNotExist(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// non-existent quota group
	qc := servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "non-existing",
	}

	err := s.callDoQuotaControl(&qc)
	c.Check(err, ErrorMatches, `group "non-existing" does not exist`)
}

func (s *quotaHandlersSuite) testQuotaUpdateSubGroupTooBig(c *C) {
	var expectedSdCalls []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.foo-foo2.slice"},
				output:  []string{"Id=snap.foo-foo2.slice\nNames=snap.foo-foo2.slice\nActiveState=active\nUnitFileState=\nType=\nNeedDaemonReload=no\n"},
			},
		}
	} else {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}

	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// CreateQuota for foo2
		systemctlCallsForCreateQuota("foo/foo2", "test-snap2"),

		// UpdateQuota for foo2 - just the slice changes
		expectedSdCalls,

		// UpdateQuota for foo2 which fails - no systemctl calls
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: quantity.SizeGiB,
		AddSnaps:    []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// ensure mem-limit is 1 GB
	expFooGroupState := quotaGroupState{
		MemoryLimit: quantity.SizeGiB,
		Snaps:       []string{"test-snap"},
	}
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": expFooGroupState,
	})

	// create a sub-group with 0.5 GiB
	qc2 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo2",
		MemoryLimit: quantity.SizeGiB / 2,
		AddSnaps:    []string{"test-snap2"},
		ParentName:  "foo",
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	expFooGroupState.SubGroups = []string{"foo2"}

	expFoo2GroupState := quotaGroupState{
		MemoryLimit: quantity.SizeGiB / 2,
		Snaps:       []string{"test-snap2"},
		ParentGroup: "foo",
	}

	// verify it was set in state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})

	// now try to increase it to the max size
	qc3 := servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		MemoryLimit: quantity.SizeGiB,
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, IsNil)

	expFoo2GroupState.MemoryLimit = quantity.SizeGiB
	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})

	// now try to increase it above the parent limit
	qc4 := servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo2",
		MemoryLimit: 2 * quantity.SizeGiB,
	}

	err = s.callDoQuotaControl(&qc4)
	c.Assert(err, ErrorMatches, `cannot update quota "foo2": group "foo2" is invalid: sub-group memory limit of 2 GiB is too large to fit inside remaining quota space 1 GiB for parent group foo`)

	// and make sure that the existing memory limit is still in place
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo":  expFooGroupState,
		"foo2": expFoo2GroupState,
	})
}

func (s *quotaHandlersSuite) TestQuotaUpdateSubGroupTooBigOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testQuotaUpdateSubGroupTooBig(c)
}

func (s *quotaHandlersSuite) TestQuotaUpdateSubGroupTooBigSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testQuotaUpdateSubGroupTooBig(c)
}

func (s *quotaHandlersSuite) testQuotaUpdateChangeMemLimit(c *C) {
	var expectedSdCalls []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.foo.slice"},
				output:  []string{"Id=snap.foo.slice\nNames=snap.foo.slice\nActiveState=active\nUnitFileState=\nType=\nNeedDaemonReload=no\n"},
			},
		}
	} else {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}

	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// UpdateQuota for foo - an existing slice was changed, so all we need
		// to is daemon-reload
		expectedSdCalls,
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup the snap so it exists
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: quantity.SizeGiB,
		AddSnaps:    []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	// ensure mem-limit is 1 GB
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// modify to 2 GB
	qc2 := servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo",
		MemoryLimit: 2 * quantity.SizeGiB,
	}
	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: 2 * quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// trying to decrease the memory limit is not yet supported
	qc3 := servicestate.QuotaControlAction{
		Action:      "update",
		QuotaName:   "foo",
		MemoryLimit: quantity.SizeGiB,
	}
	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, ErrorMatches, "cannot decrease memory limit of existing quota-group, remove and re-create it to decrease the limit")
}

func (s *quotaHandlersSuite) TestQuotaUpdateChangeMemLimitOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testQuotaUpdateChangeMemLimit(c)
}

func (s *quotaHandlersSuite) TestQuotaUpdateChangeMemLimitSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testQuotaUpdateChangeMemLimit(c)
}

func (s *quotaHandlersSuite) testQuotaUpdateAddSnap(c *C) {
	var expectedSdCalls []expectedSystemctl
	if "ubuntu-core" == release.ReleaseInfo.ID && release.ReleaseInfo.VersionID == "20" {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"show", "--property=Id,ActiveState,UnitFileState,Type,Names,NeedDaemonReload", "snap.test-snap2.svc1.service"},
				output:  []string{"Id=snap.test-snap2.svc1.service\nNames=snap.test-snap2.svc1.service\nActiveState=active\nUnitFileState=enabled\nType=simple\nNeedDaemonReload=no\n"},
			},
		}
	} else {
		expectedSdCalls = []expectedSystemctl{
			{
				expArgs: []string{"daemon-reload"},
			},
		}
	}

	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// UpdateQuota with just test-snap2 restarted since the group already
		// exists
		expectedSdCalls,
		systemctlCallsForServiceRestart("test-snap2"),
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: quantity.SizeGiB,
		AddSnaps:    []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// add a snap
	qc2 := servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo",
		AddSnaps:  []string{"test-snap2"},
	}
	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// and check that it got updated in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap", "test-snap2"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaUpdateAddSnapOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testQuotaUpdateAddSnap(c)
}

func (s *quotaHandlersSuite) TestQuotaUpdateAddSnapSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testQuotaUpdateAddSnap(c)
}

func (s *quotaHandlersSuite) testQuotaUpdateAddSnapAlreadyInOtherGroup(c *C) {

	r := s.mockSystemctlCalls(c, join(
		// CreateQuota for foo
		systemctlCallsForCreateQuota("foo", "test-snap"),

		// CreateQuota for foo2
		systemctlCallsForCreateQuota("foo2", "test-snap2"),

		// UpdateQuota for foo which fails - no systemctl calls
	))
	defer r()

	st := s.state
	st.Lock()
	defer st.Unlock()

	// setup test-snap
	snapstate.Set(s.state, "test-snap", s.testSnapState)
	snaptest.MockSnapCurrent(c, testYaml, s.testSnapSideInfo)
	// and test-snap2
	si2 := &snap.SideInfo{RealName: "test-snap2", Revision: snap.R(42)}
	snapst2 := &snapstate.SnapState{
		Sequence: []*snap.SideInfo{si2},
		Current:  si2.Revision,
		Active:   true,
		SnapType: "app",
	}
	snapstate.Set(s.state, "test-snap2", snapst2)
	snaptest.MockSnapCurrent(c, testYaml2, si2)

	// create a quota group
	qc := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo",
		MemoryLimit: quantity.SizeGiB,
		AddSnaps:    []string{"test-snap"},
	}

	err := s.callDoQuotaControl(&qc)
	c.Assert(err, IsNil)

	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
	})

	// create another quota group with the second snap
	qc2 := servicestate.QuotaControlAction{
		Action:      "create",
		QuotaName:   "foo2",
		MemoryLimit: quantity.SizeGiB,
		AddSnaps:    []string{"test-snap2"},
	}

	err = s.callDoQuotaControl(&qc2)
	c.Assert(err, IsNil)

	// verify state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap2"},
		},
	})

	// try to add test-snap2 to foo
	qc3 := servicestate.QuotaControlAction{
		Action:    "update",
		QuotaName: "foo",
		AddSnaps:  []string{"test-snap2"},
	}

	err = s.callDoQuotaControl(&qc3)
	c.Assert(err, ErrorMatches, `cannot add snap "test-snap2" to group "foo": snap already in quota group "foo2"`)

	// nothing changed in the state
	checkQuotaState(c, st, map[string]quotaGroupState{
		"foo": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap"},
		},
		"foo2": {
			MemoryLimit: quantity.SizeGiB,
			Snaps:       []string{"test-snap2"},
		},
	})
}

func (s *quotaHandlersSuite) TestQuotaUpdateAddSnapAlreadyInOtherGroupOldSystemd(c *C) {
	releaseRestore := release.MockOnClassic(true)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu", VersionID: "14.04"})
	defer releaseRestore()

	s.testQuotaUpdateAddSnapAlreadyInOtherGroup(c)
}

func (s *quotaHandlersSuite) TestQuotaUpdateAddSnapAlreadyInOtherGroupSmartSystemd(c *C) {
	releaseRestore := release.MockOnClassic(false)
	defer releaseRestore()

	releaseRestore = release.MockReleaseInfo(&release.OS{ID: "ubuntu-core", VersionID: "20"})
	defer releaseRestore()

	s.testQuotaUpdateAddSnapAlreadyInOtherGroup(c)
}
