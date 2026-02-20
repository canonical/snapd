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
	"github.com/snapcore/snapd/logger"
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
	"github.com/snapcore/snapd/store"
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

	headers := map[string]any{
		"authority-id": devAccKey.AccountID(),
		"account-id":   devAccKey.AccountID(),
		"name":         "network",
		"views": map[string]any{
			"setup-wifi": map[string]any{
				"rules": []any{
					map[string]any{"request": "eph", "storage": "wifi.eph"},
					map[string]any{"request": "ssids", "storage": "wifi.ssids"},
					map[string]any{"request": "ssid", "storage": "wifi.ssid", "access": "read-write"},
					map[string]any{"request": "password", "storage": "wifi.psk", "access": "write"},
					map[string]any{"request": "status", "storage": "wifi.status", "access": "read"},
					map[string]any{"request": "private.{placeholder}", "storage": "private.{placeholder}"},
				},
			},
		},
		"timestamp": "2030-11-06T09:16:26Z",
	}
	body := []byte(`{
  "storage": {
    "schema": {
      "private": {
        "values": "any",
        "visibility": "secret"
      },
      "wifi": {
        "schema": {
          "eph": {
            "ephemeral": true,
            "type": "string"
          },
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

func parsePath(c *C, path string) []confdb.Accessor {
	opts := confdb.ParseOptions{AllowPlaceholders: true}
	accs, err := confdb.ParsePathIntoAccessors(path, opts)
	c.Assert(err, IsNil)
	return accs
}

func (s *confdbTestSuite) TestGetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)
	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)

	res, err := confdbstate.GetViaView(bag, view, []string{"ssid"}, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]any{"ssid": "foo"})
}

func (s *confdbTestSuite) TestGetViewUsedConstraints(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "private"), map[string]string{"foo": "bar", "baz": "zab"})
	c.Assert(err, IsNil)
	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)

	res, err := confdbstate.GetViaView(bag, view, []string{"private"}, map[string]any{"placeholder": "foo"}, 0)
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]any{"private": map[string]any{"foo": "bar"}})
}

func (s *confdbTestSuite) TestGetViewUnusedConstraints(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "private"), map[string]string{"foo": "bar", "baz": "zab"})
	c.Assert(err, IsNil)
	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)

	res, err := confdbstate.GetViaView(bag, view, []string{"private"}, map[string]any{"bla": "foo"}, 0)
	c.Assert(err, FitsTypeOf, &confdb.UnmatchedConstraintsError{})
	c.Assert(err, ErrorMatches, `.*no placeholder for constraint "bla".*`)
	c.Check(res, IsNil)
}

func (s *confdbTestSuite) TestGetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()

	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "other-view")
	c.Assert(err, FitsTypeOf, &confdbstate.NoViewError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot find view "other-view" in confdb schema %s/network`, s.devAccID))
	c.Check(view, IsNil)

	view, err = confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)

	res, err := confdbstate.GetViaView(bag, view, []string{"ssid"}, nil, 0)
	c.Assert(err, FitsTypeOf, &confdb.NoDataError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid" through %s/network/setup-wifi: no data`, s.devAccID))
	c.Check(res, IsNil)

	res, err = confdbstate.GetViaView(bag, view, []string{"ssid", "ssids"}, nil, 0)
	c.Assert(err, FitsTypeOf, &confdb.NoDataError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "ssid", "ssids" through %s/network/setup-wifi: no data`, s.devAccID))
	c.Check(res, IsNil)

	res, err = confdbstate.GetViaView(bag, view, []string{"other-field"}, nil, 0)
	c.Assert(err, FitsTypeOf, &confdb.NoMatchError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "other-field" through %s/network/setup-wifi: no matching rule`, s.devAccID))
	c.Check(res, IsNil)
}

func (s *confdbTestSuite) TestSetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()
	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)

	err = confdbstate.SetViaView(bag, view, map[string]any{"ssid": "foo"}, 0)
	c.Assert(err, IsNil)

	val, err := bag.Get(parsePath(c, "wifi.ssid"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "foo")
}

func (s *confdbTestSuite) TestSetNotFound(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()
	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)

	err = confdbstate.SetViaView(bag, view, map[string]any{"foo": "bar"}, 0)
	c.Assert(err, FitsTypeOf, &confdb.NoMatchError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot set "foo" through %s/network/setup-wifi: no matching rule`, s.devAccID))

	view, err = confdbstate.GetView(s.state, s.devAccID, "network", "other-view")
	c.Assert(err, FitsTypeOf, &confdbstate.NoViewError{})
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot find view "other-view" in confdb schema %s/network`, s.devAccID))
	c.Check(view, IsNil)
}

