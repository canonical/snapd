// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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

package registrystate_test

import (
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/registrystate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type registryTestSuite struct {
	state *state.State
	o     *overlord.Overlord

	registry *registry.Registry
	devAccID string

	repo *interfaces.Repository
}

var _ = Suite(&registryTestSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *registryTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.o = overlord.Mock()
	s.state = s.o.State()

	s.state.Lock()
	defer s.state.Unlock()

	runner := s.o.TaskRunner()
	s.o.AddManager(runner)

	hookMgr, err := hookstate.Manager(s.state, runner)
	c.Assert(err, IsNil)
	s.o.AddManager(hookMgr)

	// to test the registryManager
	mgr := registrystate.Manager(s.state, hookMgr, runner)
	s.o.AddManager(mgr)

	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	c.Assert(db.Add(storeSigning.StoreAccountKey("")), IsNil)
	assertstate.ReplaceDB(s.state, db)

	// add developer1's account and account-key assertions
	devAcc := assertstest.NewAccount(storeSigning, "developer1", nil, "")
	c.Assert(storeSigning.Add(devAcc), IsNil)

	devPrivKey, _ := assertstest.GenerateKey(752)
	devAccKey := assertstest.NewAccountKey(storeSigning, devAcc, nil, devPrivKey.PublicKey(), "")

	assertstatetest.AddMany(s.state, storeSigning.StoreAccountKey(""), devAcc, devAccKey)

	signingDB := assertstest.NewSigningDB("developer1", devPrivKey)
	c.Check(signingDB, NotNil)
	c.Assert(storeSigning.Add(devAccKey), IsNil)

	headers := map[string]interface{}{
		"authority-id": devAccKey.AccountID(),
		"account-id":   devAccKey.AccountID(),
		"name":         "network",
		"views": map[string]interface{}{
			"setup-wifi": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssids", "storage": "wifi.ssids"},
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid", "access": "read-write"},
					map[string]interface{}{"request": "password", "storage": "wifi.psk", "access": "write"},
					map[string]interface{}{"request": "status", "storage": "wifi.status", "access": "read"},
					map[string]interface{}{"request": "private.{placeholder}", "storage": "private.{placeholder}"},
				},
			},
		},
		"timestamp": "2030-11-06T09:16:26Z",
	}
	body := []byte(`{
  "storage": {
    "schema": {
      "private": {
        "values": "any"
      },
      "wifi": {
        "schema": {
          "psk": "string",
          "ssid": "string",
          "ssids": {
            "type": "array",
            "values": "any"
          },
          "status": "string"
        }
      }
    }
  }
}`)

	as, err := signingDB.Sign(asserts.RegistryType, headers, body, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(s.state, as), IsNil)

	s.devAccID = devAccKey.AccountID()
	s.registry = as.(*asserts.Registry).Registry()
}

func (s *registryTestSuite) TestGetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := registry.NewJSONDataBag()
	err := databag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)
	s.state.Set("registry-databags", map[string]map[string]registry.JSONDataBag{s.devAccID: {"network": databag}})

	res, err := registrystate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *registryTestSuite) TestGetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	res, err := registrystate.Get(s.state, s.devAccID, "network", "other-view", []string{"ssid"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" in registry view %s/network/other-view: not found`, s.devAccID))
	c.Check(res, IsNil)

	res, err = registrystate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" in registry view %s/network/setup-wifi: matching rules don't map to any values`, s.devAccID))
	c.Check(res, IsNil)

	res, err = registrystate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"other-field"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "other-field" in registry view %s/network/setup-wifi: no matching read rule`, s.devAccID))
	c.Check(res, IsNil)
}

func (s *registryTestSuite) TestSetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := registrystate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "foo"})
	c.Assert(err, IsNil)

	var databags map[string]map[string]registry.JSONDataBag
	err = s.state.Get("registry-databags", &databags)
	c.Assert(err, IsNil)

	val, err := databags[s.devAccID]["network"].Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "foo")
}

func (s *registryTestSuite) TestSetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := registrystate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"foo": "bar"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" in registry view %s/network/setup-wifi: no matching write rule`, s.devAccID))

	err = registrystate.Set(s.state, s.devAccID, "network", "other-view", map[string]interface{}{"foo": "bar"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" in registry view %s/network/other-view: not found`, s.devAccID))
}

func (s *registryTestSuite) TestUnsetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := registry.NewJSONDataBag()
	err := registrystate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "foo"})
	c.Assert(err, IsNil)

	err = registrystate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": nil})
	c.Assert(err, IsNil)

	val, err := databag.Get("wifi.ssid")
	c.Assert(err, FitsTypeOf, registry.PathError(""))
	c.Assert(val, Equals, nil)
}

