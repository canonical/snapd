// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2019 Canonical Ltd
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

package devicestate_test

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/timings"
)

func TestDeviceManager(t *testing.T) { TestingT(t) }

type deviceMgrBaseSuite struct {
	o       *overlord.Overlord
	state   *state.State
	se      *overlord.StateEngine
	hookMgr *hookstate.HookManager
	mgr     *devicestate.DeviceManager
	db      *asserts.Database

	bootloader *bootloadertest.MockBootloader

	storeSigning *assertstest.StoreStack
	brands       *assertstest.SigningAccounts

	ancillary []asserts.Assertion

	restartRequests []state.RestartType

	restoreOnClassic         func()
	restoreGenericClassicMod func()
	restoreSanitize          func()

	newFakeStore func(storecontext.DeviceBackend) snapstate.StoreService
}

type deviceMgrSuite struct {
	deviceMgrBaseSuite
}

var _ = Suite(&deviceMgrSuite{})

type fakeStore struct {
	storetest.Store

	state *state.State
	db    asserts.RODatabase
}

func (sto *fakeStore) pokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	sto.state.Lock()
	sto.state.Unlock()
}

func (sto *fakeStore) Assertion(assertType *asserts.AssertionType, key []string, _ *auth.UserState) (asserts.Assertion, error) {
	sto.pokeStateLock()
	ref := &asserts.Ref{Type: assertType, PrimaryKey: key}
	return ref.Resolve(sto.db.Find)
}

var (
	brandPrivKey, _  = assertstest.GenerateKey(752)
	brandPrivKey2, _ = assertstest.GenerateKey(752)
)

func (s *deviceMgrBaseSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(dirs.SnapRunDir, 0755)

	s.restartRequests = nil

	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.bootloader)

	s.restoreOnClassic = release.MockOnClassic(false)

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)
	s.o = overlord.MockWithRestartHandler(func(req state.RestartType) {
		s.restartRequests = append(s.restartRequests, req)
	})
	s.state = s.o.State()
	s.state.Lock()
	s.state.VerifyReboot("boot-id-0")
	s.state.Unlock()
	s.se = s.o.StateEngine()

	s.restoreGenericClassicMod = sysdb.MockGenericClassicModel(s.storeSigning.GenericClassicModel)

	s.brands = assertstest.NewSigningAccounts(s.storeSigning)
	s.brands.Register("my-brand", brandPrivKey, nil)
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

	err = db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	hookMgr, err := hookstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)
	mgr, err := devicestate.Manager(s.state, hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	s.db = db
	s.hookMgr = hookMgr
	s.o.AddManager(s.hookMgr)
	s.mgr = mgr
	s.o.AddManager(s.mgr)
	s.o.AddManager(s.o.TaskRunner())

	// For triggering errors
	erroringHandler := func(task *state.Task, _ *tomb.Tomb) error {
		return errors.New("error out")
	}
	s.o.TaskRunner().AddHandler("error-trigger", erroringHandler, nil)

	c.Assert(s.o.StartUp(), IsNil)

	s.state.Lock()
	snapstate.ReplaceStore(s.state, &fakeStore{
		state: s.state,
		db:    s.storeSigning,
	})
	s.state.Unlock()
}

func (s *deviceMgrBaseSuite) newStore(devBE storecontext.DeviceBackend) snapstate.StoreService {
	return s.newFakeStore(devBE)
}

func (s *deviceMgrBaseSuite) TearDownTest(c *C) {
	s.ancillary = nil
	s.state.Lock()
	assertstate.ReplaceDB(s.state, nil)
	s.state.Unlock()
	bootloader.Force(nil)
	dirs.SetRootDir("")
	s.restoreGenericClassicMod()
	s.restoreOnClassic()
	s.restoreSanitize()
}

var settleTimeout = 15 * time.Second

func (s *deviceMgrBaseSuite) settle(c *C) {
	err := s.o.Settle(settleTimeout)
	c.Assert(err, IsNil)
}

// seeding avoids triggering a real full seeding, it simulates having it in process instead
func (s *deviceMgrBaseSuite) seeding() {
	chg := s.state.NewChange("seed", "Seed system")
	chg.SetStatus(state.DoingStatus)
}