func (s *confdbTestSuite) TestUnsetView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)

	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)

	err = confdbstate.SetViaView(bag, view, map[string]any{"ssid": nil}, 0)
	c.Assert(err, IsNil)

	val, err := bag.Get(parsePath(c, "wifi.ssid"), nil)
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
	c.Assert(val, Equals, nil)
}

func (s *confdbTestSuite) TestConfdbstateGetEntireView(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "wifi.ssids"), []any{"foo", "bar"})
	c.Assert(err, IsNil)

	err = bag.Set(parsePath(c, "wifi.psk"), "pass")
	c.Assert(err, IsNil)

	err = bag.Set(parsePath(c, "private"), map[string]any{
		"a": 1,
		"b": 2,
	})
	c.Assert(err, IsNil)

	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)

	res, err := confdbstate.GetViaView(bag, view, nil, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, map[string]any{
		"ssids": []any{"foo", "bar"},
		"private": map[string]any{
			"a": float64(1),
			"b": float64(2),
		},
	})
}

func (s *confdbTestSuite) TestGetViewUsesFetchedAssertion(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	db := assertstate.DB(s.state)
	emptyDb, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
	})
	c.Assert(err, IsNil)
	assertstate.ReplaceDB(s.state, emptyDb)

	restore := confdbstate.MockFetchConfdbSchemaAssertion(func(*state.State, int, string, string) error {
		// use the DB with the assertion, to mock fetching the assertion
		assertstate.ReplaceDB(s.state, db.(*asserts.Database))
		return nil
	})
	defer restore()

	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err, IsNil)
	c.Assert(view, NotNil)
}

func (s *confdbTestSuite) TestGetViewFetchingStoreOffline(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	buf, restore := logger.MockLogger()
	defer restore()

	emptyDb, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
	})
	c.Assert(err, IsNil)
	assertstate.ReplaceDB(s.state, emptyDb)

	restore = confdbstate.MockFetchConfdbSchemaAssertion(func(*state.State, int, string, string) error {
		return store.ErrStoreOffline
	})
	defer restore()

	view, err := confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err.Error(), Equals, fmt.Sprintf("confdb-schema (network; account-id:%s) not found", s.devAccID))
	c.Assert(buf.String(), Matches, fmt.Sprintf(".*confdb-schema %s/network not found locally, fetching from store\n.*", s.devAccID)+store.ErrStoreOffline.Error()+"\n")
	c.Assert(view, IsNil)
}

func (s *confdbTestSuite) TestGetViewNoAssertion(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
	})
	c.Assert(err, IsNil)
	assertstate.ReplaceDB(s.state, db)

	restore := confdbstate.MockFetchConfdbSchemaAssertion(func(*state.State, int, string, string) error {
		// to avoid mocking the store for this one test
		return &asserts.NotFoundError{
			Headers: map[string]string{
				"account-id": s.devAccID,
				"name":       "network",
			},
			Type: asserts.ConfdbSchemaType,
		}
	})
	defer restore()

	_, err = confdbstate.GetView(s.state, s.devAccID, "network", "setup-wifi")
	c.Assert(err.Error(), Equals, fmt.Sprintf("confdb-schema (network; account-id:%s) not found", s.devAccID))
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
	schema, err := confdb.NewSchema(s.devAccID, "confdb", map[string]any{
		// exact match
		"view-1": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.bar", "storage": "foo.bar"},
			},
		},
		// unrelated
		"view-2": map[string]any{
			"rules": []any{
				map[string]any{"request": "bar", "storage": "bar"},
			},
		},
		// more specific
		"view-3": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.bar.baz", "storage": "foo.bar.baz"},
			},
		},
		// more generic but we won't connect a plug for this view
		"view-4": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
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

	path := parsePath(c, "foo")
	snapPlugs, err := confdbstate.GetPlugsAffectedByPaths(s.state, schema, [][]confdb.Accessor{path})
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
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
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
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
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
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	nonCustodians := []string{"test-snap-1", "test-snap-2"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
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
	custodians := map[string]confdbHooks{"test-custodian-snap": allHooks}
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

	s.repo.Disconnect("test-custodian-snap", "setup", "core", "confdb-slot")
	s.testConfdbTasksNoCustodian(c)
}

