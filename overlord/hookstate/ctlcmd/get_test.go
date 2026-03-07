// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package ctlcmd_test

import (
	"fmt"
	"reflect"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type getSuite struct {
	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

type getAttrSuite struct {
	mockPlugHookContext *hookstate.Context
	mockSlotHookContext *hookstate.Context
	mockHandler         *hooktest.MockHandler
}

var _ = Suite(&getSuite{})

var _ = Suite(&getAttrSuite{})

func (s *getSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	defer state.Unlock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	var err error
	s.mockContext, err = hookstate.NewContext(task, task.State(), setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	// Initialize configuration
	tr := config.NewTransaction(state)
	tr.Set("test-snap", "initial-key", "initial-value")
	tr.Commit()
}

var getTests = []struct {
	args, stdout, error string
}{{
	args:  "get",
	error: ".*get which option.*",
}, {
	args:  "get --plug key",
	error: "cannot use --plug or --slot without <snap>:<plug|slot> argument",
}, {
	args:  "get --slot key",
	error: "cannot use --plug or --slot without <snap>:<plug|slot> argument",
}, {
	args:  "get --foo",
	error: ".*unknown flag.*foo.*",
}, {
	args:  "get :foo bar",
	error: ".*interface attributes can only be read during the execution of interface hooks.*",
}, {
	args:   "get test-key1",
	stdout: "test-value1\n",
}, {
	args:   "get test-key2",
	stdout: "2\n",
}, {
	args:   "get missing-key",
	stdout: "\n",
}, {
	args:   "get -t test-key1",
	stdout: "\"test-value1\"\n",
}, {
	args:   "get -t test-key2",
	stdout: "2\n",
}, {
	args:   "get -t missing-key",
	stdout: "null\n",
}, {
	args:  "get -t test-key2.sub",
	error: "snap \"test-snap\" option \"test-key2\" is not a map",
}, {
	args:   "get -d test-key1",
	stdout: "{\n\t\"test-key1\": \"test-value1\"\n}\n",
}, {
	args:   "get test-key1 test-key2",
	stdout: "{\n\t\"test-key1\": \"test-value1\",\n\t\"test-key2\": 2\n}\n",
}}

func (s *getSuite) TestGetTests(c *C) {
	for _, test := range getTests {
		c.Logf("Test: %s", test.args)

		mockHandler := hooktest.NewMockHandler()

		state := state.New(nil)
		state.Lock()

		task := state.NewTask("test-task", "my test task")
		setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

		var err error
		mockContext, err := hookstate.NewContext(task, task.State(), setup, mockHandler, "")
		c.Check(err, IsNil)

		// Initialize configuration
		tr := config.NewTransaction(state)
		tr.Set("test-snap", "test-key1", "test-value1")
		tr.Set("test-snap", "test-key2", 2)
		tr.Commit()

		state.Unlock()

		stdout, stderr, err := ctlcmd.Run(mockContext, strings.Fields(test.args), 0, nil)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(string(stderr), Equals, "")
			c.Check(string(stdout), Equals, test.stdout)
		}
	}
}

var getTests2 = []struct {
	setPath      string
	setValue     any
	args, stdout string
}{{
	setPath:  "root.key1",
	setValue: "c",
	args:     "get root",
	stdout:   "{\n\t\"key1\": \"c\",\n\t\"key2\": \"b\",\n\t\"key3\": {\n\t\t\"sub1\": \"x\",\n\t\t\"sub2\": \"y\"\n\t}\n}\n",
}, {
	setPath:  "root.key3",
	setValue: "d",
	args:     "get root",
	stdout:   "{\n\t\"key1\": \"a\",\n\t\"key2\": \"b\",\n\t\"key3\": \"d\"\n}\n",
}, {
	setPath:  "root.key3.sub1",
	setValue: "z",
	args:     "get root.key3",
	stdout:   "{\n\t\"sub1\": \"z\",\n\t\"sub2\": \"y\"\n}\n",
}, {
	setPath:  "root.key3",
	setValue: map[string]any{"sub3": "z"},
	args:     "get root",
	stdout:   "{\n\t\"key1\": \"a\",\n\t\"key2\": \"b\",\n\t\"key3\": {\n\t\t\"sub3\": \"z\"\n\t}\n}\n",
}}

func (s *getSuite) TestGetPartialNestedStruct(c *C) {
	for _, test := range getTests2 {
		c.Logf("Test: %s", test.args)

		mockHandler := hooktest.NewMockHandler()

		state := state.New(nil)
		state.Lock()

		task := state.NewTask("test-task", "my test task")
		setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

		var err error
		mockContext, err := hookstate.NewContext(task, task.State(), setup, mockHandler, "")
		c.Check(err, IsNil)

		// Initialize configuration
		tr := config.NewTransaction(state)
		tr.Set("test-snap", "root", map[string]any{"key1": "a", "key2": "b", "key3": map[string]any{"sub1": "x", "sub2": "y"}})
		tr.Commit()

		state.Unlock()

		mockContext.Lock()
		tr2 := configstate.ContextTransaction(mockContext)
		tr2.Set("test-snap", test.setPath, test.setValue)
		mockContext.Unlock()

		stdout, stderr, err := ctlcmd.Run(mockContext, strings.Fields(test.args), 0, nil)
		c.Assert(err, IsNil)
		c.Assert(string(stderr), Equals, "")
		c.Check(string(stdout), Equals, test.stdout)

		// transaction not committed, drop it
		tr2 = nil

		// another transaction doesn't see uncommitted changes of tr2
		state.Lock()
		defer state.Unlock()
		tr3 := config.NewTransaction(state)
		var config map[string]any
		c.Assert(tr3.Get("test-snap", "root", &config), IsNil)
		c.Assert(config, DeepEquals, map[string]any{"key1": "a", "key2": "b", "key3": map[string]any{"sub1": "x", "sub2": "y"}})
	}
}

func (s *getSuite) TestGetRegularUser(c *C) {
	state := state.New(nil)
	state.Lock()

	task := state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	// Initialize configuration
	tr := config.NewTransaction(state)
	tr.Set("test-snap", "test-key1", "test-value1")
	tr.Commit()

	state.Unlock()

	mockHandler := hooktest.NewMockHandler()
	mockContext, err := hookstate.NewContext(task, task.State(), setup, mockHandler, "")
	c.Assert(err, IsNil)
	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"get", "test-key1"}, 1000, nil)
	c.Assert(err, IsNil)
	c.Assert(string(stdout), Equals, "test-value1\n")
	c.Assert(string(stderr), Equals, "")
}