func (s *deviceMgrBaseSuite) signSerial(c *C, bhv *devicestatetest.DeviceServiceBehavior, headers map[string]interface{}, body []byte) (serial asserts.Assertion, ancillary []asserts.Assertion, err error) {
	brandID := headers["brand-id"].(string)
	model := headers["model"].(string)
	keyID := ""

	var signing assertstest.SignerDB = s.storeSigning

	switch model {
	case "pc", "pc2":
	case "classic-alt-store":
		c.Check(brandID, Equals, "canonical")
	case "generic-classic":
		c.Check(brandID, Equals, "generic")
		headers["authority-id"] = "generic"
		keyID = s.storeSigning.GenericKey.PublicKeyID()
	case "rereg-model":
		headers["authority-id"] = "rereg-brand"
		signing = s.brands.Signing("rereg-brand")
	default:
		c.Fatalf("unknown model: %s", model)
	}
	a, err := signing.Sign(asserts.SerialType, headers, body, keyID)
	return a, s.ancillary, err
}

func (s *deviceMgrBaseSuite) mockServer(c *C, reqID string, bhv *devicestatetest.DeviceServiceBehavior) *httptest.Server {
	if bhv == nil {
		bhv = &devicestatetest.DeviceServiceBehavior{}
	}

	bhv.ReqID = reqID
	bhv.SignSerial = s.signSerial
	bhv.ExpectedCapabilities = "serial-stream"

	return devicestatetest.MockDeviceService(c, bhv)
}

func (s *deviceMgrSuite) SetUpTest(c *C) {
	s.deviceMgrBaseSuite.SetUpTest(c)
}