func (s *registryTestSuite) TestRegistrystateSetWithExistingState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := registry.NewJSONDataBag()
	err := bag.Set("wifi.ssid", "bar")
	c.Assert(err, IsNil)
	databags := map[string]map[string]registry.JSONDataBag{
		s.devAccID: {"network": bag},
	}

	s.state.Set("registry-databags", databags)

	results, err := registrystate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, IsNil)
	resultsMap, ok := results.(map[string]interface{})
	c.Assert(ok, Equals, true)
	c.Assert(resultsMap["ssid"], Equals, "bar")

	err = registrystate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "baz"})
	c.Assert(err, IsNil)

	err = s.state.Get("registry-databags", &databags)
	c.Assert(err, IsNil)
	value, err := databags[s.devAccID]["network"].Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *registryTestSuite) TestRegistrystateSetWithNoState(c *C) {
	type testcase struct {
		state map[string]map[string]registry.JSONDataBag
	}

	testcases := []testcase{
		{
			state: map[string]map[string]registry.JSONDataBag{
				s.devAccID: {"network": nil},
			},
		},
		{
			state: map[string]map[string]registry.JSONDataBag{
				s.devAccID: nil,
			},
		},
		{
			state: map[string]map[string]registry.JSONDataBag{},
		},
		{
			state: nil,
		},
	}

	s.state.Lock()
	defer s.state.Unlock()
	for _, tc := range testcases {
		s.state.Set("registry-databags", tc.state)

		err := registrystate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "bar"})
		c.Assert(err, IsNil)

		var databags map[string]map[string]registry.JSONDataBag
		err = s.state.Get("registry-databags", &databags)
		c.Assert(err, IsNil)

		value, err := databags[s.devAccID]["network"].Get("wifi.ssid")
		c.Assert(err, IsNil)
		c.Assert(value, Equals, "bar")
	}
}

func (s *registryTestSuite) TestRegistrystateGetEntireView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := registrystate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{
		"ssids":    []interface{}{"foo", "bar"},
		"password": "pass",
		"private": map[string]interface{}{
			"a": 1,
			"b": 2,
		},
	})
	c.Assert(err, IsNil)

	res, err := registrystate.Get(s.state, s.devAccID, "network", "setup-wifi", nil)
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]interface{}{
		"ssids": []interface{}{"foo", "bar"},
		"private": map[string]interface{}{
			"a": float64(1),
			"b": float64(2),
		},
	})
}

func (s *registryTestSuite) TestRegistryTransaction(c *C) {
	mkRegistry := func(account, name string) *registry.Registry {
		reg, err := registry.New(account, name, map[string]interface{}{
			"bar": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "foo", "storage": "foo"},
				},
			},
		}, registry.NewJSONSchema())
		c.Assert(err, IsNil)
		return reg
	}

	s.state.Lock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	s.state.Unlock()
	mockHandler := hooktest.NewMockHandler()

	type testcase struct {
		acc1, acc2 string
		reg1, reg2 string
		equals     bool
	}

	tcs := []testcase{
		{
			// same transaction
			acc1: "acc-1", reg1: "reg-1",
			acc2: "acc-1", reg2: "reg-1",
			equals: true,
		},
		{
			// different registry name, different transaction
			acc1: "acc-1", reg1: "reg-1",
			acc2: "acc-1", reg2: "reg-2",
		},
		{
			// different account, different transaction
			acc1: "acc-1", reg1: "reg-1",
			acc2: "acc-2", reg2: "reg-1",
		},
		{
			// both different, different transaction
			acc1: "acc-1", reg1: "reg-1",
			acc2: "acc-2", reg2: "reg-2",
		},
	}

	for _, tc := range tcs {
		ctx, err := hookstate.NewContext(task, task.State(), setup, mockHandler, "")
		c.Assert(err, IsNil)
		ctx.Lock()

		reg1 := mkRegistry(tc.acc1, tc.reg1)
		reg2 := mkRegistry(tc.acc2, tc.reg2)

		tx1, err := registrystate.RegistryTransaction(ctx, reg1)
		c.Assert(err, IsNil)

		tx2, err := registrystate.RegistryTransaction(ctx, reg2)
		c.Assert(err, IsNil)

		if tc.equals {
			c.Assert(tx1, Equals, tx2)
		} else {
			c.Assert(tx1, Not(Equals), tx2)
		}
		ctx.Unlock()
	}
}

