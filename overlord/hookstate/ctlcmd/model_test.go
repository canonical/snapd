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
	"errors"
	"fmt"
	"os"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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
	mylog.Check(os.MkdirAll(dirs.SnapRunDir, 0755))

	mylog.Check(os.MkdirAll(dirs.SnapdStateDir(dirs.GlobalRootDir), 0755))


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

	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         s.storeSigning.Trusted,
		OtherPredefined: s.storeSigning.Generic,
	}))


	s.state.Lock()
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()
	s.AddCleanup(func() {
		s.state.Lock()
		assertstate.ReplaceDB(s.state, nil)
		s.state.Unlock()
	})
	mylog.Check(db.Add(s.storeSigning.StoreAccountKey("")))


	hookMgr := mylog.Check2(hookstate.Manager(s.state, s.o.TaskRunner()))


	devicestate.EarlyConfig = func(*state.State, func() (sysconfig.Device, *gadget.Info, error)) error {
		return nil
	}
	s.AddCleanup(func() { devicestate.EarlyConfig = nil })

	mgr := mylog.Check2(devicestate.Manager(s.state, hookMgr, s.o.TaskRunner(), s.newStore))


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
	s.state.Lock()
	defer s.state.Unlock()

	assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
	otherAcct := assertstest.NewAccount(s.storeSigning, "other-brand", map[string]interface{}{
		"account-id": "other-brand",
	}, "")
	assertstatetest.AddMany(s.state, otherAcct)
}

func (s *modelSuite) addSnapDeclaration(c *C, snapID, developerID, snapName string) {
	declA := mylog.Check2(s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"publisher-id": developerID,
		"snap-name":    snapName,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))

	mylog.Check(s.db.Add(declA))

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

var snapWithSnapdControlOnlyYaml = `
name: snap1-control
version: 1
plugs:
 snapd-control:
`

func (s *modelSuite) TestUnhappyModelCommandInsufficientPermissions(c *C) {
	// Verify we get an error in case that we do not match any of the three
	// criteria:
	// - snapd-control interface
	// - we are a gadget snap
	// - we come from the same publisher
	s.setupBrands()

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	mockInstalledSnap(c, s.state, snapBaseYaml, "")
	mockInstalledSnap(c, s.state, snapYaml, "")
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model"}, 0))
	c.Check(err, ErrorMatches, "insufficient permissions to get model assertion for snap \"snap1\"")
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "cannot get model assertion for snap \"snap1\": must be either a gadget snap, from the same publisher as the model or have the snapd-control interface\n")
}

