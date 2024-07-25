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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/registrystate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/snap"
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

		stdout, stderr, err := ctlcmd.Run(mockContext, strings.Fields(test.args), 0)
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
	setValue     interface{}
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
	setValue: map[string]interface{}{"sub3": "z"},
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
		tr.Set("test-snap", "root", map[string]interface{}{"key1": "a", "key2": "b", "key3": map[string]interface{}{"sub1": "x", "sub2": "y"}})
		tr.Commit()

		state.Unlock()

		mockContext.Lock()
		tr2 := configstate.ContextTransaction(mockContext)
		tr2.Set("test-snap", test.setPath, test.setValue)
		mockContext.Unlock()

		stdout, stderr, err := ctlcmd.Run(mockContext, strings.Fields(test.args), 0)
		c.Assert(err, IsNil)
		c.Assert(string(stderr), Equals, "")
		c.Check(string(stdout), Equals, test.stdout)

		// transaction not committed, drop it
		tr2 = nil

		// another transaction doesn't see uncommitted changes of tr2
		state.Lock()
		defer state.Unlock()
		tr3 := config.NewTransaction(state)
		var config map[string]interface{}
		c.Assert(tr3.Get("test-snap", "root", &config), IsNil)
		c.Assert(config, DeepEquals, map[string]interface{}{"key1": "a", "key2": "b", "key3": map[string]interface{}{"sub1": "x", "sub2": "y"}})
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
	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"get", "test-key1"}, 1000)
	c.Assert(err, IsNil)
	c.Assert(string(stdout), Equals, "test-value1\n")
	c.Assert(string(stderr), Equals, "")
}

func (s *getSuite) TestCommandWithoutContext(c *C) {
	_, _, err := ctlcmd.Run(nil, []string{"get", "foo"}, 0)
	c.Check(err, ErrorMatches, `cannot invoke snapctl operation commands \(here "get"\) from outside of a snap`)
}

func (s *setSuite) TestNull(c *C) {
	_, _, err := ctlcmd.Run(s.mockContext, []string{"set", "foo=null"}, 0)
	c.Check(err, IsNil)

	_, _, err = ctlcmd.Run(s.mockContext, []string{"set", `bar=[null]`}, 0)
	c.Check(err, IsNil)

	// Notify the context that we're done. This should save the config.
	s.mockContext.Lock()
	defer s.mockContext.Unlock()
	c.Check(s.mockContext.Done(), IsNil)

	// Verify config value
	var value interface{}
	tr := config.NewTransaction(s.mockContext.State())
	c.Assert(config.IsNoOption(tr.Get("test-snap", "foo", &value)), Equals, true)
	c.Assert(tr.Get("test-snap", "bar", &value), IsNil)
	c.Assert(value, DeepEquals, []interface{}{nil})
}

func (s *getAttrSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()

	state := state.New(nil)
	state.Lock()
	ch := state.NewChange("mychange", "mychange")

	attrsTask := state.NewTask("connect-task", "my connect task")
	attrsTask.Set("plug", &interfaces.PlugRef{Snap: "a", Name: "aplug"})
	attrsTask.Set("slot", &interfaces.SlotRef{Snap: "b", Name: "bslot"})
	staticPlugAttrs := map[string]interface{}{
		"aattr":   "foo",
		"baz":     []string{"a", "b"},
		"mapattr": map[string]interface{}{"mapattr1": "mapval1", "mapattr2": "mapval2"},
	}
	dynamicPlugAttrs := map[string]interface{}{
		"dyn-plug-attr": "c",
		"nilattr":       nil,
	}
	dynamicSlotAttrs := map[string]interface{}{
		"dyn-slot-attr": "d",
	}

	staticSlotAttrs := map[string]interface{}{
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

		stdout, stderr, err := ctlcmd.Run(s.mockPlugHookContext, strings.Fields(test.args), 0)
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

		stdout, stderr, err := ctlcmd.Run(s.mockSlotHookContext, strings.Fields(test.args), 0)
		if test.error != "" {
			c.Check(err, ErrorMatches, test.error)
		} else {
			c.Check(err, IsNil)
			c.Check(string(stderr), Equals, "")
			c.Check(string(stdout), Equals, test.stdout)
		}
	}
}