func (s *deviceMgrSuite) TearDownTest(c *C) {
	s.deviceMgrBaseSuite.TearDownTest(c)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeededAlreadySeeded(c *C) {
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State, *devicestate.PopulateStateFromSeedOptions, timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeeded(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeededChangeInFlight(c *C) {
	s.state.Lock()
	chg := s.state.NewChange("seed", "just for testing")
	chg.AddTask(s.state.NewTask("test-task", "the change needs a task"))
	s.state.Unlock()

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State, *devicestate.PopulateStateFromSeedOptions, timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeeded(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeededAlsoOnClassic(c *C) {
	release.OnClassic = true

	called := false
	restore := devicestate.MockPopulateStateFromSeed(func(*state.State, *devicestate.PopulateStateFromSeedOptions, timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeeded(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeededHappy(c *C) {
	restore := devicestate.MockPopulateStateFromSeed(func(st *state.State, opts *devicestate.PopulateStateFromSeedOptions, tm timings.Measurer) (ts []*state.TaskSet, err error) {
		c.Assert(opts, IsNil)
		t := s.state.NewTask("test-task", "a random task")
		ts = append(ts, state.NewTaskSet(t))
		return ts, nil
	})
	defer restore()

	err := devicestate.EnsureSeeded(s.mgr)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.state.Changes(), HasLen, 1)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkSkippedOnClassic(c *C) {
	s.bootloader.GetErr = fmt.Errorf("should not be called")
	release.OnClassic = true

	err := devicestate.EnsureBootOk(s.mgr)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkSkippedOnNonRunModes(c *C) {
	s.bootloader.GetErr = fmt.Errorf("should not be called")
	devicestate.SetOperatingMode(s.mgr, "install")

	err := devicestate.EnsureBootOk(s.mgr)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeededHappyWithModeenv(c *C) {
	n := 0
	restore := devicestate.MockPopulateStateFromSeed(func(st *state.State, opts *devicestate.PopulateStateFromSeedOptions, tm timings.Measurer) (ts []*state.TaskSet, err error) {
		c.Assert(opts, NotNil)
		c.Check(opts.Label, Equals, "20191127")
		c.Check(opts.Mode, Equals, "install")

		t := s.state.NewTask("test-task", "a random task")
		ts = append(ts, state.NewTaskSet(t))

		n++
		return ts, nil
	})
	defer restore()

	// mock the modeenv file
	m := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191127",
	}
	err := m.Write("")
	c.Assert(err, IsNil)

	// re-create manager so that modeenv file is-read
	s.mgr, err = devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	err = devicestate.EnsureSeeded(s.mgr)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(n, Equals, 1)
}

func (s *deviceMgrBaseSuite) makeModelAssertionInState(c *C, brandID, model string, extras map[string]interface{}) *asserts.Model {
	modelAs := s.brands.Model(brandID, model, extras)

	s.setupBrands(c)
	assertstatetest.AddMany(s.state, modelAs)
	return modelAs
}

func (s *deviceMgrBaseSuite) setPCModelInState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "serialserialserial",
	})
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkBootloaderHappy(c *C) {
	s.setPCModelInState(c)

	s.bootloader.SetBootVars(map[string]string{
		"snap_mode":     "trying",
		"snap_try_core": "core_1.snap",
	})

	s.state.Lock()
	defer s.state.Unlock()
	siCore1 := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{siCore1},
		Current:  siCore1.Revision,
	})

	s.state.Unlock()
	err := devicestate.EnsureBootOk(s.mgr)
	s.state.Lock()
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("snap_mode")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{"snap_mode": ""})
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkUpdateBootRevisionsHappy(c *C) {
	s.setPCModelInState(c)

	// simulate that we have a new core_2, tried to boot it but that failed
	s.bootloader.SetBootVars(map[string]string{
		"snap_mode":     "",
		"snap_kernel":   "kernel_1.snap",
		"snap_try_core": "core_2.snap",
		"snap_core":     "core_1.snap",
	})

	s.state.Lock()
	defer s.state.Unlock()
	siKernel1 := &snap.SideInfo{RealName: "kernel", Revision: snap.R(1)}
	snapstate.Set(s.state, "kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: []*snap.SideInfo{siKernel1},
		Current:  siKernel1.Revision,
	})

	siCore1 := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	siCore2 := &snap.SideInfo{RealName: "core", Revision: snap.R(2)}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: []*snap.SideInfo{siCore1, siCore2},
		Current:  siCore2.Revision,
	})

	s.state.Unlock()
	err := devicestate.EnsureBootOk(s.mgr)
	s.state.Lock()
	c.Assert(err, IsNil)

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(s.state.Changes()[0].Kind(), Equals, "update-revisions")
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkNotRunAgain(c *C) {
	s.setPCModelInState(c)

	s.bootloader.SetBootVars(map[string]string{
		"snap_mode":     "trying",
		"snap_try_core": "core_1.snap",
	})
	s.bootloader.SetErr = fmt.Errorf("ensure bootloader is not used")

	devicestate.SetBootOkRan(s.mgr, true)

	err := devicestate.EnsureBootOk(s.mgr)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkError(c *C) {
	s.setPCModelInState(c)

	s.state.Lock()
	// seeded
	s.state.Set("seeded", true)
	// has serial
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	s.state.Unlock()

	s.bootloader.GetErr = fmt.Errorf("bootloader err")

	devicestate.SetBootOkRan(s.mgr, false)

	err := s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "devicemgr: cannot mark boot successful: bootloader err")
}

func (s *deviceMgrBaseSuite) setupBrands(c *C) {
	assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
	otherAcct := assertstest.NewAccount(s.storeSigning, "other-brand", map[string]interface{}{
		"account-id": "other-brand",
	}, "")
	assertstatetest.AddMany(s.state, otherAcct)
}

func (s *deviceMgrSuite) setupSnapDecl(c *C, info *snap.Info, publisherID string) {
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    info.SnapName(),
		"snap-id":      info.SnapID,
		"publisher-id": publisherID,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	assertstatetest.AddMany(s.state, snapDecl)
}

func fakeMyModel(extra map[string]interface{}) *asserts.Model {
	model := map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
	}
	return assertstest.FakeAssertion(model, extra).(*asserts.Model)
}

func (s *deviceMgrSuite) TestCheckGadget(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: other-gadget, version: 0}", nil)

	s.setupBrands(c)
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	err := devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget "other-gadget", model assertion requests "gadget"`)

	// brand gadget
	brandGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	brandGadgetInfo.SnapID = "brand-gadget-id"
	s.setupSnapDecl(c, brandGadgetInfo, "my-brand")

	// canonical gadget
	canonicalGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	canonicalGadgetInfo.SnapID = "canonical-gadget-id"
	s.setupSnapDecl(c, canonicalGadgetInfo, "canonical")

	// other gadget
	otherGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	otherGadgetInfo.SnapID = "other-gadget-id"
	s.setupSnapDecl(c, otherGadgetInfo, "other-brand")

	// install brand gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install canonical gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install other gadget fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget "gadget" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherGadgetInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// parallel install fails
	otherGadgetInfo.InstanceKey = "foo"
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install "gadget_foo", parallel installation of kernel or gadget snaps is not supported`)
}

func (s *deviceMgrSuite) TestCheckGadgetOnClassic(c *C) {
	release.OnClassic = true

	s.state.Lock()
	defer s.state.Unlock()

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: other-gadget, version: 0}", nil)

	s.setupBrands(c)
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"classic": "true",
		"gadget":  "gadget",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	err := devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget "other-gadget", model assertion requests "gadget"`)

	// brand gadget
	brandGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	brandGadgetInfo.SnapID = "brand-gadget-id"
	s.setupSnapDecl(c, brandGadgetInfo, "my-brand")

	// canonical gadget
	canonicalGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	canonicalGadgetInfo.SnapID = "canonical-gadget-id"
	s.setupSnapDecl(c, canonicalGadgetInfo, "canonical")

	// other gadget
	otherGadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)
	otherGadgetInfo.SnapID = "other-gadget-id"
	s.setupSnapDecl(c, otherGadgetInfo, "other-brand")

	// install brand gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install canonical gadget ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install other gadget fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget "gadget" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherGadgetInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherGadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)
}

func (s *deviceMgrSuite) TestCheckGadgetOnClassicGadgetNotSpecified(c *C) {
	release.OnClassic = true

	s.state.Lock()
	defer s.state.Unlock()

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)

	s.setupBrands(c)
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"classic": "true",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	err := devicestate.CheckGadgetOrKernel(s.state, gadgetInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install gadget snap on classic if not requested by the model`)
}

