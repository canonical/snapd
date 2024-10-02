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
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
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

	res, err := registrystate.GetViaView(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *registryTestSuite) TestGetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	res, err := registrystate.GetViaView(s.state, s.devAccID, "network", "other-view", []string{"ssid"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" in registry view %s/network/other-view: not found`, s.devAccID))
	c.Check(res, IsNil)

	res, err = registrystate.GetViaView(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" in registry view %s/network/setup-wifi: matching rules don't map to any values`, s.devAccID))
	c.Check(res, IsNil)

	res, err = registrystate.GetViaView(s.state, s.devAccID, "network", "setup-wifi", []string{"other-field"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "other-field" in registry view %s/network/setup-wifi: no matching read rule`, s.devAccID))
	c.Check(res, IsNil)
}

func (s *registryTestSuite) TestSetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := registrystate.SetViaView(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "foo"})
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

	err := registrystate.SetViaView(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"foo": "bar"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" in registry view %s/network/setup-wifi: no matching write rule`, s.devAccID))

	err = registrystate.SetViaView(s.state, s.devAccID, "network", "other-view", map[string]interface{}{"foo": "bar"})
	c.Assert(err, FitsTypeOf, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" in registry view %s/network/other-view: not found`, s.devAccID))
}

func (s *registryTestSuite) TestUnsetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := registry.NewJSONDataBag()
	err := registrystate.SetViaView(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "foo"})
	c.Assert(err, IsNil)

	err = registrystate.SetViaView(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": nil})
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

	results, err := registrystate.GetViaView(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, IsNil)
	resultsMap, ok := results.(map[string]interface{})
	c.Assert(ok, Equals, true)
	c.Assert(resultsMap["ssid"], Equals, "bar")

	err = registrystate.SetViaView(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "baz"})
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

		err := registrystate.SetViaView(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "bar"})
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

	err := registrystate.SetViaView(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{
		"ssids":    []interface{}{"foo", "bar"},
		"password": "pass",
		"private": map[string]interface{}{
			"a": 1,
			"b": 2,
		},
	})
	c.Assert(err, IsNil)

	res, err := registrystate.GetViaView(s.state, s.devAccID, "network", "setup-wifi", nil)
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]interface{}{
		"ssids": []interface{}{"foo", "bar"},
		"private": map[string]interface{}{
			"a": float64(1),
			"b": float64(2),
		},
	})
}