func (s *getSuite) TestCommandWithoutContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"get", "foo"}, 0, nil)
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "get"\) from outside of a snap`)
}

func (s *setSuite) TestNull(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=null"}, 0, nil)
	c.Check(err, IsNil)

	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", `bar=[null]`}, 0, nil)
	c.Check(err, IsNil)

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify config value
	var value any
	tr := config.NewTransaction(s.mockContext.State())
	c.Assert(config.IsNoOption(tr.Get("test-snap", "foo", &value)), Equals, true)
	c.Assert(tr.Get("test-snap", "bar", &value), IsNil)
	c.Assert(value, DeepEquals, []any{nil})
}

func (s *getAttrSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	ch := state.NewChange("mychange", "mychange")

	attrsTask := state.NewTask("connect-task", "my connect task")
	attrsTask.Set("plug", &interfaces.PlugRef{Snap: "a", Name: "aplug"})
	attrsTask.Set("slot", &interfaces.SlotRef{Snap: "b", Name: "bslot"})
	staticPlugAttrs := map[string]any{
		"aattr":   "foo",
		"baz":     []string{"a", "b"},
		"mapattr": map[string]any{"mapattr1": "mapval1", "mapattr2": "mapval2"},
	}
	dynamicPlugAttrs := map[string]any{
		"dyn-plug-attr": "c",
		"nilattr":       nil,
	}
	dynamicSlotAttrs := map[string]any{
		"dyn-slot-attr": "d",
	}

	staticSlotAttrs := map[string]any{
		"battr": "bar",
	}
	attrsTask.Set("plug-static", staticPlugAttrs)
	attrsTask.Set("plug-dynamic", dynamicPlugAttrs)
	attrsTask.Set("slot-static", staticSlotAttrs)
	attrsTask.Set("slot-dynamic", dynamicSlotAttrs)
	ch.AddTask(attrsTask)
	state.Unlock()

	var err error

	// setup plug hook task
	state.Lock()
	plugHookTask := state.NewTask("run-hook", "my test task")
	state.Unlock()
	plugTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "connect-plug-aplug"}
	s.mockPlugHookContext, err = hookstate.NewContext(plugHookTask, plugHookTask.State(), plugTaskSetup, s.mockHandler, "")
	c.Assert(err, IsNil)

	s.mockPlugHookContext.Lock()
	s.mockPlugHookContext.Set("attrs-task", attrsTask.ID())
	s.mockPlugHookContext.Unlock()
	state.Lock()
	ch.AddTask(plugHookTask)
	state.Unlock()

	// setup slot hook task
	state.Lock()
	slotHookTask := state.NewTask("run-hook", "my test task")
	state.Unlock()
	slotTaskSetup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "connect-slot-aplug"}
	s.mockSlotHookContext, err = hookstate.NewContext(slotHookTask, slotHookTask.State(), slotTaskSetup, s.mockHandler, "")
	c.Assert(err, IsNil)

	s.mockSlotHookContext.Lock()
	s.mockSlotHookContext.Set("attrs-task", attrsTask.ID())
	s.mockSlotHookContext.Unlock()

	state.Lock()
	defer state.Unlock()
	ch.AddTask(slotHookTask)
}

var getPlugAttributesTests = []struct {
	args, stdout, error string
}{{
	args:   "get :aplug aattr",
	stdout: "foo\n",
}, {
	args:  "get :aplug aattr.sub",
	error: "snap \"test-snap\" attribute \"aattr\" is not a map",
}, {
	args:   "get -d :aplug baz",
	stdout: "{\n\t\"baz\": [\n\t\t\"a\",\n\t\t\"b\"\n\t]\n}\n",
}, {
	args:  "get :aplug",
	error: `.*get which attribute.*`,
}, {
	args:   "get :aplug mapattr.mapattr1",
	stdout: "mapval1\n",
}, {
	args:   "get -d :aplug mapattr.mapattr1",
	stdout: "{\n\t\"mapattr.mapattr1\": \"mapval1\"\n}\n",
}, {
	args:   "get :aplug dyn-plug-attr",
	stdout: "c\n",
}, {
	args:   "get -t :aplug nilattr",
	stdout: "null\n",
}, {
	// The --plug parameter doesn't do anything if used on plug side
	args:   "get --plug :aplug aattr",
	stdout: "foo\n",
}, {
	args:   "get --slot :aplug battr",
	stdout: "bar\n",
}, {
	args:  "get :aplug x",
	error: `no "x" attribute`,
}, {
	args:  "get :bslot x",
	error: `unknown plug or slot "bslot"`,
}, {
	args:  "get : foo",
	error: "plug or slot name not provided",
}}

func (s *getAttrSuite) TestPlugHookTests(c *C) {
	for _, test := range getPlugAttributesTests {
		c.Logf("Test: %s", test.args)

		stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, strings.Fields(test.args), 0, nil)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(string(stderr), Equals, "")
			c.Check(string(stdout), Equals, test.stdout)
		}
	}
}

var getSlotAttributesTests = []struct {
	args, stdout, error string
}{{
	args:   "get :bslot battr",
	stdout: "bar\n",
}, {
	args:   "get :bslot dyn-slot-attr",
	stdout: "d\n",
}, {
	// The --slot parameter doesn't do anything if used on slot side
	args:   "get --slot :bslot battr",
	stdout: "bar\n",
}, {
	args:   "get --plug :bslot aattr",
	stdout: "foo\n",
}, {
	args:  "get :bslot x",
	error: `no "x" attribute`,
}, {
	args:  "get :aplug x",
	error: `unknown plug or slot "aplug"`,
}, {
	args:  "get --slot --plug :aplug x",
	error: `cannot use --plug and --slot together`,
}}

func (s *getAttrSuite) TestSlotHookTests(c *C) {
	for _, test := range getSlotAttributesTests {
		c.Logf("Test: %s", test.args)

		stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, strings.Fields(test.args), 0, nil)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(string(stderr), Equals, "")
			c.Check(string(stdout), Equals, test.stdout)
		}
	}
}

type confdbSuite struct {
	testutil.BaseTest

	state     *state.State
	signingDB *assertstest.SigningDB
	devAccID  string

	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler

	repo *interfaces.Repository
}

var _ = Suite(&confdbSuite{})

func (s *confdbSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
	})

	s.mockHandler = hooktest.NewMockHandler()
	s.state = state.New(nil)
	s.state.Lock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	s.state.Unlock()

	var err error
	s.mockContext, err = hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	c.Assert(db.Add(storeSigning.StoreAccountKey("")), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	assertstate.ReplaceDB(s.state, db)

	// add developer1's account and account-key assertions
	devAcc := assertstest.NewAccount(storeSigning, "developer1", nil, "")
	c.Assert(storeSigning.Add(devAcc), IsNil)

	devPrivKey, _ := assertstest.GenerateKey(752)
	devAccKey := assertstest.NewAccountKey(storeSigning, devAcc, nil, devPrivKey.PublicKey(), "")
	s.devAccID = devAccKey.AccountID()

	assertstatetest.AddMany(s.state, storeSigning.StoreAccountKey(""), devAcc, devAccKey)

	s.signingDB = assertstest.NewSigningDB("developer1", devPrivKey)
	c.Check(s.signingDB, NotNil)
	c.Assert(storeSigning.Add(devAccKey), IsNil)

	headers := map[string]any{
		"authority-id": s.devAccID,
		"account-id":   s.devAccID,
		"name":         "network",
		"views": map[string]any{
			"read-wifi": map[string]any{
				"parameters": map[string]any{
					"field1": map[string]any{},
					"field2": map[string]any{},
				},
				"rules": []any{
					map[string]any{"request": "ssid", "storage": "wifi.ssid", "access": "read"},
					map[string]any{"request": "password", "storage": "wifi.psk", "access": "read"},
					map[string]any{"request": "foo", "storage": "foo[.field1={field1}][.field2={field2}]", "access": "read"},
				},
			},
			"write-wifi": map[string]any{
				"rules": []any{
					map[string]any{"request": "ssid", "storage": "wifi.ssid", "access": "write"},
					map[string]any{"request": "password", "storage": "wifi.psk", "access": "write"},
				},
			},
		},
		"timestamp": "2030-11-06T09:16:26Z",
	}

	body := []byte(`{
  "storage": {
    "schema": {
      "foo": "any",
      "wifi": {
        "schema": {
          "psk": {
            "type": "string",
            "visibility": "secret"
          },
          "ssid": "string"
        }
      }
    }
  }
}`)

	as, err := s.signingDB.Sign(asserts.ConfdbSchemaType, headers, body, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(s.state, as), IsNil)

	s.repo = interfaces.NewRepository()
	ifacerepo.Replace(s.state, s.repo)

	regIface := &ifacetest.TestInterface{InterfaceName: "confdb"}
	err = s.repo.AddInterface(regIface)
	c.Assert(err, IsNil)

	snapYaml := fmt.Sprintf(`name: test-snap