func mockInstalledSnap(c *C, st *state.State, snapYaml, cohortKey string) *snap.Info {
	info := snaptest.MockSnapCurrent(c, snapYaml, &snap.SideInfo{Revision: snap.R(1)})
	snapstate.Set(st, info.InstanceName(), &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{
				RealName: info.SnapName(),
				Revision: info.Revision,
				SnapID:   info.InstanceName() + "-id",
			},
		}),
		Current:         info.Revision,
		TrackingChannel: "stable",
		CohortKey:       cohortKey,
	})
	return info
}

func (s *registryTestSuite) TestPlugsAffectedByPaths(c *C) {
	reg, err := registry.New(s.devAccID, "reg", map[string]interface{}{
		// exact match
		"view-1": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.bar", "storage": "foo.bar"},
			},
		},
		// unrelated
		"view-2": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
		// more specific
		"view-3": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.bar.baz", "storage": "foo.bar.baz"},
			},
		},
		// more generic but we won't connect a plug for this view
		"view-4": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	repo := interfaces.NewRepository()
	s.state.Lock()
	defer s.state.Unlock()
	ifacerepo.Replace(s.state, repo)

	regIface := &ifacetest.TestInterface{InterfaceName: "registry"}
	err = repo.AddInterface(regIface)
	c.Assert(err, IsNil)

	snapYaml := fmt.Sprintf(`name: test-snap
version: 1
type: app
plugs:
  view-1:
    interface: registry
    account: %[1]s
    view: reg/view-1
  view-2:
    interface: registry
    account: %[1]s
    view: reg/view-2
  view-3:
    interface: registry
    account: %[1]s
    view: reg/view-3
  view-4:
    interface: registry
    account: %[1]s
    view: reg/view-4
`, s.devAccID)
	info := mockInstalledSnap(c, s.state, snapYaml, "")

	appSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1
type: os
slots:
 registry-slot:
  interface: registry