func (s *confdbTestSuite) TestConfdbTasksNoCustodianSnapInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no custodian snap is installed
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, nil, nonCustodians)
	s.testConfdbTasksNoCustodian(c)
}

func (s *confdbTestSuite) testConfdbTasksNoCustodian(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")

	// a non-custodian snap modifies a confdb
	_, err = confdbstate.CreateChangeConfdbTasks(s.state, tx, view, "test-snap-1")
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot commit changes to confdb made through view %s/network/%s: no custodian snap installed", s.devAccID, view.Name))
}

type confdbHooks uint8

const (
	changeView confdbHooks = 1 << iota
	saveView
	queryView
	loadView
	observeView

	end
)

func (c confdbHooks) toString() []string {
	allHooks := []string{"change-view-setup", "save-view-setup", "query-view-setup",
		"load-view-setup", "observe-view-setup"}
	if end != 1<<len(allHooks) {
		panic("confdb hook name lsit doesn't match confdbHooks values")
	}

	hooks := make([]string, 0, len(allHooks))
	for i := 0; i != int(end); i++ {
		if c&(1<<i) != 0 {
			hooks = append(hooks, allHooks[i])
		}
	}
	return hooks
}

const allHooks = observeView | queryView | loadView | saveView | changeView
const noHooks = confdbHooks(0)

