// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
)

type modelSuite struct {
	testutil.BaseTest

	o           *overlord.Overlord
	state       *state.State
	hookMgr     *hookstate.HookManager
	mgr         *devicestate.DeviceManager
	db          *asserts.Database
	mockHandler *hooktest.MockHandler

	storeSigning *assertstest.StoreStack
	brands       *assertstest.SigningAccounts

	newFakeStore func(storecontext.DeviceBackend) snapstate.StoreService
}

type fakeSnapStore struct {
	storetest.Store

	state *state.State
	db    asserts.RODatabase
}

var _ = Suite(&modelSuite{})

var (
	brandPrivKey, _  = assertstest.GenerateKey(752)
	brandPrivKey2, _ = assertstest.GenerateKey(752)
)

func (s *modelSuite) newStore(devBE storecontext.DeviceBackend) snapstate.StoreService {
	return s.newFakeStore(devBE)
}

func (s *modelSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	err := os.MkdirAll(dirs.SnapRunDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapdStateDir(dirs.GlobalRootDir), 0755)
	c.Assert(err, IsNil)

	s.AddCleanup(osutil.MockMountInfo(``))

	s.o = overlord.Mock()
	s.state = s.o.State()
	s.mockHandler = hooktest.NewMockHandler()
	s.storeSigning = assertstest.NewStoreStack("canonical", nil)

	s.AddCleanup(sysdb.MockGenericClassicModel(s.storeSigning.GenericClassicModel))

	s.brands = assertstest.NewSigningAccounts(s.storeSigning)
	s.brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"display-name": "fancy model publisher",
		"validation":   "certified",
	})
	s.brands.Register("rereg-brand", brandPrivKey2, nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	})
	c.Assert(err, IsNil)

	s.state.Lock()
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()
	s.AddCleanup(func() {
		s.state.Lock()
		assertstate.ReplaceDB(s.state, nil)
		s.state.Unlock()
	})

	err = db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	hookMgr, err := hookstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)

	devicestate.EarlyConfig = func(*state.State, func() (sysconfig.Device, *gadget.Info, error)) error {
		return nil
	}
	s.AddCleanup(func() { devicestate.EarlyConfig = nil })

	mgr, err := devicestate.Manager(s.state, hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	s.db = db
	s.hookMgr = hookMgr
	s.o.AddManager(s.hookMgr)
	s.mgr = mgr
	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())

	s.state.Lock()
	snapstate.ReplaceStore(s.state, &fakeSnapStore{
		state: s.state,
		db:    s.storeSigning,
	})
	s.state.Unlock()

	s.AddCleanup(func() { s.newFakeStore = nil })
}

func (s *modelSuite) setupBrands() {
	assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
	otherAcct := assertstest.NewAccount(s.storeSigning, "other-brand", map[string]interface{}{
		"account-id": "other-brand",
	}, "")
	assertstatetest.AddMany(s.state, otherAcct)
}

const snapGadgetYaml = `name: gadget1
type: gadget
version: 1
`

const snapBaseYaml = `name: snap1-base
type: base
version: 1
`

const snapYaml = `name: snap1
base: snap1-base
version: 1
`

func (s *modelSuite) TestUnhappyModelCommandNotGadgetOrSamePublisher(c *C) {
	// Make sure that we can not get the model assertion if we are not a gadget
	// type snap, or if we are not the publisher of the model assertion.
	s.state.Lock()
	s.setupBrands()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	c.Assert(err, IsNil)
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	mockInstalledSnap(c, s.state, snapBaseYaml, "")
	mockInstalledSnap(c, s.state, snapYaml, "")
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"model"}, 0)
	c.Check(err, ErrorMatches, "insufficient permissions to get model assertion for snap \"snap1\"")
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "cannot get model assertion for snap \"snap1\": not a gadget or from the same brand as the device model assertion\n")
}

func (s *modelSuite) TestHappyModelCommandPublisherYaml(c *C) {
	// Make sure that we can get the model assertion even if the snap is
	// is not a gadget, but comes from the same publisher as the model
	s.state.Lock()
	s.setupBrands()

	// set a model assertion
	current := s.brands.Model("my-brand", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "pc-model",
	})

	c.Assert(err, IsNil)
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"model"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, fmt.Sprintf(`brand-id:      my-brand
model:         pc-model
serial:        -- (device not registered yet)
architecture:  amd64
base:          core18
gadget:        pc
kernel:        pc-kernel
timestamp:     %s
`, timeutil.Human(time.Now())))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandGadgetYaml(c *C) {
	// This tests verifies that a snap that is a gadget can be used to
	// get the model assertion, even if from a different publisher
	s.state.Lock()
	s.setupBrands()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	c.Assert(err, IsNil)
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"model"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, fmt.Sprintf(`brand-id:      canonical
model:         pc-model
serial:        -- (device not registered yet)
architecture:  amd64
base:          core18
gadget:        pc
kernel:        pc-kernel
timestamp:     %s
`, timeutil.Human(time.Now())))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandGadgetJson(c *C) {
	s.state.Lock()
	s.setupBrands()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	c.Assert(err, IsNil)
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"model", "--json"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, fmt.Sprintf(`{
  "architecture": "amd64",
  "base": "core18",
  "brand-id": "canonical",
  "gadget": "pc",
  "kernel": "pc-kernel",
  "model": "pc-model",
  "serial": null,
  "timestamp": "%s"
}`, time.Now().Format(time.RFC3339)))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandAssertionGadgetYaml(c *C) {
	s.state.Lock()
	s.setupBrands()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	c.Assert(err, IsNil)
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"model", "--assertion"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, string(asserts.Encode(current)))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandAssertionGadgetJson(c *C) {
	s.state.Lock()
	s.setupBrands()

	// set a model assertion
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	err := assertstate.Add(s.state, current)
	c.Assert(err, IsNil)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})

	c.Assert(err, IsNil)
	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext, err := hookstate.NewContext(task, s.state, setup, s.mockHandler, "")
	c.Assert(err, IsNil)
	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr, err := ctlcmd.Run(mockContext, []string{"model", "--assertion", "--json"}, 0)
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, fmt.Sprintf(`{
  "headers": {
    "architecture": "amd64",
    "authority-id": "canonical",
    "base": "core18",
    "brand-id": "canonical",
    "gadget": "pc",
    "kernel": "pc-kernel",
    "model": "pc-model",
    "series": "16",
    "sign-key-sha3-384": "%s",
    "timestamp": "%s",
    "type": "model"
  }
}`, current.SignKeyID(), time.Now().Format(time.RFC3339)))
	c.Check(string(stderr), Equals, "")
}