type: app
version: 1
plugs:
  read-wifi:
    interface: confdb
    account: %[1]s
    view: network/read-wifi
    role: observer
  write-wifi:
    interface: confdb
    account: %[1]s
    view: network/write-wifi
    role: custodian
  other:
    interface: confdb
    account: %[1]s
    view: other/other
`, s.devAccID)
	info := mockInstalledSnap(c, s.state, snapYaml, "")

	appSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1.0
type: os
slots:
 confdb-slot:
  interface: confdb
`
	info = mockInstalledSnap(c, s.state, coreYaml, "")

	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = s.repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	for _, plugName := range []string{"read-wifi", "write-wifi", "other"} {
		ref := &interfaces.ConnRef{
			PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: plugName},
			SlotRef: interfaces.SlotRef{Snap: "core", Name: "confdb-slot"},
		}
		_, err = s.repo.Connect(ref, nil, nil, nil, nil, nil)
		c.Assert(err, IsNil)
	}

	s.setConfdbFlag(true, c)
}

func (s *confdbSuite) setConfdbFlag(val bool, c *C) {
	tr := config.NewTransaction(s.state)
	_, confOption := features.Confdb.ConfigOption()
	err := tr.Set("core", confOption, val)
	c.Assert(err, IsNil)
	tr.Commit()
}