`
	info = mockInstalledSnap(c, s.state, coreYaml, "")

	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	for _, n := range []string{"view-1", "view-2", "view-3"} {
		ref := &interfaces.ConnRef{
			PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: n},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "registry-slot"},
		}
		_, err = repo.Connect(ref, nil, nil, nil, nil, nil)
		c.Assert(err, IsNil)
	}

	snapPlugs, err := registrystate.GetPlugsAffectedByPaths(s.state, reg, []string{"foo"})
	c.Assert(err, IsNil)
	c.Assert(snapPlugs, HasLen, 1)

	plugNames := make([]string, 0, len(snapPlugs["test-snap"]))
	for _, plug := range snapPlugs["test-snap"] {
		plugNames = append(plugNames, plug.Name)
	}
	c.Assert(plugNames, testutil.DeepUnsortedMatches, []string{"view-1", "view-3"})
}

func (s *registryTestSuite) TestRegistryTasksUserSetWithCustodianInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	s.setupRegistryModificationScenario(c, []string{"custodian-snap"}, nil)

	tx, err := registrystate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.registry.View("setup-wifi")
	chg := s.state.NewChange("modify-registry", "")

	// a user (not a snap) changes a registry
	err = registrystate.CreateChangeRegistryTasks(s.state, chg, tx, view, "")
	c.Assert(err, IsNil)

	// the custodian snap's hooks are run
	tasks := []string{"clear-registry-tx-on-error", "run-hook", "run-hook", "run-hook", "commit-registry-tx", "clear-registry-tx"}
	hooks := []*hookstate.HookSetup{
		{
			Snap:        "custodian-snap",
			Hook:        "change-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
		{
			Snap:        "custodian-snap",
			Hook:        "save-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
		{
			Snap:        "custodian-snap",
			Hook:        "setup-view-changed",
			Optional:    true,
			IgnoreError: true,
		},
	}

	checkModifyRegistryTasks(c, chg, tasks, hooks)
}

func (s *registryTestSuite) TestRegistryTasksCustodianSnapSet(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	s.setupRegistryModificationScenario(c, []string{"custodian-snap"}, nil)

	tx, err := registrystate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.registry.View("setup-wifi")
	chg := s.state.NewChange("modify-registry", "")

	// a user (not a snap) changes a registry
	err = registrystate.CreateChangeRegistryTasks(s.state, chg, tx, view, "custodian-snap")
	c.Assert(err, IsNil)

	// the custodian snap's hooks are run
	tasks := []string{"clear-registry-tx-on-error", "run-hook", "run-hook", "commit-registry-tx", "clear-registry-tx"}
	hooks := []*hookstate.HookSetup{
		{
			Snap:        "custodian-snap",
			Hook:        "change-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
		{
			Snap:        "custodian-snap",
			Hook:        "save-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
	}

	checkModifyRegistryTasks(c, chg, tasks, hooks)
}

func (s *registryTestSuite) TestRegistryTasksObserverSnapSetWithCustodianInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// one custodian and several non-custodians are installed
	s.setupRegistryModificationScenario(c, []string{"custodian-snap"}, []string{"test-snap-1", "test-snap-2"})

	tx, err := registrystate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.registry.View("setup-wifi")
	chg := s.state.NewChange("modify-registry", "")

	// a non-custodian snap modifies a registry
	err = registrystate.CreateChangeRegistryTasks(s.state, chg, tx, view, "test-snap-1")
	c.Assert(err, IsNil)

	// we trigger hooks for the custodian snap and for the -view-changed for the
	// observer snap that didn't trigger the change
	tasks := []string{"clear-registry-tx-on-error", "run-hook", "run-hook", "run-hook", "run-hook", "commit-registry-tx", "clear-registry-tx"}
	hooks := []*hookstate.HookSetup{
		{
			Snap:        "custodian-snap",
			Hook:        "change-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
		{
			Snap:        "custodian-snap",
			Hook:        "save-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
		{
			Snap:        "custodian-snap",
			Hook:        "setup-view-changed",
			Optional:    true,
			IgnoreError: true,
		},
		{
			Snap:        "test-snap-2",
			Hook:        "setup-view-changed",
			Optional:    true,
			IgnoreError: true,
		},
	}

	checkModifyRegistryTasks(c, chg, tasks, hooks)
}

func (s *registryTestSuite) TestRegistryTasksDisconnectedCustodianSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// mock and installed custodian-snap but disconnect it
	s.setupRegistryModificationScenario(c, []string{"test-custodian-snap"}, []string{"test-snap"})
	s.repo.Disconnect("test-custodian-snap", "setup", "core", "registry-slot")
	s.testRegistryTasksNoCustodian(c)
}

func (s *registryTestSuite) TestRegistryTasksNoCustodianSnapInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no custodian snap is installed
	s.setupRegistryModificationScenario(c, nil, []string{"test-snap"})
	s.testRegistryTasksNoCustodian(c)
}

func (s *registryTestSuite) testRegistryTasksNoCustodian(c *C) {
	tx, err := registrystate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.registry.View("setup-wifi")
	chg := s.state.NewChange("modify-registry", "")

	// a non-custodian snap modifies a registry
	err = registrystate.CreateChangeRegistryTasks(s.state, chg, tx, view, "test-snap-1")
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot commit changes to registry %s/network: no custodian snap installed", s.devAccID))
}

func (s *registryTestSuite) setupRegistryModificationScenario(c *C, custodians, nonCustodians []string) {
	s.repo = interfaces.NewRepository()
	ifacerepo.Replace(s.state, s.repo)

	regIface := &ifacetest.TestInterface{InterfaceName: "registry"}
	err := s.repo.AddInterface(regIface)
	c.Assert(err, IsNil)

	// mock the registry slot
	const coreYaml = `name: core