func (s *deviceMgrSuite) TestCheckGadgetValid(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	gadgetInfo := snaptest.MockInfo(c, "{type: gadget, name: gadget, version: 0}", nil)

	// valid gadget.yaml
	cont := snaptest.MockContainer(c, [][]string{
		{"meta/gadget.yaml", gadgetYaml},
	})
	err := devicestate.CheckGadgetValid(s.state, gadgetInfo, nil, cont, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// invalid gadget.yaml
	cont = snaptest.MockContainer(c, [][]string{
		{"meta/gadget.yaml", `defaults:`},
	})
	err = devicestate.CheckGadgetValid(s.state, gadgetInfo, nil, cont, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `bootloader not declared in any volume`)

}

func (s *deviceMgrSuite) TestCheckKernel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	kernelInfo := snaptest.MockInfo(c, "{type: kernel, name: lnrk, version: 0}", nil)

	// not on classic
	release.OnClassic = true
	err := devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, nil, snapstate.Flags{}, nil)
	c.Check(err, ErrorMatches, `cannot install a kernel snap on classic`)
	release.OnClassic = false

	s.setupBrands(c)
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}

	err = devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install kernel "lnrk", model assertion requests "krnl"`)

	// brand kernel
	brandKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	brandKrnlInfo.SnapID = "brand-krnl-id"
	s.setupSnapDecl(c, brandKrnlInfo, "my-brand")

	// canonical kernel
	canonicalKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	canonicalKrnlInfo.SnapID = "canonical-krnl-id"
	s.setupSnapDecl(c, canonicalKrnlInfo, "canonical")

	// other kernel
	otherKrnlInfo := snaptest.MockInfo(c, "{type: kernel, name: krnl, version: 0}", nil)
	otherKrnlInfo.SnapID = "other-krnl-id"
	s.setupSnapDecl(c, otherKrnlInfo, "other-brand")

	// install brand kernel ok
	err = devicestate.CheckGadgetOrKernel(s.state, brandKrnlInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install canonical kernel ok
	err = devicestate.CheckGadgetOrKernel(s.state, canonicalKrnlInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// install other kernel fails
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install kernel "krnl" published by "other-brand" for model by "my-brand"`)

	// unasserted installation of other works
	otherKrnlInfo.SnapID = ""
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)

	// parallel install fails
	otherKrnlInfo.InstanceKey = "foo"
	err = devicestate.CheckGadgetOrKernel(s.state, otherKrnlInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install "krnl_foo", parallel installation of kernel or gadget snaps is not supported`)
}

func makeSerialAssertionInState(c *C, brands *assertstest.SigningAccounts, st *state.State, brandID, model, serialN string) *asserts.Serial {
	encDevKey, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)
	serial, err := brands.Signing(brandID).Sign(asserts.SerialType, map[string]interface{}{
		"brand-id":            brandID,
		"model":               model,
		"serial":              serialN,
		"device-key":          string(encDevKey),
		"device-key-sha3-384": devKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(st, serial)
	c.Assert(err, IsNil)
	return serial.(*asserts.Serial)
}

func (s *deviceMgrBaseSuite) makeSerialAssertionInState(c *C, brandID, model, serialN string) *asserts.Serial {
	return makeSerialAssertionInState(c, s.brands, s.state, brandID, model, serialN)
}

func (s *deviceMgrSuite) TestCanAutoRefreshOnCore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	canAutoRefresh := func() bool {
		ok, err := devicestate.CanAutoRefresh(s.state)
		c.Assert(err, IsNil)
		return ok
	}

	// not seeded, no model, no serial -> no auto-refresh
	s.state.Set("seeded", false)
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, no serial -> no auto-refresh
	s.state.Set("seeded", true)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, serial -> auto-refresh
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")
	c.Check(canAutoRefresh(), Equals, true)

	// not seeded, model, serial -> no auto-refresh
	s.state.Set("seeded", false)
	c.Check(canAutoRefresh(), Equals, false)
}

func (s *deviceMgrSuite) TestCanAutoRefreshNoSerialFallback(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	canAutoRefresh := func() bool {
		ok, err := devicestate.CanAutoRefresh(s.state)
		c.Assert(err, IsNil)
		return ok
	}

	// seeded, model, no serial, two attempts at getting serial
	// -> no auto-refresh
	devicestate.IncEnsureOperationalAttempts(s.state)
	devicestate.IncEnsureOperationalAttempts(s.state)
	s.state.Set("seeded", true)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	c.Check(canAutoRefresh(), Equals, false)

	// third attempt ongoing, or done
	// fallback, try auto-refresh
	devicestate.IncEnsureOperationalAttempts(s.state)
	// sanity
	c.Check(devicestate.EnsureOperationalAttempts(s.state), Equals, 3)
	c.Check(canAutoRefresh(), Equals, true)
}

func (s *deviceMgrSuite) TestCanAutoRefreshOnClassic(c *C) {
	release.OnClassic = true

	s.state.Lock()
	defer s.state.Unlock()

	canAutoRefresh := func() bool {
		ok, err := devicestate.CanAutoRefresh(s.state)
		c.Assert(err, IsNil)
		return ok
	}

	// not seeded, no model, no serial -> no auto-refresh
	s.state.Set("seeded", false)
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, no model -> auto-refresh
	s.state.Set("seeded", true)
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, no serial -> no auto-refresh
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"classic": "true",
	})
	c.Check(canAutoRefresh(), Equals, false)

	// seeded, model, serial -> auto-refresh
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")
	c.Check(canAutoRefresh(), Equals, true)

	// not seeded, model, serial -> no auto-refresh
	s.state.Set("seeded", false)
	c.Check(canAutoRefresh(), Equals, false)
}

func makeInstalledMockCoreSnapWithSnapdControl(c *C, st *state.State) *snap.Info {
	sideInfoCore11 := &snap.SideInfo{RealName: "core", Revision: snap.R(11), SnapID: "core-id"}
	snapstate.Set(st, "core", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfoCore11},
		Current:  sideInfoCore11.Revision,
		SnapType: "os",
	})
	core11 := snaptest.MockSnap(c, `
name: core
version: 1.0
slots:
 snapd-control:
`, sideInfoCore11)
	c.Assert(core11.Slots, HasLen, 1)

	return core11
}

var snapWithSnapdControlRefreshScheduleManagedYAML = `
name: snap-with-snapd-control
version: 1.0
plugs:
 snapd-control:
  refresh-schedule: managed
`

var snapWithSnapdControlOnlyYAML = `
name: snap-with-snapd-control
version: 1.0
plugs:
 snapd-control:
`

func makeInstalledMockSnap(c *C, st *state.State, yml string) *snap.Info {
	sideInfo11 := &snap.SideInfo{RealName: "snap-with-snapd-control", Revision: snap.R(11), SnapID: "snap-with-snapd-control-id"}
	snapstate.Set(st, "snap-with-snapd-control", &snapstate.SnapState{
		Active:   true,
		Sequence: []*snap.SideInfo{sideInfo11},
		Current:  sideInfo11.Revision,
		SnapType: "app",
	})
	info11 := snaptest.MockSnap(c, yml, sideInfo11)
	c.Assert(info11.Plugs, HasLen, 1)

	return info11
}

func makeMockRepoWithConnectedSnaps(c *C, st *state.State, info11, core11 *snap.Info, ifname string) {
	repo := interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		err := repo.AddInterface(iface)
		c.Assert(err, IsNil)
	}
	err := repo.AddSnap(info11)
	c.Assert(err, IsNil)
	err = repo.AddSnap(core11)
	c.Assert(err, IsNil)
	_, err = repo.Connect(&interfaces.ConnRef{
		PlugRef: interfaces.PlugRef{Snap: info11.InstanceName(), Name: ifname},
		SlotRef: interfaces.SlotRef{Snap: core11.InstanceName(), Name: ifname},
	}, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	conns, err := repo.Connected("snap-with-snapd-control", "snapd-control")
	c.Assert(err, IsNil)
	c.Assert(conns, HasLen, 1)
	ifacerepo.Replace(st, repo)
}

func (s *deviceMgrSuite) TestCanManageRefreshes(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// not possbile to manage by default
	c.Check(devicestate.CanManageRefreshes(st), Equals, false)

	// not possible with just a snap with "snapd-control" plug with the
	// right attribute
	info11 := makeInstalledMockSnap(c, st, snapWithSnapdControlRefreshScheduleManagedYAML)
	c.Check(devicestate.CanManageRefreshes(st), Equals, false)

	// not possible with a core snap with snapd control
	core11 := makeInstalledMockCoreSnapWithSnapdControl(c, st)
	c.Check(devicestate.CanManageRefreshes(st), Equals, false)

	// not possible even with connected interfaces
	makeMockRepoWithConnectedSnaps(c, st, info11, core11, "snapd-control")
	c.Check(devicestate.CanManageRefreshes(st), Equals, false)

	// if all of the above plus a snap declaration are in place we can
	// manage schedules
	s.setupSnapDecl(c, info11, "canonical")
	c.Check(devicestate.CanManageRefreshes(st), Equals, true)

	// works if the snap is not active as well (to fix race when a
	// snap is refreshed)
	var sideInfo11 snapstate.SnapState
	err := snapstate.Get(st, "snap-with-snapd-control", &sideInfo11)
	c.Assert(err, IsNil)
	sideInfo11.Active = false
	snapstate.Set(st, "snap-with-snapd-control", &sideInfo11)
	c.Check(devicestate.CanManageRefreshes(st), Equals, true)
}

func (s *deviceMgrSuite) TestCanManageRefreshesNoRefreshScheduleManaged(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// just having a connected "snapd-control" interface is not enough
	// for setting refresh.schedule=managed
	info11 := makeInstalledMockSnap(c, st, snapWithSnapdControlOnlyYAML)
	core11 := makeInstalledMockCoreSnapWithSnapdControl(c, st)
	makeMockRepoWithConnectedSnaps(c, st, info11, core11, "snapd-control")
	s.setupSnapDecl(c, info11, "canonical")

	c.Check(devicestate.CanManageRefreshes(st), Equals, false)
}

func (s *deviceMgrSuite) TestReloadRegistered(c *C) {
	st := state.New(nil)

	runner1 := state.NewTaskRunner(st)
	hookMgr1, err := hookstate.Manager(st, runner1)
	c.Assert(err, IsNil)
	mgr1, err := devicestate.Manager(st, hookMgr1, runner1, nil)
	c.Assert(err, IsNil)

	ok := false
	select {
	case <-mgr1.Registered():
	default:
		ok = true
	}
	c.Check(ok, Equals, true)

	st.Lock()
	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "serial",
	})
	st.Unlock()

	runner2 := state.NewTaskRunner(st)
	hookMgr2, err := hookstate.Manager(st, runner2)
	c.Assert(err, IsNil)
	mgr2, err := devicestate.Manager(st, hookMgr2, runner2, nil)
	c.Assert(err, IsNil)

	ok = false
	select {
	case <-mgr2.Registered():
		ok = true
	case <-time.After(5 * time.Second):
		c.Fatal("should have been marked registered")
	}
	c.Check(ok, Equals, true)
}

func (s *deviceMgrSuite) TestMarkSeededInConfig(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// avoid device registration
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Serial: "123",
	})

	// avoid full seeding
	s.seeding()

	// not seeded -> no config is set
	s.state.Unlock()
	s.mgr.Ensure()
	s.state.Lock()

	var seedLoaded bool
	tr := config.NewTransaction(st)
	tr.Get("core", "seed.loaded", &seedLoaded)
	c.Check(seedLoaded, Equals, false)

	// pretend we are seeded now
	s.state.Set("seeded", true)

	// seeded -> config got updated
	s.state.Unlock()
	s.mgr.Ensure()
	s.state.Lock()

	tr = config.NewTransaction(st)
	tr.Get("core", "seed.loaded", &seedLoaded)
	c.Check(seedLoaded, Equals, true)

	// only the fake seeding change is in the state, no further
	// changes
	c.Check(s.state.Changes(), HasLen, 1)
}

func (s *deviceMgrSuite) TestDevicemgrCanStandby(c *C) {
	st := state.New(nil)

	runner := state.NewTaskRunner(st)
	hookMgr, err := hookstate.Manager(st, runner)
	c.Assert(err, IsNil)
	mgr, err := devicestate.Manager(st, hookMgr, runner, nil)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()
	c.Check(mgr.CanStandby(), Equals, false)

	st.Set("seeded", true)
	c.Check(mgr.CanStandby(), Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerReadsModeenv(c *C) {
	modeEnv := &boot.Modeenv{Mode: "install"}
	err := modeEnv.Write("")
	c.Assert(err, IsNil)

	runner := s.o.TaskRunner()
	mgr, err := devicestate.Manager(s.state, s.hookMgr, runner, s.newStore)
	c.Assert(err, IsNil)
	c.Assert(mgr, NotNil)
	c.Assert(mgr.OperatingMode(), Equals, "install")
}

func (s *deviceMgrSuite) TestDeviceManagerEmptyOperatingModeRun(c *C) {
	// set empty operating mode
	devicestate.SetOperatingMode(s.mgr, "")

	// empty is returned as "run"
	c.Check(s.mgr.OperatingMode(), Equals, "run")
}

func (s *deviceMgrSuite) TestStartOfOperationTimeFromSeedTime(c *C) {
	st := state.New(nil)
	// can't use device manager from the suite, we need to avoid o.StartUp()
	mgr, err := devicestate.Manager(st, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	seedTime := time.Now().AddDate(0, -1, 0)
	st.Set("seed-time", seedTime)

	operationTime, err := mgr.StartOfOperationTime()
	c.Assert(err, IsNil)
	c.Check(operationTime.Equal(seedTime), Equals, true)

	var op time.Time
	st.Get("start-of-operation-time", &op)
	c.Check(op.Equal(operationTime), Equals, true)
}

func (s *deviceMgrSuite) TestStartOfOperationTimeAlreadySet(c *C) {
	st := state.New(nil)
	// can't use device manager from the suite, we need to avoid o.StartUp()
	mgr, err := devicestate.Manager(st, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	op := time.Now().AddDate(0, -1, 0)
	st.Set("start-of-operation-time", op)

	operationTime, err := mgr.StartOfOperationTime()
	c.Assert(err, IsNil)
	c.Check(operationTime.Equal(op), Equals, true)
}

func (s *deviceMgrSuite) TestStartOfOperationTimeNoSeedTime(c *C) {
	st := state.New(nil)
	// can't use device manager from the suite, we need to avoid o.StartUp()
	mgr, err := devicestate.Manager(st, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	now := time.Now().Add(-1 * time.Second)
	devicestate.MockTimeNow(func() time.Time {
		return now
	})

	operationTime, err := mgr.StartOfOperationTime()
	c.Assert(err, IsNil)
	c.Check(operationTime.Equal(now), Equals, true)

	// repeated call returns already set time
	prev := now
	now = time.Now().Add(-10 * time.Hour)
	operationTime, err = mgr.StartOfOperationTime()
	c.Assert(err, IsNil)
	c.Check(operationTime.Equal(prev), Equals, true)
}

func (s *deviceMgrSuite) TestStartOfOperationErrorIfPreseed(c *C) {
	restore := release.MockPreseedMode(func() bool { return true })
	defer restore()

	st := s.state

	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()
	_, err = mgr.StartOfOperationTime()
	c.Assert(err, ErrorMatches, `internal error: unexpected call to StartOfOperationTime in preseed mode`)
}
