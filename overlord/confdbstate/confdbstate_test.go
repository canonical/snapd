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

package confdbstate_test

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
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type confdbTestSuite struct {
	state *state.State
	o     *overlord.Overlord

	dbSchema *confdb.Schema
	devAccID string

	repo *interfaces.Repository
}

var _ = Suite(&confdbTestSuite{})

func Test(t *testing.T) { TestingT(t) }

func (s *confdbTestSuite) SetUpTest(c *C) {
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

	// to test the confdbManager
	mgr := confdbstate.Manager(s.state, hookMgr, runner)
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

	as, err := signingDB.Sign(asserts.ConfdbSchemaType, headers, body, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(s.state, as), IsNil)

	s.devAccID = devAccKey.AccountID()
	s.dbSchema = as.(*asserts.ConfdbSchema).Schema()

	tr := config.NewTransaction(s.state)
	_, confOption := features.Confdb.ConfigOption()
	err = tr.Set("core", confOption, true)
	c.Assert(err, IsNil)
	tr.Commit()
}

func (s *confdbTestSuite) TestGetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := confdb.NewJSONDatabag()
	err := databag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)
	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": databag}})

	res, err := confdbstate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *confdbTestSuite) TestGetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	res, err := confdbstate.Get(s.state, s.devAccID, "network", "other-view", []string{"ssid"})
	c.Assert(err, FitsTypeOf, &confdb.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot find view "other-view" in confdb schema %s/network`, s.devAccID))
	c.Check(res, IsNil)

	res, err = confdbstate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, FitsTypeOf, &confdb.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" through %s/network/setup-wifi: no view data`, s.devAccID))
	c.Check(res, IsNil)

	res, err = confdbstate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid", "ssids"})
	c.Assert(err, FitsTypeOf, &confdb.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid", "ssids" through %s/network/setup-wifi: no view data`, s.devAccID))
	c.Check(res, IsNil)

	res, err = confdbstate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"other-field"})
	c.Assert(err, FitsTypeOf, &confdb.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "other-field" through %s/network/setup-wifi: no matching rule`, s.devAccID))
	c.Check(res, IsNil)
}

func (s *confdbTestSuite) TestSetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := confdbstate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "foo"})
	c.Assert(err, IsNil)

	var databags map[string]map[string]confdb.JSONDatabag
	err = s.state.Get("confdb-databags", &databags)
	c.Assert(err, IsNil)

	val, err := databags[s.devAccID]["network"].Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "foo")
}

func (s *confdbTestSuite) TestSetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := confdbstate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"foo": "bar"})
	c.Assert(err, FitsTypeOf, &confdb.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" through %s/network/setup-wifi: no matching rule`, s.devAccID))

	err = confdbstate.Set(s.state, s.devAccID, "network", "other-view", map[string]interface{}{"foo": "bar"})
	c.Assert(err, FitsTypeOf, &confdb.NotFoundError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot find view "other-view" in confdb schema %s/network`, s.devAccID))
}

func (s *confdbTestSuite) TestUnsetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	databag := confdb.NewJSONDatabag()
	err := confdbstate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "foo"})
	c.Assert(err, IsNil)

	err = confdbstate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": nil})
	c.Assert(err, IsNil)

	val, err := databag.Get("wifi.ssid")
	c.Assert(err, FitsTypeOf, confdb.PathError(""))
	c.Assert(val, Equals, nil)
}

func (s *confdbTestSuite) TestConfdbstateSetWithExistingState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()
	err := bag.Set("wifi.ssid", "bar")
	c.Assert(err, IsNil)
	databags := map[string]map[string]confdb.JSONDatabag{
		s.devAccID: {"network": bag},
	}

	s.state.Set("confdb-databags", databags)

	results, err := confdbstate.Get(s.state, s.devAccID, "network", "setup-wifi", []string{"ssid"})
	c.Assert(err, IsNil)
	resultsMap, ok := results.(map[string]interface{})
	c.Assert(ok, Equals, true)
	c.Assert(resultsMap["ssid"], Equals, "bar")

	err = confdbstate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "baz"})
	c.Assert(err, IsNil)

	err = s.state.Get("confdb-databags", &databags)
	c.Assert(err, IsNil)
	value, err := databags[s.devAccID]["network"].Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "baz")
}

func (s *confdbTestSuite) TestConfdbstateSetWithNoState(c *C) {
	type testcase struct {
		state map[string]map[string]confdb.JSONDatabag
	}

	testcases := []testcase{
		{
			state: map[string]map[string]confdb.JSONDatabag{
				s.devAccID: {"network": nil},
			},
		},
		{
			state: map[string]map[string]confdb.JSONDatabag{
				s.devAccID: nil,
			},
		},
		{
			state: map[string]map[string]confdb.JSONDatabag{},
		},
		{
			state: nil,
		},
	}

	s.state.Lock()
	defer s.state.Unlock()
	for _, tc := range testcases {
		s.state.Set("confdb-databags", tc.state)

		err := confdbstate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{"ssid": "bar"})
		c.Assert(err, IsNil)

		var databags map[string]map[string]confdb.JSONDatabag
		err = s.state.Get("confdb-databags", &databags)
		c.Assert(err, IsNil)

		value, err := databags[s.devAccID]["network"].Get("wifi.ssid")
		c.Assert(err, IsNil)
		c.Assert(value, Equals, "bar")
	}
}

func (s *confdbTestSuite) TestConfdbstateGetEntireView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := confdbstate.Set(s.state, s.devAccID, "network", "setup-wifi", map[string]interface{}{
		"ssids":    []interface{}{"foo", "bar"},
		"password": "pass",
		"private": map[string]interface{}{
			"a": 1,
			"b": 2,
		},
	})
	c.Assert(err, IsNil)

	res, err := confdbstate.Get(s.state, s.devAccID, "network", "setup-wifi", nil)
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

func (s *confdbTestSuite) TestPlugsAffectedByPaths(c *C) {
	confdb, err := confdb.NewSchema(s.devAccID, "confdb", map[string]interface{}{
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
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	repo := interfaces.NewRepository()
	s.state.Lock()
	defer s.state.Unlock()
	ifacerepo.Replace(s.state, repo)

	confdbIface := &ifacetest.TestInterface{InterfaceName: "confdb"}
	err = repo.AddInterface(confdbIface)
	c.Assert(err, IsNil)

	snapYaml := fmt.Sprintf(`name: test-snap
version: 1
type: app
plugs:
  view-1:
    interface: confdb
    account: %[1]s
    view: confdb/view-1
  view-2:
    interface: confdb
    account: %[1]s
    view: confdb/view-2
  view-3:
    interface: confdb
    account: %[1]s
    view: confdb/view-3
  view-4:
    interface: confdb
    account: %[1]s
    view: confdb/view-4
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
 confdb-slot:
  interface: confdb
`
	info = mockInstalledSnap(c, s.state, coreYaml, nil)

	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	for _, n := range []string{"view-1", "view-2", "view-3"} {
		ref := &interfaces.ConnRef{
			PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: n},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "confdb-slot"},
		}
		_, err = repo.Connect(ref, nil, nil, nil, nil, nil)
		c.Assert(err, IsNil)
	}

	snapPlugs, err := confdbstate.GetPlugsAffectedByPaths(s.state, confdb, []string{"foo"})
	c.Assert(err, IsNil)
	c.Assert(snapPlugs, HasLen, 1)

	plugNames := make([]string, 0, len(snapPlugs["test-snap"]))
	for _, plug := range snapPlugs["test-snap"] {
		plugNames = append(plugNames, plug.Name)
	}
	c.Assert(plugNames, testutil.DeepUnsortedMatches, []string{"view-1", "view-3"})
}

func (s *confdbTestSuite) TestConfdbTasksUserSetWithCustodianInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	const noHooks = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	chg := s.state.NewChange("modify-confdb", "")

	// a user (not a snap) changes a confdb
	ts, err := confdbstate.CreateChangeConfdbTasks(s.state, tx, view, "")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// there are two edges in the taskset
	commitTask, err := ts.Edge(confdbstate.CommitEdge)
	c.Assert(err, IsNil)
	c.Assert(commitTask.Kind(), Equals, "commit-confdb-tx")

	cleanupTask, err := ts.Edge(confdbstate.ClearTxEdge)
	c.Assert(err, IsNil)
	c.Assert(cleanupTask.Kind(), Equals, "clear-confdb-tx")

	// the custodian snap's hooks are run
	tasks := []string{"clear-confdb-tx-on-error", "run-hook", "run-hook", "run-hook", "commit-confdb-tx", "clear-confdb-tx"}
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
			Hook:        "observe-view-setup",
			Optional:    true,
			IgnoreError: true,
		},
	}

	checkSetConfdbTasks(c, chg, tasks, hooks)
}

func (s *confdbTestSuite) TestConfdbTasksCustodianSnapSet(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	const noHooks = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	chg := s.state.NewChange("set-confdb", "")

	// a user (not a snap) changes a confdb
	ts, err := confdbstate.CreateChangeConfdbTasks(s.state, tx, view, "custodian-snap")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// the custodian snap's hooks are run
	tasks := []string{"clear-confdb-tx-on-error", "run-hook", "run-hook", "commit-confdb-tx", "clear-confdb-tx"}
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

	checkSetConfdbTasks(c, chg, tasks, hooks)
}

func (s *confdbTestSuite) TestConfdbTasksObserverSnapSetWithCustodianInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// one custodian and several non-custodians are installed
	const noHooks = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, []string{"test-snap-1", "test-snap-2"})

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	chg := s.state.NewChange("modify-confdb", "")

	// a non-custodian snap modifies a confdb
	ts, err := confdbstate.CreateChangeConfdbTasks(s.state, tx, view, "test-snap-1")
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	// we trigger hooks for the custodian snap and for the observe-view- for the
	// observer snap that didn't trigger the change
	tasks := []string{"clear-confdb-tx-on-error", "run-hook", "run-hook", "run-hook", "run-hook", "commit-confdb-tx", "clear-confdb-tx"}
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
			Hook:        "observe-view-setup",
			Optional:    true,
			IgnoreError: true,
		},
		{
			Snap:        "test-snap-2",
			Hook:        "observe-view-setup",
			Optional:    true,
			IgnoreError: true,
		},
	}

	checkSetConfdbTasks(c, chg, tasks, hooks)
}

func (s *confdbTestSuite) TestConfdbTasksDisconnectedCustodianSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// mock and installed custodian-snap but disconnect it
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"test-custodian-snap"}, []string{"test-snap"})
	s.repo.Disconnect("test-custodian-snap", "setup", "core", "confdb-slot")
	s.testConfdbTasksNoCustodian(c)
}

func (s *confdbTestSuite) TestConfdbTasksNoCustodianSnapInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no custodian snap is installed
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, nil, []string{"test-snap"})
	s.testConfdbTasksNoCustodian(c)
}

func (s *confdbTestSuite) testConfdbTasksNoCustodian(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")

	// a non-custodian snap modifies a confdb
	_, err = confdbstate.CreateChangeConfdbTasks(s.state, tx, view, "test-snap-1")
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot commit changes to confdb made through view %s/network/%s: no custodian snap installed", s.devAccID, view.Name))
}

func (s *confdbTestSuite) setupConfdbScenario(c *C, noHooks bool, custodians, nonCustodians []string) {
	s.repo = interfaces.NewRepository()
	ifacerepo.Replace(s.state, s.repo)

	confdbIface := &ifacetest.TestInterface{InterfaceName: "confdb"}
	err := s.repo.AddInterface(confdbIface)
	c.Assert(err, IsNil)

	// mock the confdb slot
	const coreYaml = `name: core
version: 1
type: os
slots:
  confdb-slot:
    interface: confdb
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
    interface: confdb
    account: %s
    view: network/setup-wifi
`, snapName, s.devAccID)

		if isCustodian {
			snapYaml +=
				`    role: custodian`
		}

		info := mockInstalledSnap(c, s.state, snapYaml, hooks)
		if !noHooks {
			for _, hook := range hooks {
				info.Hooks[hook] = &snap.HookInfo{
					Name: hook,
					Snap: info,
				}
			}
		}

		appSet, err := interfaces.NewSnapAppSet(info, nil)
		c.Assert(err, IsNil)
		err = s.repo.AddAppSet(appSet)
		c.Assert(err, IsNil)

		ref := &interfaces.ConnRef{
			PlugRef: interfaces.PlugRef{Snap: snapName, Name: "setup"},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "confdb-slot"},
		}
		_, err = s.repo.Connect(ref, nil, nil, nil, nil, nil)
		c.Assert(err, IsNil)
	}

	// mock custodians
	hooks := []string{"change-view-setup", "save-view-setup", "query-view-setup", "load-view-setup", "observe-view-setup"}
	for _, snap := range custodians {
		isCustodian := true
		mockSnap(snap, isCustodian, hooks)
	}

	// mock non-custodians
	hooks = []string{"observe-view-setup", "install"}
	for _, snap := range nonCustodians {
		isCustodian := false
		mockSnap(snap, isCustodian, hooks)
	}
}

func checkSetConfdbTasks(c *C, chg *state.Change, taskKinds []string, hooksups []*hookstate.HookSetup) {
	c.Assert(chg.Tasks(), HasLen, len(taskKinds))
	commitTask := findTask(chg, "commit-confdb-tx")

	// commit task carries the transaction
	var tx *confdbstate.Transaction
	err := commitTask.Get("confdb-transaction", &tx)
	c.Assert(err, IsNil)
	c.Assert(tx, NotNil)

	t := findTask(chg, "clear-confdb-tx-on-error")
	var hookIndex int
	var i int
loop:
	for ; t != nil; i++ {
		c.Assert(t.Kind(), Equals, taskKinds[i])
		if t.Kind() == "run-hook" {
			c.Assert(getHookSetup(c, t), DeepEquals, hooksups[hookIndex])
			hookIndex++
		}

		if t.Kind() != "commit-confdb-tx" {
			// all tasks (other than the commit) are linked to the commit task
			var id string
			err := t.Get("tx-task", &id)
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

func (s *confdbTestSuite) TestGetStoredTransaction(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("some-change", "")
	commitTask := s.state.NewTask("commit", "")
	chg.AddTask(commitTask)
	commitTask.Set("confdb-transaction", tx)

	refTask := s.state.NewTask("links-to-commit", "")
	chg.AddTask(refTask)
	refTask.Set("tx-task", commitTask.ID())

	for _, t := range []*state.Task{commitTask, refTask} {
		storedTx, txTask, saveChanges, err := confdbstate.GetStoredTransaction(t)
		c.Assert(err, IsNil)
		c.Assert(storedTx.ConfdbAccount, Equals, tx.ConfdbAccount)
		c.Assert(storedTx.ConfdbName, Equals, tx.ConfdbName)
		c.Assert(saveChanges, NotNil)
		c.Assert(txTask.ID(), Equals, commitTask.ID())

		// check that making and saving changes works
		c.Assert(storedTx.Set("foo", "bar"), IsNil)
		saveChanges()

		tx = nil
		tx, _, _, err = confdbstate.GetStoredTransaction(t)
		c.Assert(err, IsNil)

		val, err := tx.Get("foo")
		c.Assert(err, IsNil)
		c.Assert(val, Equals, "bar")

		c.Assert(tx.Clear(s.state), IsNil)
		commitTask.Set("confdb-transaction", tx)
	}
}

func (s *confdbTestSuite) checkOngoingWriteConfdbTx(c *C, account, confdbName string) {
	var ongoingConfdbTxs map[string]*confdbstate.ConfdbTransactions
	err := s.state.Get("confdb-ongoing-txs", &ongoingConfdbTxs)
	c.Assert(err, IsNil)

	confdbRef := account + "/" + confdbName
	ongoingTxs, ok := ongoingConfdbTxs[confdbRef]
	c.Assert(ok, Equals, true)
	commitTask := s.state.Task(ongoingTxs.WriteTxID)
	c.Assert(commitTask.Kind(), Equals, "commit-confdb-tx")
	c.Assert(commitTask.Status(), Equals, state.DoStatus)
}

func (s *confdbTestSuite) TestGetTransactionFromUserCreatesNewChange(c *C) {
	hooks, restore := s.mockConfdbHooks(c)
	defer restore()

	restore = confdbstate.MockEnsureNow(func(*state.State) {
		s.checkOngoingWriteConfdbTx(c, s.devAccID, "network")

		go s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	view := s.dbSchema.View("setup-wifi")

	tx, commitTxFunc, err := confdbstate.GetTransactionToSet(nil, s.state, view)
	c.Assert(err, IsNil)
	c.Assert(tx, NotNil)
	c.Assert(commitTxFunc, NotNil)

	err = tx.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	// mock the daemon triggering the commit
	changeID, waitChan, err := commitTxFunc()
	c.Assert(err, IsNil)

	s.state.Unlock()
	select {
	case <-waitChan:
	case <-time.After(testutil.HostScaledTimeout(5 * time.Second)):
		s.state.Lock()
		c.Fatal("test timed out after 5s")
	}
	s.state.Lock()

	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Assert(chg.Kind(), Equals, "set-confdb")
	c.Assert(changeID, Equals, chg.ID())

	s.checkSetConfdbChange(c, chg, hooks)
}

func (s *confdbTestSuite) TestGetTransactionFromSnapCreatesNewChange(c *C) {
	hooks, restore := s.mockConfdbHooks(c)
	defer restore()

	restore = confdbstate.MockEnsureNow(func(*state.State) {
		s.checkOngoingWriteConfdbTx(c, s.devAccID, "network")

		go s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, []string{"test-snap"})

	ctx, err := hookstate.NewContext(nil, s.state, &hookstate.HookSetup{Snap: "test-snap"}, nil, "")
	c.Assert(err, IsNil)

	s.state.Unlock()
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=foo"}, 0)
	c.Assert(err, IsNil)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	// this is called automatically by hooks or manually for daemon/
	ctx.Lock()
	ctx.Done()
	ctx.Unlock()

	s.state.Lock()
	c.Assert(s.state.Changes(), HasLen, 1)
	chg := s.state.Changes()[0]
	c.Assert(chg.Kind(), Equals, "set-confdb")

	s.checkSetConfdbChange(c, chg, hooks)
}

func (s *confdbTestSuite) TestGetTransactionFromNonConfdbHookAddsConfdbTx(c *C) {
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

	restore = confdbstate.MockEnsureNow(func(st *state.State) {
		// we actually want to call ensure here (since we use Loop) but check the
		// transaction was added to the state as usual
		s.checkOngoingWriteConfdbTx(c, s.devAccID, "network")
		st.EnsureBefore(0)
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()
	// only one custodian snap is installed
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, []string{"test-snap"})

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
		c.Fatal("test timed out")
	}

	s.state.Lock()
	s.checkSetConfdbChange(c, chg, &hooks)
}

func (s *confdbTestSuite) mockConfdbHooks(c *C) (*[]string, func()) {
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

func (s *confdbTestSuite) checkSetConfdbChange(c *C, chg *state.Change, hooks *[]string) {
	c.Assert(chg.Status(), Equals, state.DoneStatus)
	c.Assert(*hooks, DeepEquals, []string{"change-view-setup", "save-view-setup", "observe-view-setup"})

	commitTask := findTask(chg, "commit-confdb-tx")
	tx, _, _, err := confdbstate.GetStoredTransaction(commitTask)
	c.Assert(err, IsNil)

	// the state was cleared
	var txCommits map[string]string
	err = s.state.Get("confdb-tx-commits", &txCommits)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})

	err = tx.Clear(s.state)
	c.Assert(err, IsNil)

	// was committed (otherwise would've been removed by Clear)
	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")
}

func (s *confdbTestSuite) TestGetTransactionFromChangeViewHook(c *C) {
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
	tx, _, _, err := confdbstate.GetStoredTransaction(t)
	c.Assert(err, IsNil)

	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")
}

func (s *confdbTestSuite) TestGetTransactionFromSaveViewHook(c *C) {
	ctx := s.testGetReadableOngoingTransaction(c, "save-view-setup")

	// non change-view hooks cannot modify the transaction
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=bar"}, 0)
	c.Assert(err, ErrorMatches, `cannot modify confdb in "save-view-setup" hook`)
	c.Assert(stdout, IsNil)
	c.Assert(stderr, IsNil)
}

func (s *confdbTestSuite) TestGetTransactionFromViewChangedHook(c *C) {
	ctx := s.testGetReadableOngoingTransaction(c, "observe-view-setup")

	// non change-view hooks cannot modify the transaction
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=bar"}, 0)
	c.Assert(err, ErrorMatches, `cannot modify confdb in "observe-view-setup" hook`)
	c.Assert(stdout, IsNil)
	c.Assert(stderr, IsNil)
}

func (s *confdbTestSuite) testGetReadableOngoingTransaction(c *C, hook string) *hookstate.Context {
	s.state.Lock()
	defer s.state.Unlock()

	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, []string{"test-snap"})

	originalTx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = originalTx.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("test", "")
	commitTask := s.state.NewTask("commit-confdb-tx", "")
	commitTask.Set("confdb-transaction", originalTx)
	chg.AddTask(commitTask)

	hookTask := s.state.NewTask("run-hook", "")
	chg.AddTask(hookTask)
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: hook}
	mockHandler := hooktest.NewMockHandler()
	hookTask.Set("tx-task", commitTask.ID())

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

func (s *confdbTestSuite) TestGetDifferentTransactionThanOngoing(c *C) {
	s.state.Lock()

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("some-change", "")
	commitTask := s.state.NewTask("commit", "")
	chg.AddTask(commitTask)
	commitTask.Set("confdb-transaction", tx)

	refTask := s.state.NewTask("change-view-setup", "")
	chg.AddTask(refTask)
	refTask.Set("tx-task", commitTask.ID())

	// make some other confdb to access concurrently
	confdb, err := confdb.NewSchema("foo", "bar", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			}}}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	s.state.Unlock()

	mockHandler := hooktest.NewMockHandler()
	setup := &hookstate.HookSetup{Snap: "test-snap", Hook: "change-view-setup"}
	ctx, err := hookstate.NewContext(refTask, s.state, setup, mockHandler, "")
	c.Assert(err, IsNil)

	ctx.Lock()
	tx, commitTxFunc, err := confdbstate.GetTransactionToSet(ctx, s.state, confdb.View("foo"))
	ctx.Unlock()
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot access confdb through view foo/bar/foo: ongoing transaction for %s/network`, s.devAccID))
	c.Assert(tx, IsNil)
	c.Assert(commitTxFunc, IsNil)
}