func (s *modelSuite) TestHappyModelCommandIdenticalPublisher(c *C) {
	// Test that verifies we can get the model assertion if we are the publisher
	// of the snap that requests
	s.addSnapDeclaration(c, "snap1-id", "canonical", "snap1")
	s.setupBrands()

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	mockInstalledSnap(c, s.state, snapBaseYaml, "")
	mockInstalledSnap(c, s.state, snapYaml, "")
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model"}, 0))

	// For this test we just check that no error is returned, we have other testsw
	// that verifies formats for each case. So make sure that stderr is empty and that
	// we get data printed on stdout.
	c.Check(err, IsNil)
	c.Check(len(string(stdout)) > 0, Equals, true)
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandSnapdControlPlug(c *C) {
	// Verify that we can retrieve the model assertion in the case that we are
	// not a gadget snap, or from the same publisher, but we do have the snapd-control
	// interface connected.
	s.setupBrands()
	s.addSnapDeclaration(c, "snap1-control-id", "other-brand", "snap1-control")

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1-control", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	mockInstalledSnap(c, s.state, snapWithSnapdControlOnlyYaml, "")

	s.state.Set("conns", map[string]interface{}{
		"snap1-control:plug core:slot": map[string]interface{}{"interface": "snapd-control"},
	})
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model"}, 0))
	c.Check(err, IsNil)
	c.Check(len(string(stdout)) > 0, Equals, true)
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandPublisherYaml(c *C) {
	// Verify that we can read the model assertion when the snap has the same
	// publisher as the model assertion.
	s.addSnapDeclaration(c, "snap1-id", "canonical", "snap1")
	s.setupBrands()

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "snap1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	mockInstalledSnap(c, s.state, snapYaml, "")
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model"}, 0))
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, fmt.Sprintf(`brand-id:      canonical
model:         pc-model
serial:        -- (device not registered yet)
architecture:  amd64
base:          core18
gadget:        pc
kernel:        pc-kernel
timestamp:     %s
`, current.Timestamp().Format(time.RFC3339)))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandGadgetYaml(c *C) {
	// This tests verifies that a snap that is a gadget can be used to
	// get the model assertion, even if from a different publisher
	s.addSnapDeclaration(c, "gadget1-id", "canonical", "gadget1")
	s.setupBrands()

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model"}, 0))
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, fmt.Sprintf(`brand-id:      canonical
model:         pc-model
serial:        -- (device not registered yet)
architecture:  amd64
base:          core18
gadget:        pc
kernel:        pc-kernel
timestamp:     %s
`, current.Timestamp().Format(time.RFC3339)))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandGadgetJson(c *C) {
	s.addSnapDeclaration(c, "gadget1-id", "canonical", "gadget1")
	s.setupBrands()

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model", "--json"}, 0))
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
}`, current.Timestamp().Format(time.RFC3339)))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandAssertionGadgetYaml(c *C) {
	s.addSnapDeclaration(c, "gadget1-id", "canonical", "gadget1")
	s.setupBrands()

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model", "--assertion"}, 0))
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, string(asserts.Encode(current)))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestHappyModelCommandAssertionGadgetJson(c *C) {
	s.addSnapDeclaration(c, "gadget1-id", "canonical", "gadget1")
	s.setupBrands()

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	task := s.state.NewTask("test-task", "my test task")
	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1), Hook: "test-hook"}
	mockContext := mylog.Check2(hookstate.NewContext(task, s.state, setup, s.mockHandler, ""))

	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model", "--assertion", "--json"}, 0))
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
}`, current.SignKeyID(), current.Timestamp().Format(time.RFC3339)))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestRunWithoutHook(c *C) {
	s.addSnapDeclaration(c, "gadget1-id", "canonical", "gadget1")
	s.setupBrands()

	// set a model assertion
	s.state.Lock()
	current := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	mylog.Check(assertstate.Add(s.state, current))

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})


	setup := &hookstate.HookSetup{Snap: "gadget1", Revision: snap.R(1)}
	mockContext := mylog.Check2(hookstate.NewContext(nil, s.state, setup, nil, ""))

	mockInstalledSnap(c, s.state, snapGadgetYaml, "")
	s.state.Unlock()

	stdout, stderr := mylog.Check3(ctlcmd.Run(mockContext, []string{"model", "--json"}, 0))
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
}`, current.Timestamp().Format(time.RFC3339)))
	c.Check(string(stderr), Equals, "")
}

func (s *modelSuite) TestLongPublisherUnproven(c *C) {
	snapInfo := &snap.Info{
		Publisher: snap.StoreAccount{
			ID:          "canonical-id",
			Username:    "canonical",
			DisplayName: "Canonical",
			Validation:  "unproven",
		},
	}
	c.Assert(ctlcmd.FormatLongPublisher(snapInfo, ""), Equals, "Canonical")
}

func (s *modelSuite) TestLongPublisherStarred(c *C) {
	snapInfo := &snap.Info{
		Publisher: snap.StoreAccount{
			ID:          "canonical-id",
			Username:    "canonical",
			DisplayName: "Canonical",
			Validation:  "starred",
		},
	}
	c.Assert(ctlcmd.FormatLongPublisher(snapInfo, ""), Equals, "Canonical*")
}

func (s *modelSuite) TestLongPublisherVerified(c *C) {
	snapInfo := &snap.Info{
		Publisher: snap.StoreAccount{
			ID:          "canonical-id",
			Username:    "canonical",
			DisplayName: "Canonical",
			Validation:  "verified",
		},
	}
	c.Assert(ctlcmd.FormatLongPublisher(snapInfo, ""), Equals, "Canonical**")
}

func (s *modelSuite) signSerial(accountID, model, serial string, timestamp time.Time, extras ...map[string]interface{}) *asserts.Serial {
	encodedPubKey, _ := asserts.EncodePublicKey(brandPrivKey2.PublicKey())
	headers := map[string]interface{}{
		"series":              "16",
		"serial":              serial,
		"brand-id":            accountID,
		"model":               model,
		"timestamp":           timestamp.Format(time.RFC3339),
		"device-key":          string(encodedPubKey),
		"device-key-sha3-384": brandPrivKey2.PublicKey().ID(),
	}
	for _, extra := range extras {
		for k, v := range extra {
			headers[k] = v
		}
	}

	signer := s.brands.Signing(accountID)

	serialAs := mylog.Check2(signer.Sign(asserts.SerialType, headers, nil, ""))

	return serialAs.(*asserts.Serial)
}

func (s *modelSuite) TestFindSerialAssertionNone(c *C) {
	s.setupBrands()
	model := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})

	s.state.Lock()
	defer s.state.Unlock()
	assertstatetest.AddMany(s.state, model)

	result := mylog.Check2(ctlcmd.FindSerialAssertion(s.state, model))
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	c.Assert(result, IsNil)
}

func (s *modelSuite) TestFindSerialAssertionMatch(c *C) {
	s.setupBrands()
	model := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	serial := s.signSerial("canonical", "pc-model", "1", time.Now(), map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})

	s.state.Lock()
	defer s.state.Unlock()
	assertstatetest.AddMany(s.state, model, serial)

	result := mylog.Check2(ctlcmd.FindSerialAssertion(s.state, model))

	c.Check(result.Serial(), Equals, "1")
}

func (s *modelSuite) TestFindSerialAssertionMultiple(c *C) {
	// In case of multiple matches, we should return the one with the
	// newest timestamp.
	s.setupBrands()

	now := time.Now()
	tomorrow := now.AddDate(0, 0, 1)

	model := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	serial := s.signSerial("canonical", "pc-model", "1", now, map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	serialnext := s.signSerial("canonical", "pc-model", "2", tomorrow, map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})

	s.state.Lock()
	defer s.state.Unlock()
	assertstatetest.AddMany(s.state, model, serial, serialnext)

	result := mylog.Check2(ctlcmd.FindSerialAssertion(s.state, model))

	c.Check(result.Timestamp(), Equals, serialnext.Timestamp())
}