func (s *confdbTestSuite) setupConfdbScenario(c *C, custodians map[string]confdbHooks, nonCustodians []string) {
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
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "confdb-slot"},
		}
		_, err = s.repo.Connect(ref, nil, nil, nil, nil, nil)
		c.Assert(err, IsNil)
	}

	// mock custodians
	for snap, hooks := range custodians {
		const isCustodian = true
		mockSnap(snap, isCustodian, hooks.toString())
	}

	// mock non-custodians
	hooks := []string{"observe-view-setup", "install"}
	for _, snap := range nonCustodians {
		const isCustodian = false
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
		c.Assert(storedTx.Set(parsePath(c, "foo"), "bar"), IsNil)
		saveChanges()

		tx = nil
		tx, _, _, err = confdbstate.GetStoredTransaction(t)
		c.Assert(err, IsNil)

		val, err := tx.Get(parsePath(c, "foo"), nil)
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
	hooks, restore := s.mockConfdbHooks()
	defer restore()

	restore = confdbstate.MockEnsureNow(func(*state.State) {
		s.checkOngoingWriteConfdbTx(c, s.devAccID, "network")

		go s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, nil)

	view := s.dbSchema.View("setup-wifi")

	tx, commitTxFunc, err := confdbstate.GetTransactionToSet(nil, s.state, view)
	c.Assert(err, IsNil)
	c.Assert(tx, NotNil)
	c.Assert(commitTxFunc, NotNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "foo")
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
	hooks, restore := s.mockConfdbHooks()
	defer restore()

	restore = confdbstate.MockEnsureNow(func(*state.State) {
		s.checkOngoingWriteConfdbTx(c, s.devAccID, "network")

		go s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	})
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

	ctx, err := hookstate.NewContext(nil, s.state, &hookstate.HookSetup{Snap: "test-snap"}, nil, "")
	c.Assert(err, IsNil)

	s.state.Unlock()
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=foo"}, 0, nil)
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
			_, _, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=foo"}, 0, nil)
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

	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

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

func (s *confdbTestSuite) mockConfdbHooks() (*[]string, func()) {
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

		// ignore non-confdb hooks
		if confdbstate.IsConfdbHookCtx(ctx) {
			hooks = append(hooks, hooksup.Hook)
		}

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
	val, err := tx.Get(parsePath(c, "wifi.ssid"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")
}

func (s *confdbTestSuite) TestGetTransactionFromChangeViewHook(c *C) {
	ctx := s.testGetReadableOngoingTransaction(c, "change-view-setup")

	// change-view hooks can also write to the transaction
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=bar"}, 0, nil)
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

	val, err := tx.Get(parsePath(c, "wifi.ssid"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")
}

func (s *confdbTestSuite) TestGetTransactionFromSaveViewHook(c *C) {
	ctx := s.testGetReadableOngoingTransaction(c, "save-view-setup")

	// non change-view hooks cannot modify the transaction
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=bar"}, 0, nil)
	c.Assert(err, ErrorMatches, `cannot modify confdb in "save-view-setup" hook`)
	c.Assert(stdout, IsNil)
	c.Assert(stderr, IsNil)
}

func (s *confdbTestSuite) TestGetTransactionFromViewChangedHook(c *C) {
	ctx := s.testGetReadableOngoingTransaction(c, "observe-view-setup")

	// non change-view hooks cannot modify the transaction
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"set", "--view", ":setup", "ssid=bar"}, 0, nil)
	c.Assert(err, ErrorMatches, `cannot modify confdb in "observe-view-setup" hook`)
	c.Assert(stdout, IsNil)
	c.Assert(stderr, IsNil)
}

func (s *confdbTestSuite) testGetReadableOngoingTransaction(c *C, hook string) *hookstate.Context {
	s.state.Lock()
	defer s.state.Unlock()

	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, []string{"test-snap"})

	originalTx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = originalTx.Set(parsePath(c, "wifi.ssid"), "foo")
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
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"get", "--view", ":setup", "ssid"}, 0, nil)
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
	confdb, err := confdb.NewSchema("foo", "bar", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
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
	custodians := map[string]confdbHooks{"test-custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, []string{"test-snap"})

	s.repo.Disconnect("test-custodian-snap", "setup", "core", "confdb-slot")
	s.testConfdbLoadNoCustodian(c)
}

func (s *confdbTestSuite) TestConfdbLoadNoCustodianInstalled(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// no custodian snap is installed
	s.setupConfdbScenario(c, nil, []string{"test-snap"})
	s.testConfdbLoadNoCustodian(c)
}

func (s *confdbTestSuite) testConfdbLoadNoCustodian(c *C) {
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")

	// a non-custodian snap modifies a confdb
	_, err = confdbstate.CreateLoadConfdbTasks(s.state, tx, view, []string{"ssid"}, nil)
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

	// find first task relevant to confdb (might be reading from another change)
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

	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	chg := s.state.NewChange("load-confdb", "")

	ts, err := confdbstate.CreateLoadConfdbTasks(s.state, tx, view, []string{"ssid"}, nil)
	c.Assert(err, IsNil)
	chg.AddAll(ts)

	cleanupTask, err := ts.Edge(confdbstate.ClearTxEdge)
	c.Assert(err, IsNil)
	c.Assert(cleanupTask.Kind(), Equals, "clear-confdb-tx")

	// the custodian snap's hooks are run
	tasks := []string{"clear-confdb-tx-on-error", "run-hook", "run-hook", "clear-confdb-tx"}
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
	custodians := map[string]confdbHooks{"custodian-snap": noHooks}
	s.setupConfdbScenario(c, custodians, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	ts, err := confdbstate.CreateLoadConfdbTasks(s.state, tx, view, []string{"ssid"}, nil)
	c.Assert(err, IsNil)
	// no hooks, nothing to run
	c.Assert(ts, IsNil)
}

func (s *confdbTestSuite) TestConfdbLoadTasks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// only one custodian snap is installed
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, nil)

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	ts, err := confdbstate.CreateLoadConfdbTasks(s.state, tx, view, []string{"ssid"}, nil)
	c.Assert(err, IsNil)
	chg := s.state.NewChange("get-confdb", "")
	chg.AddAll(ts)

	tasks := []string{"clear-confdb-tx-on-error", "run-hook", "run-hook", "clear-confdb-tx"}
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
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, nil)

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
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	// the non-custodian snap doesn't matter in the loading case but we can reuse
	// the helper to set it up with an install hook
	s.setupConfdbScenario(c, custodians, []string{"test-snap"})

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
	hooks, restore := s.mockConfdbHooks()
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
	err := bag.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view := s.dbSchema.View("setup-wifi")
	tx, err := confdbstate.GetTransactionForSnapctlGet(ctx, view, []string{"ssid"}, nil)
	c.Assert(err, IsNil)
	c.Assert(s.state.Changes(), HasLen, 1)

	val, err := tx.Get(parsePath(c, "wifi.ssid"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")

	chg := s.state.Changes()[0]
	s.checkGetConfdbTasks(c, chg, hooks)
	return chg
}

func (s *confdbTestSuite) TestGetTransactionInConfdbHook(c *C) {
	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "wifi.ssid"), "foo")
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
	tx, err := confdbstate.GetTransactionForSnapctlGet(ctx, view, []string{"ssid"}, nil)
	c.Assert(err, IsNil)
	// reads synchronously without creating new change or tasks
	c.Assert(s.state.Changes(), HasLen, 1)
	c.Assert(chg.Tasks(), HasLen, 2)

	val, err := tx.Get(parsePath(c, "wifi.ssid"), nil)
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")
}

func (s *confdbTestSuite) TestGetTransactionNoConfdbHooks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// the custodian snap has no snaps, no tasks should be scheduled
	custodians := map[string]confdbHooks{"custodian-snap": noHooks}
	s.setupConfdbScenario(c, custodians, nil)

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
	err = bag.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view := s.dbSchema.View("setup-wifi")
	tx, err := confdbstate.GetTransactionForSnapctlGet(ctx, view, []string{"ssid"}, nil)
	c.Assert(err, IsNil)
	c.Assert(tx, NotNil)

	// no tasks were scheduled
	c.Assert(s.state.Changes(), HasLen, 0)

	val, err := tx.Get(parsePath(c, "wifi.ssid"), nil)
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
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	s.setupConfdbScenario(c, custodians, nil)

	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(nil, s.state, nil, mockHandler, "")
	c.Assert(err, IsNil)

	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err = bag.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})
	s.state.Unlock()

	view := s.dbSchema.View("setup-wifi")
	ctx.Lock()
	defer ctx.Unlock()

	tx, err := confdbstate.GetTransactionForSnapctlGet(ctx, view, nil, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot load confdb %s/network in change 1: timed out after 0s", s.devAccID))
	c.Assert(tx, IsNil)
}