func mockInstalledSnap(c *C, st *state.State, snapYaml string, hooks []string) *snap.Info {
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
	})

	for _, hook := range hooks {
		c.Assert(os.MkdirAll(info.HooksDir(), 0775), IsNil)
		err := os.WriteFile(filepath.Join(info.HooksDir(), hook), nil, 0755)
		c.Assert(err, IsNil)
	}

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
	info := mockInstalledSnap(c, s.state, snapYaml, nil)

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
	info = mockInstalledSnap(c, s.state, coreYaml, nil)

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
	ts, err := registrystate.CreateChangeRegistryTasks(s.state, tx, view, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// there are two edges in the taskset
	commitTask, err := ts.Edge(registrystate.CommitEdge)
	c.Assert(err, IsNil)
	c.Assert(commitTask.Kind(), Equals, "commit-registry-tx")

	cleanupTask, err := ts.Edge(registrystate.LastEdge)
	c.Assert(err, IsNil)
	c.Assert(cleanupTask.Kind(), Equals, "clear-registry-tx")

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
	ts, err := registrystate.CreateChangeRegistryTasks(s.state, tx, view, "custodian-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

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
	ts, err := registrystate.CreateChangeRegistryTasks(s.state, tx, view, "test-snap-1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

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

	// a non-custodian snap modifies a registry
	_, err = registrystate.CreateChangeRegistryTasks(s.state, tx, view, "test-snap-1")
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
	info := mockInstalledSnap(c, s.state, coreYaml, nil)

	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = s.repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	mockSnap := func(snapName string, isCustodian bool, hooks []string) {
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

		info := mockInstalledSnap(c, s.state, snapYaml, hooks)
		for _, hook := range hooks {
			info.Hooks[hook] = &snap.HookInfo{
				Name: hook,
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
	hooks := []string{"change-view-setup", "save-view-setup", "setup-view-changed"}
	for _, snap := range custodians {
		isCustodian := true
		mockSnap(snap, isCustodian, hooks)
	}

	// mock non-custodians
	hooks = []string{"change-view-setup", "save-view-setup", "setup-view-changed", "install"}
	for _, snap := range nonCustodians {
		isCustodian := false
		mockSnap(snap, isCustodian, hooks)
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

func (s *registryTestSuite) checkOngoingRegistryTransaction(c *C, account, registryName string) {
	var commitTasks map[string]string
	err := s.state.Get("registry-commit-tasks", &commitTasks)
	c.Assert(err, IsNil)

	registryRef := account + "/" + registryName
	taskID, ok := commitTasks[registryRef]
	c.Assert(ok, Equals, true)
	commitTask := s.state.Task(taskID)
	c.Assert(commitTask.Kind(), Equals, "commit-registry-tx")
	c.Assert(commitTask.Status(), Equals, state.DoStatus)
}

func (s *registryTestSuite) TestGetTransactionFromUserCreatesNewChange(c *C) {
	hooks, restore := s.mockRegistryHooks(c)
	defer restore()

	restore = registrystate.MockEnsureNow(func(*state.State) {
		s.checkOngoingRegistryTransaction(c, s.devAccID, "network")

		go s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	s.setupRegistryModificationScenario(c, []string{"custodian-snap"}, nil)

	view := s.registry.View("setup-wifi")

	ctx := registrystate.NewContext(nil)
	tx, err := registrystate.GetTransaction(ctx, s.state, view)
	c.Assert(err, IsNil)
	c.Assert(tx, NotNil)

	err = tx.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	// mock the daemon calling Done() in api_registry
	ctx.Done()

	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Assert(chg.Kind(), Equals, "modify-registry")

	s.checkModifyRegistryChange(c, chg, hooks)
}

func (s *registryTestSuite) TestGetTransactionFromSnapCreatesNewChange(c *C) {
	hooks, restore := s.mockRegistryHooks(c)
	defer restore()

	restore = registrystate.MockEnsureNow(func(*state.State) {
		s.checkOngoingRegistryTransaction(c, s.devAccID, "network")

		go s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	s.setupRegistryModificationScenario(c, []string{"custodian-snap"}, []string{"test-snap"})

	ctx, err := hookstate.NewContext(nil, s.state, &hookstate.HookSetup{Snap: "test-snap"}, nil, "")
	c.Assert(err, IsNil)

	s.state.Unlock()
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=foo"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	// the daemon calls Done() in api_snapctl
	ctx.Lock()
	ctx.Done()
	ctx.Unlock()

	s.state.Lock()
	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Assert(chg.Kind(), Equals, "modify-registry")

	s.checkModifyRegistryChange(c, chg, hooks)
}

func (s *registryTestSuite) TestGetTransactionFromNonRegistryHookAddsRegistryTx(c *C) {
	var hooks []string
	restore := hookstate.MockRunHook(func(ctx *hookstate.Context, _ *tomb.Tomb) ([]byte, error) {
		t, _ := ctx.Task()

		ctx.State().Lock()
		var hooksup *hookstate.HookSetup
		err := t.Get("hook-setup", &hooksup)
		ctx.State().Unlock()
		if err != nil {
			return nil, err
		}

		if hooksup.Hook == "install" {
			_, _, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=foo"}, 0)
			c.Assert(err, IsNil)
			return nil, nil
		}

		hooks = append(hooks, hooksup.Hook)
		return nil, nil
	})
	defer restore()

	restore = registrystate.MockEnsureNow(func(st *state.State) {
		// we actually want to call ensure here (since we use Loop) but check the
		// transaction was added to the state as usual
		s.checkOngoingRegistryTransaction(c, s.devAccID, "network")
		st.EnsureBefore(0)
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()
	// only one custodian snap is installed
	s.setupRegistryModificationScenario(c, []string{"custodian-snap"}, []string{"test-snap"})

	hookTask := s.state.NewTask("run-hook", "")
	chg := s.state.NewChange("install", "")
	chg.AddTask(hookTask)

	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "install",
	}
	hookTask.Set("hook-setup", hooksup)
	s.state.Unlock()

	c.Assert(s.o.StartUp(), IsNil)
	s.state.EnsureBefore(0)
	s.o.Loop()
	defer s.o.Stop()

	select {
	case <-chg.Ready():
	case <-time.After(5 * time.Second):
		c.Fatalf("test timed out")
	}

	s.state.Lock()
	s.checkModifyRegistryChange(c, chg, &hooks)
}

func (s *registryTestSuite) mockRegistryHooks(c *C) (*[]string, func()) {
	var hooks []string
	restore := hookstate.MockRunHook(func(ctx *hookstate.Context, _ *tomb.Tomb) ([]byte, error) {
		t, _ := ctx.Task()
		ctx.State().Lock()
		defer ctx.State().Unlock()

		var hooksup *hookstate.HookSetup
		err := t.Get("hook-setup", &hooksup)
		if err != nil {
			return nil, err
		}

		hooks = append(hooks, hooksup.Hook)
		return nil, nil
	})

	return &hooks, restore
}

func (s *registryTestSuite) checkModifyRegistryChange(c *C, chg *state.Change, hooks *[]string) {
	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(*hooks, DeepEquals, []string{"change-view-setup", "save-view-setup", "setup-view-changed"})

	commitTask := findTask(chg, "commit-registry-tx")
	tx, _, err := registrystate.GetStoredTransaction(commitTask)
	c.Assert(err, IsNil)

	// the state was cleared
	var txCommits map[string]string
	err = s.state.Get("registry-tx-commits", &txCommits)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})

	err = tx.Clear(s.state)
	c.Assert(err, IsNil)

	// was committed (otherwise would've been removed by Clear)
	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")
}

func (s *registryTestSuite) TestGetTransactionDifferentFromOngoingOnlyForRead(c *C) {
}

func (s *registryTestSuite) TestGetTransactionFromChangeViewHook(c *C) {
	ctx := s.testGetReadableOngoingTransaction(c, "change-view-setup")

	// change-view hooks can also write to the transaction
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=bar"}, 0)
	c.Assert(err, IsNil)
	// accessed an ongoing transaction
	c.Assert(stdout, IsNil)
	c.Assert(stderr, IsNil)

	// this save the changes that the hook performs
	ctx.Lock()
	ctx.Done()
	ctx.Unlock()

	s.state.Lock()
	defer s.state.Unlock()
	t, _ := ctx.Task()
	tx, _, err := registrystate.GetStoredTransaction(t)
	c.Assert(err, IsNil)

	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")
}

func (s *registryTestSuite) TestGetTransactionFromSaveViewHook(c *C) {
	ctx := s.testGetReadableOngoingTransaction(c, "save-view-setup")

	// non change-view hooks cannot modify the transaction
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=bar"}, 0)
	c.Assert(err, ErrorMatches, `cannot modify registry in "save-view-setup" hook`)
	c.Assert(stdout, IsNil)
	c.Assert(stderr, IsNil)
}

func (s *registryTestSuite) TestGetTransactionFromViewChangedHook(c *C) {
	ctx := s.testGetReadableOngoingTransaction(c, "setup-view-changed")

	// non change-view hooks cannot modify the transaction
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=bar"}, 0)
	c.Assert(err, ErrorMatches, `cannot modify registry in "setup-view-changed" hook`)
	c.Assert(stdout, IsNil)
	c.Assert(stderr, IsNil)
}

func (s *registryTestSuite) testGetReadableOngoingTransaction(c *C, hook string) *hookstate.Context {
	s.state.Lock()
	defer s.state.Unlock()

	s.setupRegistryModificationScenario(c, []string{"custodian-snap"}, []string{"test-snap"})

	originalTx, err := registrystate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = originalTx.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("test", "")
	commitTask := s.state.NewTask("commit-registry-tx", "")
	commitTask.Set("registry-transaction", originalTx)
	chg.AddTask(commitTask)

	hookTask := s.state.NewTask("run-hook", "")
	chg.AddTask(hookTask)
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: hook}
	mockHandler := hooktest.NewMockHandler()
	hookTask.Set("commit-task", commitTask.ID())

	ctx, err := hookstate.NewContext(hookTask, s.state, setup, mockHandler, "")
	c.Assert(err, IsNil)

	s.state.Unlock()
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"get", "--view", ":setup", "ssid"}, 0)
	s.state.Lock()
	c.Assert(err, IsNil)
	// accessed an ongoing transaction
	c.Assert(string(stdout), Equals, "foo\n")
	c.Assert(stderr, IsNil)

	return ctx
}

func (s *registryTestSuite) TestGetDifferentTransactionThanOngoing(c *C) {
	s.state.Lock()

	tx, err := registrystate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("some-change", "")
	commitTask := s.state.NewTask("commit", "")
	chg.AddTask(commitTask)
	commitTask.Set("registry-transaction", tx)

	refTask := s.state.NewTask("change-view-setup", "")
	chg.AddTask(refTask)
	refTask.Set("commit-task", commitTask.ID())

	// make some other registry to access concurrently
	reg, err := registry.New("foo", "bar", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			}}}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	s.state.Unlock()

	mockHandler := hooktest.NewMockHandler()
	setup := &hookstate.HookSetup{Snap: "test-snap", Hook: "change-view-setup"}
	hookCtx, err := hookstate.NewContext(refTask, s.state, setup, mockHandler, "")
	c.Assert(err, IsNil)

	hookCtx.Lock()
	ctx := registrystate.NewContext(hookCtx)
	tx, err = registrystate.GetTransaction(ctx, s.state, reg.View("foo"))
	hookCtx.Unlock()
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot access registry foo/bar: ongoing transaction for %s/network`, s.devAccID))
	c.Assert(tx, IsNil)
}