func (s *confdbTestSuite) TestConfdbLoadDisconnectedCustodianSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no connected custodian
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"test-custodian-snap"}, []string{"test-snap"})
	s.repo.Disconnect("test-custodian-snap", "setup", "core", "confdb-slot")
	s.testConfdbLoadNoCustodian(c)
}

func (s *confdbTestSuite) TestConfdbLoadNoCustodianInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no custodian snap is installed
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, nil, []string{"test-snap"})
	s.testConfdbLoadNoCustodian(c)
}

func (s *confdbTestSuite) testConfdbLoadNoCustodian(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")

	// a non-custodian snap modifies a confdb
	_, err = confdbstate.CreateLoadConfdbTasks(s.state, tx, view)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot load confdb through view %s/network/setup-wifi: no custodian snap connected", s.devAccID))
}

func checkLoadConfdbTasks(c *C, chg *state.Change, taskKinds []string, hooksups []*hookstate.HookSetup) {
	// check clear-confdb-tx carries the transaction
	c.Assert(chg.Tasks(), HasLen, len(taskKinds))
	clearTxTask := findTask(chg, "clear-confdb-tx")

	var tx *confdbstate.Transaction
	err := clearTxTask.Get("confdb-transaction", &tx)
	c.Assert(err, IsNil)
	c.Assert(tx, NotNil)

	t := findTask(chg, "run-hook")
	var hookIndex int
	var i int
loop:
	for ; t != nil; i++ {
		c.Assert(t.Kind(), Equals, taskKinds[i])
		if t.Kind() == "run-hook" {
			c.Assert(getHookSetup(c, t), DeepEquals, hooksups[hookIndex])
			hookIndex++
		}

		// check all other tasks link to it
		if t.Kind() != "clear-confdb-tx" {
			var id string
			err := t.Get("tx-task", &id)
			c.Assert(err, IsNil)
			c.Assert(id, Equals, clearTxTask.ID())
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

func (s *confdbTestSuite) TestConfdbLoadCustodianInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	chg := s.state.NewChange("load-confdb", "")

	ts, err := confdbstate.CreateLoadConfdbTasks(s.state, tx, view)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	cleanupTask, err := ts.Edge(confdbstate.ClearTxEdge)
	c.Assert(err, IsNil)
	c.Assert(cleanupTask.Kind(), Equals, "clear-confdb-tx")

	// the custodian snap's hooks are run
	tasks := []string{"run-hook", "run-hook", "clear-confdb-tx"}
	hooks := []*hookstate.HookSetup{
		{
			Snap:        "custodian-snap",
			Hook:        "load-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
		{
			Snap:        "custodian-snap",
			Hook:        "query-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
	}

	checkLoadConfdbTasks(c, chg, tasks, hooks)
}

func (s *confdbTestSuite) TestConfdbLoadCustodianWithNoHooks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	const noHooks bool = true
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	ts, err := confdbstate.CreateLoadConfdbTasks(s.state, tx, view)
	c.Assert(err, IsNil)
	// no hooks, nothing to run
	c.Assert(ts, IsNil)
}

func (s *confdbTestSuite) TestConfdbLoadTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	ts, err := confdbstate.CreateLoadConfdbTasks(s.state, tx, view)
	c.Assert(err, IsNil)
	chg := s.state.NewChange("get-confdb", "")
	chg.AddAll(ts)

	tasks := []string{"run-hook", "run-hook", "clear-confdb-tx"}
	hooks := []*hookstate.HookSetup{
		{
			Snap:        "custodian-snap",
			Hook:        "load-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
		{
			Snap:        "custodian-snap",
			Hook:        "query-view-setup",
			Optional:    true,
			IgnoreError: false,
		},
	}
	checkLoadConfdbTasks(c, chg, tasks, hooks)
}

func (s *confdbTestSuite) TestGetTransactionForSnapctlNoHook(c *C) {
	s.state.Lock()
	// only one custodian snap is installed
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(nil, s.state, nil, mockHandler, "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	chg := s.testGetTransactionForSnapctl(c, ctx)

	s.state.Lock()
	defer s.state.Unlock()
	// read outside of a task so creates a new change
	c.Assert(chg.Kind(), Equals, "get-confdb")
	c.Assert(chg.Summary(), Equals, fmt.Sprintf("Get confdb through \"%s/network/setup-wifi\"", s.devAccID))
}

func (s *confdbTestSuite) TestGetTransactionForSnapctlNonConfdbHook(c *C) {
	s.state.Lock()
	// only one custodian snap is installed
	const noHooks bool = false
	// the non-custodian snap doesn't matter in the loading case but we can reuse
	// the helper to set it up with an install hook
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, []string{"test-snap"})

	hookTask := s.state.NewTask("run-hook", "")
	chg := s.state.NewChange("install", "")
	chg.AddTask(hookTask)

	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "install",
	}
	hookTask.Set("hook-setup", hooksup)
	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(hookTask, s.state, hooksup, mockHandler, "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	s.testGetTransactionForSnapctl(c, ctx)
}

func (s *confdbTestSuite) testGetTransactionForSnapctl(c *C, ctx *hookstate.Context) *state.Change {
	hooks, restore := s.mockConfdbHooks(c)
	defer restore()

	restore = confdbstate.MockEnsureNow(func(*state.State) {
		s.checkOngoingReadConfdbTx(c, s.devAccID, "network")
		go func() {
			s.o.Settle(5 * time.Second)
		}()
	})
	defer restore()

	ctx.Lock()
	defer ctx.Unlock()

	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err := bag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view := s.dbSchema.View("setup-wifi")
	tx, err := confdbstate.GetTransactionForSnapctlGet(ctx, view)
	c.Assert(err, IsNil)
	c.Assert(s.state.Changes(), HasLen, 1)

	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")

	chg := s.state.Changes()[0]
	s.checkGetConfdbTasks(c, chg, hooks)
	return chg
}

func (s *confdbTestSuite) TestGetTransactionInConfdbHook(c *C) {
	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err := bag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	originalTx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	chg := s.state.NewChange("test", "")
	clearTask := s.state.NewTask("clear-confdb-tx", "")
	clearTask.Set("confdb-transaction", originalTx)
	chg.AddTask(clearTask)

	hookTask := s.state.NewTask("run-hook", "")
	chg.AddTask(hookTask)
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "load-view-setup"}
	mockHandler := hooktest.NewMockHandler()
	hookTask.Set("tx-task", clearTask.ID())

	ctx, err := hookstate.NewContext(hookTask, s.state, setup, mockHandler, "")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	tx, err := confdbstate.GetTransactionForSnapctlGet(ctx, view)
	c.Assert(err, IsNil)
	// reads synchronously without creating new change or tasks
	c.Assert(s.state.Changes(), HasLen, 1)
	c.Assert(chg.Tasks(), HasLen, 2)

	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")
}

func (s *confdbTestSuite) TestGetTransactionNoConfdbHooks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// the custodian snap has no snaps, no tasks should be scheduled
	const noHooks bool = true
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	hookTask := s.state.NewTask("run-hook", "")

	hooksup := &hookstate.HookSetup{
		Snap: "test-snap",
		Hook: "install",
	}
	hookTask.Set("hook-setup", hooksup)
	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(hookTask, s.state, hooksup, mockHandler, "")
	c.Assert(err, IsNil)

	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err = bag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view := s.dbSchema.View("setup-wifi")
	tx, err := confdbstate.GetTransactionForSnapctlGet(ctx, view)
	c.Assert(err, IsNil)
	c.Assert(tx, NotNil)

	// no tasks were scheduled
	c.Assert(s.state.Changes(), HasLen, 0)

	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")

	// we're not tracking the ongoing tx read because it's all synchronous (no possible conflicts)
	var confdbTxs map[string]*confdbstate.ConfdbTransactions
	err = s.state.Get("confdb-ongoing-txs", &confdbTxs)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *confdbTestSuite) TestGetTransactionTimesOut(c *C) {
	restore := confdbstate.MockTransactionTimeout(0)
	defer restore()

	s.state.Lock()
	const noHooks bool = false
	s.setupConfdbScenario(c, noHooks, []string{"custodian-snap"}, nil)

	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(nil, s.state, nil, mockHandler, "")
	c.Assert(err, IsNil)

	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err = bag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})
	s.state.Unlock()

	view := s.dbSchema.View("setup-wifi")
	ctx.Lock()
	defer ctx.Unlock()

	tx, err := confdbstate.GetTransactionForSnapctlGet(ctx, view)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot load confdb %s/network in change 1: timed out after 0s", s.devAccID))
	c.Assert(tx, IsNil)
}