func (s *confdbTestSuite) checkGetConfdbTasks(c *C, chg *state.Change, executedHooks *[]string) {
	c.Assert(chg.Status(), Equals, state.DoneStatus)
	tasks := chg.Tasks()

	if tasks[0].Kind() == "run-hook" {
		// the test might be using an "install" hook as a starting point so it
		// shows up here
		var hooksup hookstate.HookSetup
		err := tasks[0].Get("hook-setup", &hooksup)
		c.Assert(err, IsNil)
		c.Assert(hooksup.Hook, Equals, "install")
		tasks = tasks[1:]
	}

	// first confdb-related task should be the clearing on error
	c.Assert(tasks[0].Kind(), Equals, "clear-confdb-tx-on-error")
	_, _, _, err := confdbstate.GetStoredTransaction(tasks[0])
	c.Assert(err, IsNil)
	tasks = tasks[1:]

	// check that the change's hooks and the actual executed hooks (if any) are in the right order
	expectedHooks := []string{"load-view-setup", "query-view-setup"}

	var i int
	for len(tasks) > 0 && tasks[0].Kind() == "run-hook" {
		var hooksup hookstate.HookSetup
		err := tasks[0].Get("hook-setup", &hooksup)
		c.Assert(err, IsNil)

		// check hook order
		if executedHooks != nil {
			c.Assert((*executedHooks)[i], Equals, expectedHooks[i])
		}
		c.Assert(hooksup.Hook, Equals, expectedHooks[i])
		i++

		tasks = tasks[1:]
	}

	// next task should be the clearing on success
	clearTask := tasks[0]
	c.Assert(clearTask.Kind(), Equals, "clear-confdb-tx")
	_, _, _, err = confdbstate.GetStoredTransaction(clearTask)
	c.Assert(err, IsNil)

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

func (s *confdbTestSuite) TestGetTransactionForAPI(c *C) {
	s.state.Lock()
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

	hooks, restore := s.mockConfdbHooks()
	defer restore()

	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "private"), map[string]string{"foo": "bar", "baz": "zab"})
	c.Assert(err, IsNil)
	err = bag.Set(parsePath(c, "wifi.ssids"), []string{"abc", "xyz"})
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view := s.dbSchema.View("setup-wifi")
	chgID, err := confdbstate.LoadConfdbAsync(s.state, view, []string{"private", "ssids"}, map[string]any{"placeholder": "foo"}, 0)
	c.Assert(err, IsNil)
	c.Assert(s.state.Changes(), HasLen, 1)

	s.state.Unlock()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.Change(chgID)
	s.checkGetConfdbTasks(c, chg, hooks)

	loadTask := chg.Tasks()[len(chg.Tasks())-1]
	c.Assert(loadTask.Kind(), Equals, "load-confdb-change")
	var viewName string
	c.Assert(loadTask.Get("view-name", &viewName), IsNil)
	c.Assert(viewName, Equals, "setup-wifi")

	// check the load task carries request/filtering data
	var requests []string
	c.Assert(loadTask.Get("requests", &requests), IsNil)
	c.Assert(requests, DeepEquals, []string{"private", "ssids"})
	var constraints map[string]string
	c.Assert(loadTask.Get("constraints", &constraints), IsNil)
	c.Assert(constraints, DeepEquals, map[string]string{"placeholder": "foo"})

	var apiData map[string]any
	err = chg.Get("api-data", &apiData)
	c.Assert(err, IsNil)
	val := apiData["values"]
	c.Assert(val, DeepEquals, map[string]any{
		"private": map[string]any{"foo": "bar"},
		"ssids":   []any{"abc", "xyz"},
	})
}