func (s *confdbSuite) TestConfdbGetSingleView(c *C) {
	s.state.Lock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
	c.Assert(err, IsNil)
	s.state.Unlock()

	restore := ctlcmd.MockConfdbstateTransactionForGet(func(ctx *hookstate.Context, view *confdb.View, requests []string, _ map[string]any) (*confdbstate.Transaction, error) {
		c.Assert(requests, DeepEquals, []string{"ssid"})
		c.Assert(view.Schema().Account, Equals, s.devAccID)
		c.Assert(view.Schema().Name, Equals, "network")
		return tx, nil
	})
	defer restore()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi", "ssid"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "my-ssid\n")
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbGetManyViews(c *C) {
	s.state.Lock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "wifi.psk"), "secret")
	c.Assert(err, IsNil)
	s.state.Unlock()

	restore := ctlcmd.MockConfdbstateTransactionForGet(func(ctx *hookstate.Context, view *confdb.View, requests []string, _ map[string]any) (*confdbstate.Transaction, error) {
		c.Assert(requests, DeepEquals, []string{"ssid", "password"})
		c.Assert(view.Schema().Account, Equals, s.devAccID)
		c.Assert(view.Schema().Name, Equals, "network")
		return tx, nil
	})
	defer restore()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi", "ssid", "password"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, `{
	"password": "secret",
	"ssid": "my-ssid"
}
`)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbGetNoRequest(c *C) {
	s.state.Lock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "wifi.ssid"), "my-ssid")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.psk"), "secret")
	c.Assert(err, IsNil)
	s.state.Unlock()

	restore := ctlcmd.MockConfdbstateTransactionForGet(func(ctx *hookstate.Context, view *confdb.View, requests []string, _ map[string]any) (*confdbstate.Transaction, error) {
		c.Assert(requests, IsNil)
		c.Assert(view.Schema().Account, Equals, s.devAccID)
		c.Assert(view.Schema().Name, Equals, "network")
		return tx, nil
	})
	defer restore()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, `{
	"password": "secret",
	"ssid": "my-ssid"
}
`)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbGetInvalid(c *C) {
	type testcase struct {
		args []string
		err  string
	}

	tcs := []testcase{
		{
			args: []string{"--slot", ":something"},
			err:  `cannot use --plug or --slot with --view`,
		},
		{
			args: []string{"--plug", ":something"},
			err:  `cannot use --plug or --slot with --view`,
		},
		{
			args: []string{":non-existent"},
			err:  `cannot find plug :non-existent for snap "test-snap"`,
		},
	}

	for _, tc := range tcs {
		stdout, stderr, err := ctlcmd.Run(s.mockContext, append([]string{"get", "--view"}, tc.args...), 0, nil)
		c.Assert(err, ErrorMatches, tc.err)
		c.Check(stdout, IsNil)
		c.Check(stderr, IsNil)
	}
}

func (s *confdbSuite) TestConfdbGetAndSetNonConfdbPlug(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
	})

	s.state.Lock()
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	err := repo.AddInterface(&ifacetest.TestInterface{InterfaceName: "random"})
	c.Assert(err, IsNil)

	snapYaml := `name: test-snap