func (s *confdbTestSuite) checkGetConfdbTasks(c *C, chg *state.Change, hooks *[]string) {
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// using DeepContains because there may be an install hook as well
	for _, hookName := range []string{"load-view-setup", "query-view-setup"} {
		c.Assert(*hooks, testutil.DeepContains, hookName)
	}

	clearTask := findTask(chg, "clear-confdb-tx")
	_, _, _, err := confdbstate.GetStoredTransaction(clearTask)
	c.Assert(err, IsNil)
	// TODO?

	// the state was cleared
	var ongoingTxs map[string]string
	err = s.state.Get("confdb-tx-commits", &ongoingTxs)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})
}

func (s *confdbTestSuite) checkOngoingReadConfdbTx(c *C, account, confdbName string) {
	var ongoingTxs map[string]*confdbstate.ConfdbTransactions
	err := s.state.Get("confdb-ongoing-txs", &ongoingTxs)
	c.Assert(err, IsNil)

	confdbRef := account + "/" + confdbName
	txTasks, ok := ongoingTxs[confdbRef]
	c.Assert(ok, Equals, true)
	c.Assert(txTasks.WriteTxID, Equals, "")
	c.Assert(txTasks.ReadTxIDs, HasLen, 1)

	clearTask := s.state.Task(txTasks.ReadTxIDs[0])
	c.Assert(clearTask.Kind(), Equals, "clear-confdb-tx")
	c.Assert(clearTask.Status(), Equals, state.DoStatus)
}