func (s *confdbTestSuite) TestGetTransactionForAPINoHooks(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	custodians := map[string]confdbHooks{"custodian-snap": noHooks}
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "private"), map[string]string{"foo": "bar", "baz": "zab"})
	c.Assert(err, IsNil)
	err = bag.Set(parsePath(c, "wifi.ssids"), []string{"abc", "xyz"})
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view := s.dbSchema.View("setup-wifi")
	requests := []string{"private", "ssids"}
	constraints := map[string]any{"placeholder": "foo"}
	chgID, err := confdbstate.LoadConfdbAsync(s.state, view, requests, constraints, 0)
	c.Assert(err, IsNil)
	c.Assert(s.state.Changes(), HasLen, 1)

	// no hooks so we loaded the data directly into the change
	chg := s.state.Change(chgID)
	c.Assert(chg.Tasks(), HasLen, 0)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	var ongoingTxs map[string]string
	err = s.state.Get("confdb-tx-commits", &ongoingTxs)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})

	var apiData map[string]any
	err = chg.Get("api-data", &apiData)
	c.Assert(err, IsNil)
	val := apiData["values"]
	c.Assert(val, DeepEquals, map[string]any{
		"ssids":   []any{"abc", "xyz"},
		"private": map[string]any{"foo": "bar"},
	})
}

func (s *confdbTestSuite) TestGetTransactionForAPINoHooksError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	custodians := map[string]confdbHooks{"custodian-snap": noHooks}
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

	view := s.dbSchema.View("setup-wifi")
	chgID, err := confdbstate.LoadConfdbAsync(s.state, view, []string{"ssid"}, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(s.state.Changes(), HasLen, 1)

	// no hooks so we loaded the data directly into the change
	chg := s.state.Change(chgID)
	c.Assert(chg.Tasks(), HasLen, 0)
	c.Assert(chg.Status(), Equals, state.DoneStatus)

	var ongoingTxs map[string]string
	err = s.state.Get("confdb-tx-commits", &ongoingTxs)
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})

	var apiData map[string]any
	err = chg.Get("api-data", &apiData)
	c.Assert(err, IsNil)
	errData, ok := apiData["error"].(map[string]any)
	c.Assert(ok, Equals, true, Commentf(`expected "error" in apiData to be map[string]any`))

	errStr := errData["message"].(string)
	errKind := errData["kind"].(string)
	c.Assert(errStr, Equals, fmt.Sprintf(`cannot get "ssid" through %s/network/setup-wifi: no data`, s.devAccID))
	c.Assert(errKind, Equals, "option-not-found")
}

func (s *confdbTestSuite) TestGetTransactionForAPIError(c *C) {
	s.state.Lock()
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

	_, restore := s.mockConfdbHooks()
	defer restore()

	view := s.dbSchema.View("setup-wifi")
	chgID, err := confdbstate.LoadConfdbAsync(s.state, view, []string{"ssid"}, nil, 0)
	c.Assert(err, IsNil)
	c.Assert(s.state.Changes(), HasLen, 1)

	s.state.Unlock()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.Change(chgID)
	s.checkGetConfdbTasks(c, chg, nil)

	var apiData map[string]any
	err = chg.Get("api-data", &apiData)
	c.Assert(err, IsNil, Commentf("%+v", chg))
	errData, ok := apiData["error"].(map[string]any)
	c.Assert(ok, Equals, true, Commentf(`expected "error" in apiData to be map[string]any`))

	errStr := errData["message"].(string)
	errKind := errData["kind"].(string)
	c.Assert(errStr, Equals, fmt.Sprintf(`cannot get "ssid" through %s/network/setup-wifi: no data`, s.devAccID))
	c.Assert(errKind, Equals, "option-not-found")
}