type: app
version: 1
plugs:
  my-plug:
    interface: random
`
	info := mockInstalledSnap(c, s.state, snapYaml, "")

	appSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1.0
type: os
slots:
  my-slot:
    interface: random
`
	info = mockInstalledSnap(c, s.state, coreYaml, "")

	coreSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(coreSet)
	c.Assert(err, IsNil)

	ref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: "my-plug"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "my-slot"},
	}
	_, err = repo.Connect(ref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":my-plug"}, 0, nil)
	c.Assert(err, ErrorMatches, "cannot use --view with non-confdb plug :my-plug")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"set", "--view", ":my-plug", "ssid=my-ssid"}, 0, nil)
	c.Assert(err, ErrorMatches, "cannot use --view with non-confdb plug :my-plug")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbGetAndSetViewNotFound(c *C) {
	headers := map[string]any{
		"authority-id": s.devAccID,
		"account-id":   s.devAccID,
		"revision":     "1",
		"name":         "network",
		"views": map[string]any{
			"other": map[string]any{
				"rules": []any{
					map[string]any{"request": "a", "storage": "a"},
				},
			},
		},
		"timestamp": "2030-11-06T09:16:26Z",
	}

	body := []byte(`{
  "storage": {
    "schema": {
      "a": "any"
    }
  }
}`)

	as, err := s.signingDB.Sign(asserts.ConfdbSchemaType, headers, body, "")
	c.Assert(err, IsNil)
	s.state.Lock()
	c.Assert(assertstate.Add(s.state, as), IsNil)
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find view \"read-wifi\" in confdb schema %s/network", s.devAccID))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "ssid=my-ssid"}, 0, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot find view \"write-wifi\" in confdb schema %s/network", s.devAccID))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbGetPrevious(c *C) {
	s.state.Lock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)
	c.Assert(tx.Commit(s.state, confdb.NewJSONSchema()), IsNil)

	tx, err = confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "wifi.ssid"), "bar")
	c.Assert(err, IsNil)

	restore := ctlcmd.MockConfdbstateTransactionForGet(func(*hookstate.Context, *confdb.View, []string, map[string]any) (*confdbstate.Transaction, error) {
		return tx, nil
	})
	defer restore()

	task := s.state.NewTask("run-hook", "")
	setup := &hookstate.HookSetup{Snap: "test-snap", Hook: "save-view-plug"}
	ctx, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	// current transaction has uncommitted write "bar"
	stdout, stderr, err := ctlcmd.Run(ctx, []string{"get", "--view", ":read-wifi", "ssid"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "bar\n")
	c.Check(stderr, IsNil)

	// but --previous show "foo"
	stdout, stderr, err = ctlcmd.Run(ctx, []string{"get", "--view", "--previous", ":read-wifi", "ssid"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "foo\n")
	c.Check(stderr, IsNil)

	s.state.Lock()
	// simulate a commit and then a observe-view- hook
	c.Assert(tx.Commit(s.state, confdb.NewJSONSchema()), IsNil)
	setup = &hookstate.HookSetup{Snap: "test-snap", Hook: "observe-view-plug"}
	ctx, err = hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	// --previous in observe-view hook refers to pre-commit databag
	stdout, stderr, err = ctlcmd.Run(ctx, []string{"get", "--view", "--previous", ":read-wifi", "ssid"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "foo\n")
	c.Check(stderr, IsNil)

}

func (s *confdbSuite) TestConfdbGetDifferentViewThanOngoingTx(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	task := s.state.NewTask("run-hook", "")
	setup := &hookstate.HookSetup{Snap: "test-snap", Hook: "save-view-plug"}
	ctx, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)

	// set ongoing tx related to the network confdb
	task.Set("confdb-transaction", tx)

	s.state.Unlock()
	defer s.state.Lock()

	restore := ctlcmd.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		reg, err := confdb.NewSchema(s.devAccID, "other", map[string]any{
			"other": map[string]any{
				"rules": []any{
					map[string]any{"request": "ssid", "storage": "ssid"},
				},
			},
		}, confdb.NewJSONSchema())
		c.Assert(err, IsNil)
		return reg.View("other"), nil
	})
	defer restore()

	stdout, stderr, err := ctlcmd.Run(ctx, []string{"get", "--view", ":other", "ssid"}, 0, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot load confdb %[1]s/other: ongoing transaction for %[1]s/network`, s.devAccID))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbExperimentalFlag(c *C) {
	s.state.Lock()
	s.setConfdbFlag(false, c)
	s.state.Unlock()

	for _, cmd := range []string{"get", "set", "unset"} {
		stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{cmd, "--view", ":read-wifi"}, 0, nil)
		c.Assert(err, ErrorMatches, i18n.G(`"confdb" feature flag is disabled: set 'experimental.confdb' to true`))
		c.Check(stdout, IsNil)
		c.Check(stderr, IsNil)
	}
}

func (s *confdbSuite) TestConfdbGetPreviousInvalid(c *C) {
	restore := confdbstate.MockFetchConfdbSchemaAssertion(func(*state.State, int, string, string) error {
		return store.ErrStoreOffline
	})
	defer restore()

	// the parsing succeeded
	success := fmt.Sprintf(`confdb-schema (other; account-id:%s) not found`, s.devAccID)
	forbidMsg := `cannot use --previous outside of save-view, change-view or observe-view hooks`

	type testcase struct {
		hook string
		err  string
	}

	tcs := []testcase{
		{
			hook: "save-view-plug",
			err:  success,
		},
		{
			hook: "load-view-plug",
			err:  forbidMsg,
		},
		{
			hook: "change-view-plug",
			err:  success,
		},
		{
			hook: "query-view-pug",
			err:  forbidMsg,
		},
		{
			hook: "observe-view-plug",
			err:  success,
		},
		{
			hook: "",
			err:  forbidMsg,
		},
	}

	for _, tc := range tcs {
		var ctx *hookstate.Context
		var err error
		if tc.hook != "" {
			s.state.Lock()
			task := s.state.NewTask("run-hook", "hook-task")
			setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: tc.hook}
			s.state.Unlock()

			ctx, err = hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
			c.Assert(err, IsNil)
		} else {
			ctx, err = hookstate.NewContext(nil, s.state, nil, s.mockHandler, "")
			c.Assert(err, IsNil)
		}

		stdout, stderr, err := ctlcmd.Run(ctx, []string{"get", "--view", "--previous", ":other", "foo"}, 0, nil)
		c.Assert(err.Error(), Equals, tc.err)
		c.Check(stdout, IsNil)
		c.Check(stderr, IsNil)
	}

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--previous", ":other", "foo"}, 0, nil)
	c.Assert(err.Error(), Equals, "cannot use --previous without --view")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbAccessUnconnectedPlug(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)
	restore := ctlcmd.MockConfdbstateTransactionForGet(func(*hookstate.Context, *confdb.View, []string, map[string]any) (*confdbstate.Transaction, error) {
		c.Fatal("should not allow access to confdb")
		return tx, nil
	})
	defer restore()

	for _, plugName := range []string{"read-wifi", "write-wifi", "other"} {
		err := s.repo.Disconnect("test-snap", plugName, "core", "confdb-slot")
		c.Assert(err, IsNil)
	}

	s.state.Unlock()
	defer s.state.Lock()
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0, nil)
	c.Assert(err, ErrorMatches, "cannot access confdb through unconnected plug :read-wifi")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "ssid=my-ssid"}, 0, nil)
	c.Assert(err, ErrorMatches, "cannot access confdb through unconnected plug :write-wifi")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"unset", "--view", ":write-wifi", "ssid"}, 0, nil)
	c.Assert(err, ErrorMatches, "cannot access confdb through unconnected plug :write-wifi")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbDefaultMultipleKeys(c *C) {
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", "--default", "foo", ":write-wifi", "ssid", "password"}, 0, nil)
	c.Assert(err, ErrorMatches, "cannot use --default with more than one confdb request")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestDefaultNonConfdbRead(c *C) {
	s.state.Lock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}

	mockHandler := hooktest.NewMockHandler()
	mockContext, err := hookstate.NewContext(task, task.State(), setup, mockHandler, "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"get", "--default", "foo", "key"}, 0, nil)
	c.Assert(err, ErrorMatches, `cannot use --default with non-confdb read \(missing --view\)`)
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbDefaultIfNoData(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)
	restore := ctlcmd.MockConfdbstateTransactionForGet(func(*hookstate.Context, *confdb.View, []string, map[string]any) (*confdbstate.Transaction, error) {
		return tx, nil
	})
	defer restore()

	s.state.Unlock()
	defer s.state.Lock()
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", "--default", "bar", ":read-wifi", "password"}, 0, nil)
	c.Assert(err, IsNil)
	c.Check(string(stdout), DeepEquals, "bar\n")
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbDefaultNoFallbackIfTyped(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	err = tx.Set(parsePath(c, "wifi.ssid"), "foo")
	c.Assert(err, IsNil)
	restore := ctlcmd.MockConfdbstateTransactionForGet(func(*hookstate.Context, *confdb.View, []string, map[string]any) (*confdbstate.Transaction, error) {
		return tx, nil
	})
	defer restore()

	s.state.Unlock()
	defer s.state.Lock()
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", "--default", "bar", "-t", ":read-wifi", "password"}, 0, nil)
	c.Assert(err, ErrorMatches, "cannot unmarshal default value as strictly typed")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *confdbSuite) TestConfdbDefaultWithOtherFlags(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)

	restore := ctlcmd.MockConfdbstateTransactionForGet(func(*hookstate.Context, *confdb.View, []string, map[string]any) (*confdbstate.Transaction, error) {
		return tx, nil
	})
	defer restore()

	s.state.Unlock()
	defer s.state.Lock()

	type testcase struct {
		flags  []string
		output string
	}

	tcs := []testcase{
		{
			// if default isn't JSON, we fallback to treating it as a string (unless -t is on)
			flags:  []string{"--default", "foo"},
			output: "foo\n",
		},
		{
			flags: []string{"-d", "--default", "foo"},
			output: `{
	"ssid": "foo"
}
`,
		},
		{
			flags:  []string{"-t", "--default", `"1"`},
			output: "\"1\"\n",
		},
		{
			flags:  []string{"-t", "--default", `"foo"`},
			output: "\"foo\"\n",
		},
		{
			flags: []string{"-t", "--default", `{"baz":1}`},
			output: `{
	"baz": 1
}
`,
		},
		{
			flags: []string{"-d", "--default", `{"baz":1}`},
			output: `{
	"ssid": {
		"baz": 1
	}
}
`,
		},
		{
			flags:  []string{"-t", "--default", "123"},
			output: "123\n",
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("testcase %d", i+1)
		stdout, stderr, err := ctlcmd.Run(s.mockContext, append([]string{"get", "--view", ":read-wifi", "ssid"}, tc.flags...), 0, nil)
		c.Assert(err, IsNil, cmt)
		c.Check(string(stdout), DeepEquals, tc.output, cmt)
		c.Check(stderr, IsNil, cmt)
	}
}

func (s *confdbSuite) TestConfdbGetWithConstraints(c *C) {
	s.state.Lock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "foo"), map[string]string{"field1": "value1", "field2": "value2"})
	c.Assert(err, IsNil)
	s.state.Unlock()

	var gotConstraints map[string]any
	restore := ctlcmd.MockConfdbstateTransactionForGet(func(_ *hookstate.Context, _ *confdb.View, _ []string, constraints map[string]any) (*confdbstate.Transaction, error) {
		gotConstraints = constraints
		return tx, nil
	})
	defer restore()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi", "foo", "--with", "field1=value1", "--with", "field2=value2"}, 0, nil)
	expectedOutput := `{
	"field1": "value1",
	"field2": "value2"
}
`
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, expectedOutput)
	c.Check(stderr, IsNil)
	c.Check(gotConstraints, DeepEquals, map[string]any{"field1": "value1", "field2": "value2"})
}

func (s *confdbSuite) TestConfdbGetWithStrictConstraintsInvalid(c *C) {
	type testcase struct {
		constraint string
		err        string
	}

	tcs := []testcase{
		{
			constraint: "invalid",
			err:        `--with constraints must be in the form <param>=<constraint> but got "invalid" instead`,
		},
		{
			constraint: "invalid=",
			err:        `--with constraints must be in the form <param>=<constraint> but got "invalid=" instead`,
		},
		{
			constraint: "=invalid",
			err:        `--with constraints must be in the form <param>=<constraint> but got "=invalid" instead`,
		},
		{
			constraint: "=",
			err:        `--with constraints must be in the form <param>=<constraint> but got "=" instead`,
		},
		{
			constraint: "foo=bar",
			err:        `cannot unmarshal constraint as JSON as required by -t flag: bar`,
		},
		{
			constraint: "foo=[1,2,3]",
			err:        `--with constraints cannot take non-scalar JSON constraint: \[1,2,3\]`,
		},
		{
			constraint: `foo={"a":"b"}`,
			err:        `--with constraints cannot take non-scalar JSON constraint: {"a":"b"}`,
		},
		{
			constraint: "foo=null",
			err:        `--with constraints cannot take non-scalar JSON constraint: null`,
		},
	}

	for _, tc := range tcs {
		_, _, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", "-t", ":read-wifi", "ssid", "--with", tc.constraint}, 0, nil)
		c.Assert(err, ErrorMatches, tc.err)
	}
}

func (s *confdbSuite) TestWithNonConfdbRead(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"get", ":read-wifi", "ssid", "--with", "field=value"}, 0, nil)
	c.Assert(err, ErrorMatches, `cannot use --with with non-confdb read \(missing --view\)`)
}

func (s *confdbSuite) TestConfdbGetTypedConstraints(c *C) {
	type testcase struct {
		constraint string
		expected   any
	}

	tcs := []testcase{
		{
			constraint: `field1="foo"`,
			expected:   "foo",
		},
		{
			constraint: `field1=1.2`,
			expected:   1.2,
		},
		{
			constraint: `field1=2.0`,
			expected:   float64(2),
		},
		{
			constraint: `field1=true`,
			expected:   true,
		},
		// the following would be invalid with strict typing (-t) but we fallback
		// to interpreting them as strings
		{
			constraint: `field1=bar`,
			expected:   "bar",
		},
		{
			constraint: `field1=null`,
			expected:   "null",
		},
		{
			constraint: `field1=[1,2]`,
			expected:   "[1,2]",
		},
		{
			constraint: `field1={"a":"b"}`,
			expected:   `{"a":"b"}`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("testcase %d/%d", i+1, len(tcs))

		s.state.Lock()
		tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
		c.Assert(err, IsNil)
		err = tx.Set(parsePath(c, "foo"), map[string]any{"field1": tc.expected, "field2": "value2"})
		c.Assert(err, IsNil)
		s.state.Unlock()

		var gotConstraints map[string]any
		restore := ctlcmd.MockConfdbstateTransactionForGet(func(_ *hookstate.Context, _ *confdb.View, _ []string, constraints map[string]any) (*confdbstate.Transaction, error) {
			gotConstraints = constraints
			return tx, nil
		})

		stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi", "foo", "--with", tc.constraint}, 0, nil)
		var expectedOutput string
		if reflect.TypeOf(tc.expected).Kind() == reflect.String {
			// for string expected values, we need to quote them
			expectedOutput = fmt.Sprintf(`{
	"field1": %q,
	"field2": "value2"
}
`, tc.expected)
		} else {
			expectedOutput = fmt.Sprintf(`{
	"field1": %v,
	"field2": "value2"
}
`, tc.expected)
		}

		c.Assert(err, IsNil, cmt)
		c.Check(string(stdout), Equals, expectedOutput, cmt)
		c.Check(stderr, IsNil, cmt)
		c.Check(gotConstraints, DeepEquals, map[string]any{"field1": tc.expected}, cmt)

		restore()
	}
}

func (s *confdbSuite) TestConfdbGetSecretVisibility(c *C) {
	s.state.Lock()
	tx, err := confdbstate.NewTransaction(s.state, s.devAccID, "network")
	c.Assert(err, IsNil)
	err = tx.Set(parsePath(c, "wifi.psk"), "secret")
	c.Assert(err, IsNil)
	s.state.Unlock()

	restore := ctlcmd.MockConfdbstateTransactionForGet(func(ctx *hookstate.Context, view *confdb.View, requests []string, _ map[string]any) (*confdbstate.Transaction, error) {
		c.Assert(requests, DeepEquals, []string{"password"})
		c.Assert(view.Schema().Account, Equals, s.devAccID)
		c.Assert(view.Schema().Name, Equals, "network")
		return tx, nil
	})
	defer restore()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi", "password"}, 1000, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot get "password" through %s/network/read-wifi: unauthorized access`, s.devAccID))
	c.Check(stderr, IsNil)
	c.Assert(stdout, IsNil)
}