version: 1
type: os
slots:
  registry-slot:
    interface: registry
`
	info := mockInstalledSnap(c, s.state, coreYaml, "")

	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = s.repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	mockSnap := func(snapName string, isCustodian bool) {
		snapYaml := fmt.Sprintf(`name: %s
version: 1
type: app
plugs:
  setup:
    interface: registry
    account: %s
    view: network/setup-wifi
`, snapName, s.devAccID)

		if isCustodian {
			snapYaml +=
				`    role: custodian`
		}

		info := mockInstalledSnap(c, s.state, snapYaml, "")

		// by default, mock all the hooks a custodians can have
		for _, hookName := range []string{"change-view-setup", "save-view-setup", "setup-view-changed"} {
			info.Hooks[hookName] = &snap.HookInfo{
				Name: hookName,
				Snap: info,
			}
		}

		appSet, err := interfaces.NewSnapAppSet(info, nil)
		c.Assert(err, IsNil)
		err = s.repo.AddAppSet(appSet)
		c.Assert(err, IsNil)

		ref := &interfaces.ConnRef{
			PlugRef: interfaces.PlugRef{Snap: snapName, Name: "setup"},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "registry-slot"},
		}
		_, err = s.repo.Connect(ref, nil, nil, nil, nil, nil)
		c.Assert(err, IsNil)
	}

	// mock custodians
	for _, snap := range custodians {
		isCustodian := true
		mockSnap(snap, isCustodian)
	}

	// mock non-custodians
	for _, snap := range nonCustodians {
		isCustodian := false
		mockSnap(snap, isCustodian)
	}
}

func checkModifyRegistryTasks(c *C, chg *state.Change, taskKinds []string, hooksups []*hookstate.HookSetup) {
	c.Assert(chg.Tasks(), HasLen, len(taskKinds))
	commitTask := findTask(chg, "commit-registry-tx")

	// commit task carries the transaction
	var tx *registrystate.Transaction
	err := commitTask.Get("registry-transaction", &tx)
	c.Assert(err, IsNil)
	c.Assert(tx, NotNil)

	t := findTask(chg, "clear-registry-tx-on-error")
	var hookIndex int
	var i int
loop:
	for ; t != nil; i++ {
		c.Assert(t.Kind(), Equals, taskKinds[i])
		if t.Kind() == "run-hook" {
			c.Assert(getHookSetup(c, t), DeepEquals, hooksups[hookIndex])
			hookIndex++
		}

		if t.Kind() != "commit-registry-tx" {
			// all tasks (other than the commit) are linked to the commit task
			var id string
			err := t.Get("commit-task", &id)
			c.Assert(err, IsNil)
			c.Assert(id, Equals, commitTask.ID())
		}

		switch len(t.HaltTasks()) {
		case 0:
			break loop
		case 1:
			t = t.HaltTasks()[0]
		}
	}

	c.Assert(i, Equals, len(taskKinds)-1)
}

func getHookSetup(c *C, t *state.Task) *hookstate.HookSetup {
	var sup *hookstate.HookSetup
	err := t.Get("hook-setup", &sup)
	c.Assert(err, IsNil)
	return sup
}

func findTask(chg *state.Change, kind string) *state.Task {
	for _, t := range chg.Tasks() {
		if t.Kind() == kind {
			return t
		}
	}

	return nil
}

func (s *registryTestSuite) TestGetStoredTransaction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tx, err := registrystate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("some-change", "")
	commitTask := s.state.NewTask("commit", "")
	chg.AddTask(commitTask)
	commitTask.Set("registry-transaction", tx)

	refTask := s.state.NewTask("links-to-commit", "")
	chg.AddTask(refTask)
	refTask.Set("commit-task", commitTask.ID())

	for _, t := range []*state.Task{commitTask, refTask} {
		storedTx, carryingTask, err := registrystate.GetStoredTransaction(t)
		c.Assert(err, IsNil)

		c.Assert(storedTx.RegistryAccount, Equals, tx.RegistryAccount)
		c.Assert(storedTx.RegistryName, Equals, tx.RegistryName)
		c.Assert(storedTx.AlteredPaths(), DeepEquals, tx.AlteredPaths())
		c.Assert(carryingTask, Equals, commitTask)
	}
}