func (s *confdbTestSuite) TestConcurrentAccessWithOngoingWrite(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setupConfdbScenario(c, nil, nil)

	err := confdbstate.SetWriteTransaction(s.state, s.devAccID, "network", "1")
	c.Assert(err, IsNil)

	view := s.dbSchema.View("setup-wifi")
	// reading from API
	_, err = confdbstate.LoadConfdbAsync(s.state, view, []string{"ssid"}, nil, 0)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot access confdb view %s/network/setup-wifi: ongoing write transaction", s.devAccID))

	// reading from the snap
	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(nil, s.state, nil, mockHandler, "")
	c.Assert(err, IsNil)

	_, err = confdbstate.GetTransactionForSnapctlGet(ctx, view, nil, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot access confdb view %s/network/setup-wifi: ongoing write transaction", s.devAccID))

	// writing (used both from snap or API)
	_, _, err = confdbstate.GetTransactionToSet(nil, s.state, view)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot write confdb through view %s/network/setup-wifi: ongoing transaction", s.devAccID))
}

func (s *confdbTestSuite) TestConcurrentAccessWithOngoingRead(c *C) {
	s.state.Lock()
	// it's better not to have hooks here because if we do the GetTransactionForSnapctlGet
	// needs to schedule tasks and will block on them, making this test more timing based/annoying
	s.setupConfdbScenario(c, map[string]confdbHooks{"custodian-snap": noHooks}, nil)

	err := confdbstate.AddReadTransaction(s.state, s.devAccID, "network", "1")
	c.Assert(err, IsNil)
	s.state.Unlock()

	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(nil, s.state, nil, mockHandler, "")
	c.Assert(err, IsNil)

	ctx.Lock()
	defer ctx.Unlock()

	view := s.dbSchema.View("setup-wifi")
	// writing (used both from snap or API) conflicts
	_, _, err = confdbstate.GetTransactionToSet(ctx, s.state, view)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot write confdb through view %s/network/setup-wifi: ongoing transaction", s.devAccID))

	// we can read from the API and the snap concurrently with other reads
	_, err = confdbstate.LoadConfdbAsync(s.state, view, []string{"ssid"}, nil, 0)
	c.Assert(err, IsNil)

	_, err = confdbstate.GetTransactionForSnapctlGet(ctx, view, []string{"ssid"}, nil)
	c.Assert(err, IsNil)
}

func (s *confdbTestSuite) TestWriteAffectingEphemeralMustDefineSaveViewHook(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	hooks := observeView | queryView | loadView | changeView
	s.setupConfdbScenario(c, map[string]confdbHooks{"custodian-snap": hooks}, nil)

	view := s.dbSchema.View("setup-wifi")
	tx, commitTx, err := confdbstate.GetTransactionToSet(nil, s.state, view)
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "wifi.eph"), "foo")
	c.Assert(err, IsNil)

	// can't write an ephemeral path w/o a save-view hook
	_, _, err = commitTx()
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot access %s/network/setup-wifi: write might change ephemeral data but no custodians has a save-view hook", s.devAccID))

	err = tx.Clear(s.state)
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)

	// but we can if the path can't touch any ephemeral data
	_, _, err = commitTx()
	c.Assert(err, IsNil)
}