type registrySuite struct {
	testutil.BaseTest

	state     *state.State
	signingDB *assertstest.SigningDB
	devAccID  string

	mockContext *hookstate.Context
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&registrySuite{})

func (s *registrySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() {
		dirs.SetRootDir("/")
	})
	s.state = state.New(nil)

	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	c.Assert(db.Add(storeSigning.StoreAccountKey("")), IsNil)

	s.state.Lock()
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

	headers := map[string]interface{}{
		"authority-id": s.devAccID,
		"account-id":   s.devAccID,
		"name":         "network",
		"views": map[string]interface{}{
			"read-wifi": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid", "access": "read"},
					map[string]interface{}{"request": "password", "storage": "wifi.psk", "access": "read"},
				},
			},
			"write-wifi": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid", "access": "write"},
					map[string]interface{}{"request": "password", "storage": "wifi.psk", "access": "write"},
				},
			},
		},
		"timestamp": "2030-11-06T09:16:26Z",
	}

	body := []byte(`{
  "storage": {
    "schema": {
      "wifi": "any"
    }
  }
}`)

	as, err := s.signingDB.Sign(asserts.RegistryType, headers, body, "")
	c.Assert(err, IsNil)
	c.Assert(assertstate.Add(s.state, as), IsNil)

	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.state, repo)

	regIface := &ifacetest.TestInterface{InterfaceName: "registry"}
	err = repo.AddInterface(regIface)
	c.Assert(err, IsNil)

	snapYaml := fmt.Sprintf(`name: test-snap
type: app
version: 1
plugs:
  read-wifi:
    interface: registry
    account: %[1]s
    view: network/read-wifi
  write-wifi:
    interface: registry
    account: %[1]s
    view: network/write-wifi
    role: manager
`, s.devAccID)
	info := mockInstalledSnap(c, s.state, snapYaml, "")

	appSet, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	const coreYaml = `name: core
version: 1.0
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

	ref := &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: "read-wifi"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "registry-slot"},
	}
	_, err = repo.Connect(ref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	ref = &interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: "test-snap", Name: "write-wifi"},
		SlotRef: interfaces.SlotRef{Snap: "core", Name: "registry-slot"},
	}
	_, err = repo.Connect(ref, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	s.state.Unlock()

	// TODO: mock registry.RegistryTransaction for these tests and move all of
	// this mocking of assertions, iface connections, etc into a test suite of
	// RegistryTransaction in registrystate

	s.mockHandler = hooktest.NewMockHandler()
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: ""}

	s.mockContext, err = hookstate.NewContext(nil, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	s.mockContext.Lock()
	defer s.mockContext.Unlock()

	schema, err := registry.ParseSchema([]byte(`{ "schema": { "wifi": "any" } }`))
	c.Assert(err, IsNil)

	tx, err := registrystate.NewTransaction(s.state, false, s.devAccID, "network")
	c.Assert(err, IsNil)

	s.mockContext.OnDone(func() error {
		return tx.Commit(s.state, schema)
	})

	ctlcmd.MockGetTransaction(func(*hookstate.Context, *state.State, *registry.View) (*registrystate.Transaction, string, error) {
		return tx, "", nil
	})
}

func (s *registrySuite) TestRegistryGetSingleView(c *C) {
	s.state.Lock()
	err := registrystate.SetViaView(s.state, s.devAccID, "network", "write-wifi", map[string]interface{}{
		"ssid": "my-ssid",
	})
	s.state.Unlock()
	c.Assert(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi", "ssid"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, "my-ssid\n")
	c.Check(stderr, IsNil)
}

func (s *registrySuite) TestRegistryGetManyViews(c *C) {
	s.state.Lock()
	err := registrystate.SetViaView(s.state, s.devAccID, "network", "write-wifi", map[string]interface{}{
		"ssid":     "my-ssid",
		"password": "secret",
	})
	s.state.Unlock()
	c.Assert(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi", "ssid", "password"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, `{
	"password": "secret",
	"ssid": "my-ssid"
}
`)
	c.Check(stderr, IsNil)
}

func (s *registrySuite) TestRegistryGetNoRequest(c *C) {
	s.state.Lock()
	err := registrystate.SetViaView(s.state, s.devAccID, "network", "write-wifi", map[string]interface{}{
		"ssid":     "my-ssid",
		"password": "secret",
	})
	s.state.Unlock()
	c.Assert(err, IsNil)

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, `{
	"password": "secret",
	"ssid": "my-ssid"
}
`)
	c.Check(stderr, IsNil)
}

func (s *registrySuite) TestRegistryGetHappensTransactionally(c *C) {
	s.state.Lock()
	err := registrystate.SetViaView(s.state, s.devAccID, "network", "write-wifi", map[string]interface{}{
		"ssid": "my-ssid",
	})
	s.state.Unlock()
	c.Assert(err, IsNil)

	// registry transaction is created when snapctl runs for the first time
	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, `{
	"ssid": "my-ssid"
}
`)
	c.Check(stderr, IsNil)

	s.state.Lock()
	err = registrystate.SetViaView(s.state, s.devAccID, "network", "write-wifi", map[string]interface{}{
		"ssid": "other-ssid",
	})
	s.state.Unlock()
	c.Assert(err, IsNil)

	// the new write wasn't reflected because it didn't run in the same transaction
	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, `{
	"ssid": "my-ssid"
}
`)
	c.Check(stderr, IsNil)

	// make a new context so we get a new transaction
	s.state.Lock()
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "test-snap", Revision: snap.R(1), Hook: "test-hook"}
	s.mockContext, err = hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	s.state.Unlock()
	c.Assert(err, IsNil)

	// now we get the new data
	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0)
	c.Assert(err, IsNil)
	c.Check(string(stdout), Equals, `{
	"ssid": "other-ssid"
}
`)
	c.Check(stderr, IsNil)
}

func (s *registrySuite) TestRegistryGetInvalid(c *C) {
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
			err:  `cannot get registry: cannot find plug :non-existent for snap "test-snap"`,
		},
	}

	for _, tc := range tcs {
		stdout, stderr, err := ctlcmd.Run(s.mockContext, append([]string{"get", "--view"}, tc.args...), 0)
		c.Assert(err, ErrorMatches, tc.err)
		c.Check(stdout, IsNil)
		c.Check(stderr, IsNil)
	}
}

func (s *registrySuite) TestRegistryGetAndSetNonRegistryPlug(c *C) {
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

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":my-plug"}, 0)
	c.Assert(err, ErrorMatches, "cannot get registry: cannot use --view with non-registry plug :my-plug")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"set", "--view", ":my-plug", "ssid=my-ssid"}, 0)
	c.Assert(err, ErrorMatches, "cannot set registry: cannot use --view with non-registry plug :my-plug")
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}

func (s *registrySuite) TestRegistryGetAndSetAssertionNotFound(c *C) {
	storeSigning := assertstest.NewStoreStack("can0nical", nil)
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	c.Assert(db.Add(storeSigning.StoreAccountKey("")), IsNil)

	s.state.Lock()
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot get registry: registry assertion %s/network not found", s.devAccID))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "ssid=my-ssid"}, 0)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot set registry: registry assertion %s/network not found", s.devAccID))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

}

func (s *registrySuite) TestRegistryGetAndSetViewNotFound(c *C) {
	headers := map[string]interface{}{
		"authority-id": s.devAccID,
		"account-id":   s.devAccID,
		"revision":     "1",
		"name":         "network",
		"views": map[string]interface{}{
			"other": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "a", "storage": "a"},
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

	as, err := s.signingDB.Sign(asserts.RegistryType, headers, body, "")
	c.Assert(err, IsNil)
	s.state.Lock()
	c.Assert(assertstate.Add(s.state, as), IsNil)
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(s.mockContext, []string{"get", "--view", ":read-wifi"}, 0)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot get registry: view \"read-wifi\" not found in registry %s/network", s.devAccID))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)

	stdout, stderr, err = ctlcmd.Run(s.mockContext, []string{"set", "--view", ":write-wifi", "ssid=my-ssid"}, 0)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot set registry: view \"write-wifi\" not found in registry %s/network", s.devAccID))
	c.Check(stdout, IsNil)
	c.Check(stderr, IsNil)
}