func (s *confdbTestSuite) TestReadCoveringEphemeralMustDefineLoadViewHook(c *C) {
	s.state.Lock()
	hooks := observeView | queryView | saveView | changeView
	s.setupConfdbScenario(c, map[string]confdbHooks{"custodian-snap": hooks}, nil)

	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(nil, s.state, nil, mockHandler, "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	ctx.Lock()
	view := s.dbSchema.View("setup-wifi")
	// can't read an ephemeral path w/o a load-view hook
	_, err = confdbstate.GetTransactionForSnapctlGet(ctx, view, []string{"eph"}, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot schedule tasks to access %s/network/setup-wifi: read might cover ephemeral data but no custodian has a load-view hook", s.devAccID))

	// so we don't block on the read
	restore := confdbstate.MockTransactionTimeout(0)
	defer restore()

	// but if the path isn't ephemeral it's fine
	_, err = confdbstate.GetTransactionForSnapctlGet(ctx, view, []string{"ssid"}, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot load confdb %s/network in change 1: timed out after 0s", s.devAccID))
	ctx.Unlock()

	s.state.Lock()
	defer s.state.Unlock()
	// can't read an ephemeral path w/o a load-view hook
	_, err = confdbstate.LoadConfdbAsync(s.state, view, []string{"eph"}, nil, 0)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot schedule tasks to access %s/network/setup-wifi: read might cover ephemeral data but no custodian has a load-view hook", s.devAccID))

	// but reading a non-ephemeral path succeeds
	_, err = confdbstate.LoadConfdbAsync(s.state, view, []string{"ssid"}, nil, 0)
	c.Assert(err, IsNil)
}

func (s *confdbTestSuite) TestBadPathHookChecks(c *C) {
	s.state.Lock()
	s.setupConfdbScenario(c, map[string]confdbHooks{"custodian-snap": allHooks}, nil)

	mockHandler := hooktest.NewMockHandler()
	ctx, err := hookstate.NewContext(nil, s.state, nil, mockHandler, "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	ctx.Lock()
	defer ctx.Unlock()
	view := s.dbSchema.View("setup-wifi")

	_, err = confdbstate.GetTransactionForSnapctlGet(ctx, view, []string{"foo"}, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "foo" through %s/network/setup-wifi: no matching rule`, s.devAccID))

	_, err = confdbstate.LoadConfdbAsync(s.state, view, []string{"foo"}, nil, 0)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "foo" through %s/network/setup-wifi: no matching rule`, s.devAccID))

	tx, commitTxFunc, err := confdbstate.GetTransactionToSet(nil, s.state, view)
	c.Assert(err, IsNil)
	// this shouldn't happen unless there's a mismatch between views and schemas but check we're robust
	c.Assert(tx.Set(parsePath(c, "foo"), "bar"), IsNil)
	_, _, err = commitTxFunc()
	c.Assert(err, ErrorMatches, `cannot check if write affects ephemeral data: cannot use "foo" as key in map`)
}

func (s *confdbTestSuite) TestEnsureLoopLogging(c *C) {
	testutil.CheckEnsureLoopLogging("confdbmgr.go", c, false)
}

func (s *confdbTestSuite) TestGetTransactionWithSecretVisibility(c *C) {
	s.state.Lock()
	custodians := map[string]confdbHooks{"custodian-snap": allHooks}
	nonCustodians := []string{"test-snap"}
	s.setupConfdbScenario(c, custodians, nonCustodians)

	_, restore := s.mockConfdbHooks()
	defer restore()

	// write some value for the get to read
	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "private"), map[string]any{
		"a": 1,
		"b": 2,
	})
	c.Assert(err, IsNil)

	s.state.Set("confdb-databags", map[string]map[string]confdb.JSONDatabag{s.devAccID: {"network": bag}})

	view := s.dbSchema.View("setup-wifi")
	chgID, err := confdbstate.LoadConfdbAsync(s.state, view, []string{"private"}, nil, 1000)
	c.Assert(err, IsNil)
	c.Assert(s.state.Changes(), HasLen, 1)

	s.state.Unlock()
	c.Assert(s.o.Settle(5*time.Second), IsNil)
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.Change(chgID)
	c.Assert(chg.Status(), Equals, state.ErrorStatus)

	loadTask := chg.Tasks()[len(chg.Tasks())-1]
	c.Assert(loadTask.Kind(), Equals, "load-confdb-change")
	var viewName string
	c.Assert(loadTask.Get("view-name", &viewName), IsNil)
	c.Assert(viewName, Equals, "setup-wifi")

	// check the load task carries request/filtering data
	var requests []string
	c.Assert(loadTask.Get("requests", &requests), IsNil)
	c.Assert(requests, DeepEquals, []string{"private"})
	var constraints map[string]string
	c.Assert(loadTask.Get("constraints", &constraints), IsNil)
	c.Assert(constraints, IsNil)

	// check the visibility set in the task
	var userID int
	c.Assert(loadTask.Get("userID", &userID), IsNil)
	c.Assert(userID, Equals, 1000)

	// check that no data got loaded
	var apiData map[string]any
	err = chg.Get("api-data", &apiData)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	log := loadTask.Log()
	c.Assert(log, HasLen, 1)
	c.Assert(log[0], Matches, fmt.Sprintf(`.*cannot get "private" through %s/network/setup-wifi: unauthorized access`, s.devAccID))
}
