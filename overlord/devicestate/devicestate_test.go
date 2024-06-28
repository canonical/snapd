// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"os"
	"path/filepath"
	"strings"
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
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
	"github.com/snapcore/snapd/timings"
)

var (
	settleTimeout = testutil.HostScaledTimeout(30 * time.Second)
)

func TestDeviceManager(t *testing.T) { TestingT(t) }

type deviceMgrBaseSuite struct {
	testutil.BaseTest

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

	restartRequests []restart.RestartType
	restartObserve  func()

	newFakeStore func(storecontext.DeviceBackend) snapstate.StoreService

	// saved so that if a derived suite wants to undo the cloud-init mocking to
	// test the actual functions, it can just call this in it's SetUpTest, see
	// devicestate_cloudinit_test.go for details
	restoreCloudInitStatusRestore func()

	restoreProcessAutoImportAssertion func()
}

// mockRestartAndSettle expects the state to be locked
func (s *deviceMgrBaseSuite) mockRestartAndSettle(c *C, st *state.State, chg *state.Change) {
	restart.MockPending(st, restart.RestartUnset)
	restart.MockAfterRestartForChange(chg)

	st.Unlock()
	defer st.Lock()
	err := s.o.Settle(settleTimeout)
	c.Check(err, IsNil)
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

func (sto *fakeStore) SeqFormingAssertion(assertType *asserts.AssertionType, key []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	sto.pokeStateLock()

	ref := &asserts.AtSequence{
		Type:        assertType,
		SequenceKey: key,
		Sequence:    sequence,
		Revision:    asserts.RevisionNotKnown,
		Pinned:      sequence > 0,
	}

	if sequence <= 0 {
		hdrs, err := asserts.HeadersFromSequenceKey(ref.Type, ref.SequenceKey)
		if err != nil {
			return nil, err
		}
		return sto.db.FindSequence(ref.Type, hdrs, -1, ref.Type.MaxSupportedFormat())
	}

	return ref.Resolve(sto.db.Find)
}

var (
	brandPrivKey, _  = assertstest.GenerateKey(752)
	brandPrivKey2, _ = assertstest.GenerateKey(752)
	brandPrivKey3, _ = assertstest.GenerateKey(752)
)

func (s *deviceMgrBaseSuite) setupBaseTest(c *C, classic bool) {
	s.BaseTest.SetUpTest(c)

	s.AddCleanup(release.MockOnClassic(classic))

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	err := os.MkdirAll(dirs.SnapRunDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(dirs.SnapdStateDir(dirs.GlobalRootDir), 0755)
	c.Assert(err, IsNil)

	s.AddCleanup(osutil.MockMountInfo(``))

	s.restartRequests = nil

	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.bootloader = bootloadertest.Mock("mock", c.MkDir())
	bootloader.Force(s.bootloader)
	s.AddCleanup(func() { bootloader.Force(nil) })

	s.storeSigning = assertstest.NewStoreStack("canonical", nil)
	s.restartObserve = nil
	s.o = overlord.Mock()
	s.state = s.o.State()
	s.state.Lock()
	_, err = restart.Manager(s.state, "boot-id-0", snapstatetest.MockRestartHandler(func(req restart.RestartType) {
		s.restartRequests = append(s.restartRequests, req)
		if s.restartObserve != nil {
			s.restartObserve()
		}
	}))
	s.state.Unlock()
	c.Assert(err, IsNil)
	s.se = s.o.StateEngine()

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

	s.restoreCloudInitStatusRestore = devicestate.MockCloudInitStatus(func() (sysconfig.CloudInitState, error) {
		return sysconfig.CloudInitRestrictedBySnapd, nil
	})
	s.AddCleanup(s.restoreCloudInitStatusRestore)

	s.AddCleanup(func() { s.ancillary = nil })
	s.AddCleanup(func() { s.newFakeStore = nil })

	s.AddCleanup(devicestate.MockTimeutilIsNTPSynchronized(func() (bool, error) {
		return true, nil
	}))
	s.AddCleanup(devicestate.MockSecbootMarkSuccessful(func() error {
		return nil
	}))

	s.restoreProcessAutoImportAssertion = devicestate.MockProcessAutoImportAssertion(func(*state.State, seed.Seed, asserts.RODatabase, func(batch *asserts.Batch) error) error {
		return nil
	})
	s.AddCleanup(s.restoreProcessAutoImportAssertion)
}

func (s *deviceMgrBaseSuite) newStore(devBE storecontext.DeviceBackend) snapstate.StoreService {
	return s.newFakeStore(devBE)
}

func (s *deviceMgrBaseSuite) settle(c *C) {
	err := s.o.Settle(settleTimeout)
	c.Assert(err, IsNil)
}

// seeding avoids triggering a real full seeding, it simulates having it in process instead
func (s *deviceMgrBaseSuite) seeding() {
	chg := s.state.NewChange("seed", "Seed system")
	chg.SetStatus(state.DoingStatus)
	s.state.Set("seeded", false)
}

func (s *deviceMgrBaseSuite) makeModelAssertionInState(c *C, brandID, model string, extras map[string]interface{}) *asserts.Model {
	modelAs := s.brands.Model(brandID, model, extras)

	s.setupBrands()
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

func (s *deviceMgrBaseSuite) setUC20PCModelInState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.makeModelAssertionInState(c, "canonical", "pc-20", map[string]interface{}{
		"architecture": "amd64",
		// UC20
		"grade": "dangerous",
		"base":  "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-20",
		Serial: "serialserialserial",
	})
}

func (s *deviceMgrBaseSuite) setupBrands() {
	assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
	otherAcct := assertstest.NewAccount(s.storeSigning, "other-brand", map[string]interface{}{
		"account-id": "other-brand",
	}, "")
	assertstatetest.AddMany(s.state, otherAcct)
}

func (s *deviceMgrBaseSuite) setupSnapDeclForNameAndID(c *C, name, snapID, publisherID string) {
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-name":    name,
		"snap-id":      snapID,
		"publisher-id": publisherID,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	assertstatetest.AddMany(s.state, snapDecl)
}

func (s *deviceMgrBaseSuite) setupSnapDecl(c *C, info *snap.Info, publisherID string) {
	s.setupSnapDeclForNameAndID(c, info.SnapName(), info.SnapID, publisherID)
}

func (s *deviceMgrBaseSuite) setupSnapRevisionForFileAndID(c *C, file, snapID, publisherID string, revision snap.Revision) {
	sha3_384, size, err := asserts.SnapFileSHA3_384(file)
	c.Assert(err, IsNil)

	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": sha3_384,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-id":       snapID,
		"developer-id":  publisherID,
		"snap-revision": revision.String(),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	assertstatetest.AddMany(s.state, snapRev)
}

func (s *deviceMgrBaseSuite) setupSnapRevision(c *C, info *snap.Info, publisherID string, revision snap.Revision) {
	s.setupSnapRevisionForFileAndID(c, info.MountFile(), info.SnapID, publisherID, revision)
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

func (s *deviceMgrSuite) SetUpTest(c *C) {
	classic := false
	s.setupBaseTest(c, classic)
}

func (s *deviceMgrSuite) TestDeviceManagerSetTimeOnce(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// set first time
	now := time.Now()
	err := devicestate.SetTimeOnce(s.mgr, "key-name", now)
	c.Assert(err, IsNil)

	later := now.Add(1 * time.Minute)
	// setting again doesn't change value
	err = devicestate.SetTimeOnce(s.mgr, "key-name", later)
	c.Assert(err, IsNil)

	var t time.Time
	s.state.Get("key-name", &t)

	c.Assert(t.Equal(now), Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeededAlreadySeeded(c *C) {
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	called := false
	restore := devicestate.MockPopulateStateFromSeed(s.mgr, func(string, string, timings.Measurer) ([]*state.TaskSet, error) {
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
	restore := devicestate.MockPopulateStateFromSeed(s.mgr, func(string, string, timings.Measurer) ([]*state.TaskSet, error) {
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
	restore := devicestate.MockPopulateStateFromSeed(s.mgr, func(sLabel, sMode string, tm timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		c.Check(sMode, Equals, "")
		c.Check(sLabel, Equals, "")
		return nil, nil
	})
	defer restore()

	err := devicestate.EnsureSeeded(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(called, Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeededHappy(c *C) {
	restore := devicestate.MockPopulateStateFromSeed(s.mgr, func(sLabel, sMode string, tm timings.Measurer) (ts []*state.TaskSet, err error) {
		c.Assert(sLabel, Equals, "")
		c.Assert(sMode, Equals, "")
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

	var seedStartTime time.Time
	c.Assert(s.state.Get("seed-start-time", &seedStartTime), IsNil)
	c.Check(seedStartTime.Equal(devicestate.StartTime()), Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkSkippedOnClassic(c *C) {
	s.bootloader.GetErr = fmt.Errorf("should not be called")
	release.OnClassic = true

	err := devicestate.EnsureBootOk(s.mgr)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkSkippedOnNonRunModes(c *C) {
	s.bootloader.GetErr = fmt.Errorf("should not be called")
	devicestate.SetSystemMode(s.mgr, "install")

	err := devicestate.EnsureBootOk(s.mgr)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) switchDevManagerToClassicWithModes(c *C) {
	release.OnClassic = true

	// mock the modeenv file
	m := boot.Modeenv{
		Mode: "run",
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// re-create manager so that modeenv file is-read
	s.mgr, err = devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkRunsOnClassicWithModes(c *C) {
	s.switchDevManagerToClassicWithModes(c)
	s.setPCModelInState(c)

	secbootMarkSuccessfulCalled := 0
	r := devicestate.MockSecbootMarkSuccessful(func() error {
		secbootMarkSuccessfulCalled++
		return nil
	})
	defer r()

	err := devicestate.EnsureBootOk(s.mgr)
	c.Assert(err, IsNil)
	c.Check(secbootMarkSuccessfulCalled, Equals, 1)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureSeededHappyWithModeenv(c *C) {
	n := 0

	// mock the modeenv file
	m := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20191127",
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// re-create manager so that modeenv file is-read
	s.mgr, err = devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)
	restore := devicestate.MockPopulateStateFromSeed(s.mgr, func(sLabel, sMode string, tm timings.Measurer) (ts []*state.TaskSet, err error) {
		c.Check(sLabel, Equals, "20191127")
		c.Check(sMode, Equals, "install")

		t := s.state.NewTask("test-task", "a random task")
		ts = append(ts, state.NewTaskSet(t))

		n++
		return ts, nil
	})
	defer restore()

	err = devicestate.EnsureSeeded(s.mgr)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	c.Check(s.state.Changes(), HasLen, 1)
	c.Check(n, Equals, 1)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsureBootOkBootloaderHappy(c *C) {
	s.setPCModelInState(c)

	secbootMarkSuccessfulCalled := 0
	r := devicestate.MockSecbootMarkSuccessful(func() error {
		secbootMarkSuccessfulCalled++
		return nil
	})
	defer r()

	s.bootloader.SetBootVars(map[string]string{
		"snap_mode":     boot.TryingStatus,
		"snap_try_core": "core_1.snap",
	})

	s.state.Lock()
	defer s.state.Unlock()
	siCore1 := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siCore1}),
		Current:  siCore1.Revision,
	})

	s.state.Unlock()
	err := devicestate.EnsureBootOk(s.mgr)
	s.state.Lock()
	c.Assert(err, IsNil)
	c.Check(secbootMarkSuccessfulCalled, Equals, 1)

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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siKernel1}),
		Current:  siKernel1.Revision,
	})

	siCore1 := &snap.SideInfo{RealName: "core", Revision: snap.R(1)}
	siCore2 := &snap.SideInfo{RealName: "core", Revision: snap.R(2)}
	snapstate.Set(s.state, "core", &snapstate.SnapState{
		SnapType: "os",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{siCore1, siCore2}),
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
		"snap_mode":     boot.TryingStatus,
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

	s.setupBrands()
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

	s.setupBrands()
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

	s.setupBrands()
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

	// not on classic without modes
	release.OnClassic = true
	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"classic": "true",
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}
	err := devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, ErrorMatches, `cannot install a kernel snap if classic boot`)
	release.OnClassic = false

	s.setupBrands()
	// model assertion in device context
	model = fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})
	deviceCtx = &snapstatetest.TrivialDeviceContext{DeviceModel: model}

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

func (s *deviceMgrSuite) TestCheckKernelOnClassicWithModes(c *C) {
	s.switchDevManagerToClassicWithModes(c)

	s.state.Lock()
	defer s.state.Unlock()
	kernelInfo := snaptest.MockInfo(c, "{type: kernel, name: pc-kernel, version: 0}", nil)

	// model assertion in device context
	model := fakeMyModel(map[string]interface{}{
		"architecture": "amd64",
		"classic":      "true",
		"grade":        "dangerous",
		"distribution": "ubuntu",
		"base":         "core22",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "22",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "22",
			},
		},
	})
	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: model}
	err := devicestate.CheckGadgetOrKernel(s.state, kernelInfo, nil, nil, snapstate.Flags{}, deviceCtx)
	c.Check(err, IsNil)
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
	// validity
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfoCore11}),
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
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo11}),
		Current:  sideInfo11.Revision,
		SnapType: "app",
	})
	info11 := snaptest.MockSnap(c, yml, sideInfo11)
	c.Assert(info11.Plugs, HasLen, 1)

	return info11
}

func makeInstalledMockKernelSnap(c *C, st *state.State, yml string) *snap.Info {
	sideInfo11 := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(11), SnapID: "pc-kernel-id"}
	snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo11}),
		Current:  sideInfo11.Revision,
		SnapType: "kernel",
	})
	info11 := snaptest.MockSnap(c, yml, sideInfo11)

	return info11
}

func makeMockRepoWithConnectedSnaps(c *C, st *state.State, info11, core11 *snap.Info, ifname string) {
	repo := interfaces.NewRepository()
	for _, iface := range builtin.Interfaces() {
		err := repo.AddInterface(iface)
		c.Assert(err, IsNil)
	}

	info11AppSet, err := interfaces.NewSnapAppSet(info11, nil)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(info11AppSet)
	c.Assert(err, IsNil)

	core11AppSet, err := interfaces.NewSnapAppSet(core11, nil)
	c.Assert(err, IsNil)

	err = repo.AddAppSet(core11AppSet)
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

func (s *deviceMgrSuite) TestResetSession(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	c.Assert(devicestate.ResetSession(st), IsNil)

	devicestatetest.SetDevice(st, &auth.DeviceState{
		SessionMacaroon: "session",
	})
	c.Assert(devicestate.ResetSession(st), IsNil)

	device, err := devicestatetest.Device(st)
	c.Assert(err, IsNil)
	c.Check(device.SessionMacaroon, Equals, "")
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
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)

	runner := s.o.TaskRunner()
	mgr, err := devicestate.Manager(s.state, s.hookMgr, runner, s.newStore)
	c.Assert(err, IsNil)
	c.Assert(mgr, NotNil)
	c.Assert(mgr.SystemMode(devicestate.SysAny), Equals, "install")
	c.Assert(mgr.SystemMode(devicestate.SysHasModeenv), Equals, "install")
}

func (s *deviceMgrSuite) TestDeviceManagerEmptySystemModeRun(c *C) {
	// set empty system mode
	devicestate.SetSystemMode(s.mgr, "")

	// empty is returned as "run" for SysAny
	c.Check(s.mgr.SystemMode(devicestate.SysAny), Equals, "run")
	// empty is returned as itself for SysHasModeenv
	c.Check(s.mgr.SystemMode(devicestate.SysHasModeenv), Equals, "")
}

func (s *deviceMgrSuite) TestDeviceManagerSystemModeInfoTooEarly(c *C) {
	runner := s.o.TaskRunner()
	mgr, err := devicestate.Manager(s.state, s.hookMgr, runner, s.newStore)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	_, err = mgr.SystemModeInfo()
	c.Check(err, ErrorMatches, `cannot report system mode information before device model is acknowledged`)
}

func (s *deviceMgrSuite) TestDeviceManagerSystemModeInfoUC18(c *C) {
	runner := s.o.TaskRunner()
	mgr, err := devicestate.Manager(s.state, s.hookMgr, runner, s.newStore)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// have a model
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base:":        "core18",
	})

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	smi, err := mgr.SystemModeInfo()
	c.Assert(err, IsNil)
	c.Check(smi, DeepEquals, &devicestate.SystemModeInfo{
		Mode:   "run",
		Seeded: false,
	})

	// seeded
	s.state.Set("seeded", true)

	smi, err = mgr.SystemModeInfo()
	c.Assert(err, IsNil)
	c.Check(smi, DeepEquals, &devicestate.SystemModeInfo{
		Mode:   "run",
		Seeded: true,
	})
}

func (s *deviceMgrSuite) TestDeviceManagerSystemModeInfoUC20Install(c *C) {
	modeEnv := &boot.Modeenv{Mode: "install"}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)

	runner := s.o.TaskRunner()
	mgr, err := devicestate.Manager(s.state, s.hookMgr, runner, s.newStore)
	c.Assert(err, IsNil)

	s.setUC20PCModelInState(c)

	s.state.Lock()
	defer s.state.Unlock()

	// seeded
	s.state.Set("seeded", true)
	// no flags
	c.Assert(boot.InitramfsExposeBootFlagsForSystem(nil), IsNil)
	// data present
	ubuntuData := filepath.Dir(filepath.Join(dirs.GlobalRootDir, "/run/mnt/ubuntu-data/system-data"))
	c.Assert(os.MkdirAll(ubuntuData, 0755), IsNil)

	smi, err := mgr.SystemModeInfo()
	c.Assert(err, IsNil)
	c.Check(smi, DeepEquals, &devicestate.SystemModeInfo{
		Mode:              "install",
		HasModeenv:        true,
		Seeded:            true,
		BootFlags:         []string{},
		HostDataLocations: []string{ubuntuData},
	})

	// factory
	c.Assert(boot.InitramfsExposeBootFlagsForSystem([]string{"factory"}), IsNil)
	smi, err = mgr.SystemModeInfo()
	c.Assert(err, IsNil)
	c.Check(smi, DeepEquals, &devicestate.SystemModeInfo{
		Mode:              "install",
		HasModeenv:        true,
		Seeded:            true,
		BootFlags:         []string{"factory"},
		HostDataLocations: []string{ubuntuData},
	})
}

func (s *deviceMgrSuite) TestDeviceManagerSystemModeInfoUC20Run(c *C) {
	modeEnv := &boot.Modeenv{Mode: "run"}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)

	runner := s.o.TaskRunner()
	mgr, err := devicestate.Manager(s.state, s.hookMgr, runner, s.newStore)
	c.Assert(err, IsNil)

	s.setUC20PCModelInState(c)

	s.state.Lock()
	defer s.state.Unlock()

	// not seeded
	// no flags
	c.Assert(boot.InitramfsExposeBootFlagsForSystem(nil), IsNil)

	smi, err := mgr.SystemModeInfo()
	c.Assert(err, IsNil)
	c.Check(smi, DeepEquals, &devicestate.SystemModeInfo{
		Mode:              "run",
		HasModeenv:        true,
		Seeded:            false,
		BootFlags:         []string{},
		HostDataLocations: []string{boot.InitramfsDataDir, dirs.GlobalRootDir},
	})

	// given state only
	smi, err = devicestate.SystemModeInfoFromState(s.state)
	c.Assert(err, IsNil)
	c.Check(smi, DeepEquals, &devicestate.SystemModeInfo{
		Mode:              "run",
		HasModeenv:        true,
		Seeded:            false,
		BootFlags:         []string{},
		HostDataLocations: []string{boot.InitramfsDataDir, dirs.GlobalRootDir},
	})
}

const (
	mountRunMntUbuntuSaveFmt = `26 27 8:3 / %s/run/mnt/ubuntu-save rw,relatime shared:7 - ext4 /dev/fakedevice0p1 rw,data=ordered`
	mountSnapSaveFmt         = `26 27 8:3 / %s/var/lib/snapd/save rw,relatime shared:7 - ext4 /dev/fakedevice0p1 rw,data=ordered`
)

func (s *deviceMgrSuite) TestDeviceManagerStartupUC20UbuntuSaveRunModeHappy(c *C) {
	modeEnv := &boot.Modeenv{Mode: "run"}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)
	s.setUC20PCModelInState(c)

	// create a new manager so that the modeenv we mocked in read
	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	// make this one fail so we test both are invoked
	sysctlCmd := testutil.MockCommand(c, "systemctl", "echo 'Failed to start var-lib-snapd-save.mount: Unit var-lib-snapd-save.mount not found.'; exit 1")
	defer sysctlCmd.Restore()

	mountCmd := testutil.MockCommand(c, "systemd-mount", "")
	defer mountCmd.Restore()

	// ubuntu-save not mounted
	err = mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(sysctlCmd.Calls(), HasLen, 0)
	c.Check(mountCmd.Calls(), HasLen, 0)

	restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir))
	defer restore()

	err = mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(sysctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "start", "var-lib-snapd-save.mount"},
	})
	c.Check(mountCmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", "-o", "bind", boot.InitramfsUbuntuSaveDir, dirs.SnapSaveDir},
	})

	// known as available
	c.Check(devicestate.SaveAvailable(mgr), Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerStartupUC20UbuntuSaveSystemCtlFails(c *C) {
	modeEnv := &boot.Modeenv{Mode: "run"}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)
	s.setUC20PCModelInState(c)

	// create a new manager so that the modeenv we mocked in read
	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	// this one now fails without a known reason, systemd-mount should not
	// be invoked, and we should receive an error
	sysctlCmd := testutil.MockCommand(c, "systemctl", "echo failed; exit 1")
	defer sysctlCmd.Restore()

	mountCmd := testutil.MockCommand(c, "systemd-mount", "")
	defer mountCmd.Restore()

	restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir))
	defer restore()

	err = mgr.StartUp()
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "cannot set up ubuntu-save: systemctl command [start var-lib-snapd-save.mount] failed with exit status 1: failed")
	c.Check(sysctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "start", "var-lib-snapd-save.mount"},
	})
	c.Check(mountCmd.Calls(), HasLen, 0)

	// since the systemctl fails, and it was not due to the missing mount
	// unit, then we should get an error, and ubuntu save should not bge available
	c.Check(devicestate.SaveAvailable(mgr), Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerStartupUC20UbuntuSaveMountUnitExists(c *C) {
	modeEnv := &boot.Modeenv{Mode: "run"}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)
	s.setUC20PCModelInState(c)

	// create a new manager so that the modeenv we mocked in read
	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	// this one now succeeds, which means systemd-mount should not
	// be invoked
	sysctlCmd := testutil.MockCommand(c, "systemctl", "")
	defer sysctlCmd.Restore()

	mountCmd := testutil.MockCommand(c, "systemd-mount", "")
	defer mountCmd.Restore()

	restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir))
	defer restore()

	err = mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(sysctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "start", "var-lib-snapd-save.mount"},
	})
	c.Check(mountCmd.Calls(), HasLen, 0)

	// known as available
	c.Check(devicestate.SaveAvailable(mgr), Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerStartupUC20UbuntuSaveAlreadyMounted(c *C) {
	modeEnv := &boot.Modeenv{Mode: "run"}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)
	s.setUC20PCModelInState(c)

	// create a new manager so that the modeenv we mocked in read
	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	sysctlCmd := testutil.MockCommand(c, "systemctl", "")
	defer sysctlCmd.Restore()

	mountCmd := testutil.MockCommand(c, "systemd-mount", "")
	defer mountCmd.Restore()

	// already mounted
	restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir) + "\n" +
		fmt.Sprintf(mountSnapSaveFmt, dirs.GlobalRootDir))
	defer restore()

	err = mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(sysctlCmd.Calls(), HasLen, 0)
	c.Check(mountCmd.Calls(), HasLen, 0)

	// known as available
	c.Check(devicestate.SaveAvailable(mgr), Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerStartupUC20NoUbuntuSave(c *C) {
	modeEnv := &boot.Modeenv{Mode: "run"}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)
	s.setUC20PCModelInState(c)

	// create a new manager so that the modeenv we mocked in read
	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	cmd := testutil.MockCommand(c, "systemd-mount", "")
	defer cmd.Restore()

	// ubuntu-save not mounted
	err = mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(cmd.Calls(), HasLen, 0)

	// known as available
	c.Check(devicestate.SaveAvailable(mgr), Equals, true)
}

func (s *deviceMgrSuite) TestDeviceManagerStartupUC20UbuntuSaveErr(c *C) {
	modeEnv := &boot.Modeenv{Mode: "run"}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)
	s.setUC20PCModelInState(c)

	// create a new manager so that the modeenv we mocked in read
	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	sysctlCmd := testutil.MockCommand(c, "systemctl", "echo 'Failed to start var-lib-snapd-save.mount: Unit var-lib-snapd-save.mount not found.'; exit 1")
	defer sysctlCmd.Restore()

	mountCmd := testutil.MockCommand(c, "systemd-mount", "echo failed; exit 1")
	defer mountCmd.Restore()

	restore := osutil.MockMountInfo(fmt.Sprintf(mountRunMntUbuntuSaveFmt, dirs.GlobalRootDir))
	defer restore()

	err = mgr.StartUp()
	c.Assert(err, ErrorMatches, "cannot set up ubuntu-save: cannot bind mount .*/run/mnt/ubuntu-save under .*/var/lib/snapd/save: failed")
	c.Check(sysctlCmd.Calls(), DeepEquals, [][]string{
		{"systemctl", "start", "var-lib-snapd-save.mount"},
	})
	c.Check(mountCmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", "-o", "bind", boot.InitramfsUbuntuSaveDir, dirs.SnapSaveDir},
	})

	// known as not available
	c.Check(devicestate.SaveAvailable(mgr), Equals, false)
}

func (s *deviceMgrSuite) TestDeviceManagerStartupNonUC20NoUbuntuSave(c *C) {
	err := os.RemoveAll(dirs.SnapModeenvFileUnder(dirs.GlobalRootDir))
	c.Assert(err, IsNil)
	// create a new manager so that we know it does not see the modeenv
	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)

	sysctlCmd := testutil.MockCommand(c, "systemctl", "")
	defer sysctlCmd.Restore()

	mountCmd := testutil.MockCommand(c, "systemd-mount", "")
	defer mountCmd.Restore()

	// ubuntu-save not mounted
	err = mgr.StartUp()
	c.Assert(err, IsNil)
	c.Check(sysctlCmd.Calls(), HasLen, 0)
	c.Check(mountCmd.Calls(), HasLen, 0)

	// known as not available
	c.Check(devicestate.SaveAvailable(mgr), Equals, false)
}

var kernelYamlNoFdeSetup = `name: pc-kernel
version: 1.0
type: kernel
`

var kernelYamlWithFdeSetup = `name: pc-kernel
version: 1.0
type: kernel
hooks:
 fde-setup:
`

func (s *deviceMgrSuite) TestHasFdeSetupHook(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	for _, tc := range []struct {
		kernelYaml      string
		hasFdeSetupHook bool
	}{
		{kernelYamlNoFdeSetup, false},
		{kernelYamlWithFdeSetup, true},
	} {
		makeInstalledMockKernelSnap(c, st, tc.kernelYaml)

		hasHook, err := devicestate.DeviceManagerHasFDESetupHook(s.mgr, nil)
		c.Assert(err, IsNil)
		c.Check(hasHook, Equals, tc.hasFdeSetupHook)
	}
}

func (s *deviceMgrSuite) TestHasFdeSetupHookOtherKernel(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})

	otherSI := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
	_, otherInfo := snaptest.MakeTestSnapInfoWithFiles(c, kernelYamlWithFdeSetup, nil, otherSI)
	makeInstalledMockKernelSnap(c, st, kernelYamlNoFdeSetup)

	hasHook, err := devicestate.DeviceManagerHasFDESetupHook(s.mgr, nil)
	c.Assert(err, IsNil)
	c.Check(hasHook, Equals, false)

	hasHook, err = devicestate.DeviceManagerHasFDESetupHook(s.mgr, otherInfo)
	c.Assert(err, IsNil)
	c.Check(hasHook, Equals, true)
}

func (s *deviceMgrSuite) TestRunFDESetupHookHappy(c *C) {
	st := s.state

	st.Lock()
	makeInstalledMockKernelSnap(c, st, kernelYamlWithFdeSetup)
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	st.Unlock()

	mockKey := keys.EncryptionKey{1, 2, 3, 4}

	var hookCalled []string
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		// check that the context has the right data
		c.Check(ctx.HookName(), Equals, "fde-setup")
		var fdeSetup fde.SetupRequest
		ctx.Get("fde-setup-request", &fdeSetup)
		c.Check(fdeSetup, DeepEquals, fde.SetupRequest{
			Op:      "op",
			Key:     mockKey[:],
			KeyName: "some-key-name",
		})
		ctx.Set("fde-setup-result", []byte("result"))
		hookCalled = append(hookCalled, ctx.InstanceName())
		return nil, nil
	}

	rhk := hookstate.MockRunHook(hookInvoke)
	defer rhk()

	s.o.Loop()
	defer s.o.Stop()

	req := &fde.SetupRequest{
		Op:      "op",
		Key:     mockKey[:],
		KeyName: "some-key-name",
	}
	st.Lock()
	res, err := devicestate.DeviceManagerRunFDESetupHook(s.mgr, req)
	st.Unlock()
	c.Assert(err, IsNil)
	c.Check(res, DeepEquals, []byte("result"))
	c.Check(hookCalled, DeepEquals, []string{"pc-kernel"})
}

func (s *deviceMgrSuite) TestRunFDESetupHookErrors(c *C) {
	st := s.state

	st.Lock()
	makeInstalledMockKernelSnap(c, st, kernelYamlWithFdeSetup)
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	st.Unlock()

	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		return nil, fmt.Errorf("hook failed")
	}

	rhk := hookstate.MockRunHook(hookInvoke)
	defer rhk()

	s.o.Loop()
	defer s.o.Stop()

	req := &fde.SetupRequest{
		Op: "op",
	}
	st.Lock()
	_, err := devicestate.DeviceManagerRunFDESetupHook(s.mgr, req)
	st.Unlock()
	c.Assert(err, ErrorMatches, `cannot run hook for "op": run hook "fde-setup": hook failed`)
}

func (s *deviceMgrSuite) TestRunFDESetupHookErrorResult(c *C) {
	st := s.state

	st.Lock()
	makeInstalledMockKernelSnap(c, st, kernelYamlWithFdeSetup)
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc",
	})
	st.Unlock()

	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		// Simulate an incorrect type for fde-setup-result here to
		// test error string from runFDESetupHook.
		// This should never happen in practice, "snapctl fde-setup"
		// will always set this to []byte
		ctx.Lock()
		ctx.Set("fde-setup-result", "not-bytes")
		ctx.Unlock()
		return nil, nil
	}

	rhk := hookstate.MockRunHook(hookInvoke)
	defer rhk()

	s.o.Loop()
	defer s.o.Stop()

	req := &fde.SetupRequest{
		Op: "op",
	}
	st.Lock()
	_, err := devicestate.DeviceManagerRunFDESetupHook(s.mgr, req)
	st.Unlock()
	c.Assert(err, ErrorMatches, `cannot get result from fde-setup hook "op": cannot unmarshal context value for "fde-setup-result": illegal base64 data at input byte 3`)
}

type startOfOperationTimeSuite struct {
	state  *state.State
	mgr    *devicestate.DeviceManager
	runner *state.TaskRunner
}

var _ = Suite(&startOfOperationTimeSuite{})

func (s *startOfOperationTimeSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	os.MkdirAll(dirs.SnapRunDir, 0755)

	s.state = state.New(nil)
	s.runner = state.NewTaskRunner(s.state)
	s.mgr = nil
}

func (s *startOfOperationTimeSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
}

func (s *startOfOperationTimeSuite) manager(c *C) *devicestate.DeviceManager {
	if s.mgr == nil {
		hookMgr, err := hookstate.Manager(s.state, s.runner)
		c.Assert(err, IsNil)
		mgr, err := devicestate.Manager(s.state, hookMgr, s.runner, nil)
		c.Assert(err, IsNil)
		s.mgr = mgr
	}
	return s.mgr
}

func (s *startOfOperationTimeSuite) TestStartOfOperationTimeFromSeedTime(c *C) {
	mgr := s.manager(c)

	st := s.state
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

func (s *startOfOperationTimeSuite) TestStartOfOperationTimeAlreadySet(c *C) {
	mgr := s.manager(c)

	st := s.state
	st.Lock()
	defer st.Unlock()

	op := time.Now().AddDate(0, -1, 0)
	st.Set("start-of-operation-time", op)

	operationTime, err := mgr.StartOfOperationTime()
	c.Assert(err, IsNil)
	c.Check(operationTime.Equal(op), Equals, true)
}

func (s *startOfOperationTimeSuite) TestStartOfOperationTimeNoSeedTime(c *C) {
	mgr := s.manager(c)

	st := s.state
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
	operationTime, err = s.manager(c).StartOfOperationTime()
	c.Assert(err, IsNil)
	c.Check(operationTime.Equal(prev), Equals, true)
}

func (s *startOfOperationTimeSuite) TestStartOfOperationErrorIfPreseed(c *C) {
	restore := snapdenv.MockPreseeding(true)
	defer restore()

	mgr := s.manager(c)
	st := s.state

	st.Lock()
	defer st.Unlock()
	_, err := mgr.StartOfOperationTime()
	c.Assert(err, ErrorMatches, `internal error: unexpected call to StartOfOperationTime in preseed mode`)
}

func (s *deviceMgrSuite) TestCanAutoRefreshNTP(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// CanAutoRefresh is ready
	s.state.Set("seeded", true)
	s.makeModelAssertionInState(c, "canonical", "pc", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc",
		Serial: "8989",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc", "8989")

	// now check that the ntp-sync information is honored
	n := 0
	ntpSynced := false
	restore := devicestate.MockTimeutilIsNTPSynchronized(func() (bool, error) {
		n++
		return ntpSynced, nil
	})
	defer restore()

	// not ntp-synced
	ok, err := devicestate.CanAutoRefresh(s.state)
	c.Assert(err, IsNil)
	c.Check(ok, Equals, false)
	c.Check(n, Equals, 1)

	// now ntp-synced
	ntpSynced = true
	ok, err = devicestate.CanAutoRefresh(s.state)
	c.Assert(err, IsNil)
	c.Check(ok, Equals, true)
	c.Check(n, Equals, 2)

	// and the result was cached
	ok, err = devicestate.CanAutoRefresh(s.state)
	c.Assert(err, IsNil)
	c.Check(ok, Equals, true)
	c.Check(n, Equals, 2)
}

func (s *deviceMgrSuite) TestNTPSyncedOrWaitedLongerThan(c *C) {
	restore := devicestate.MockTimeutilIsNTPSynchronized(func() (bool, error) {
		return false, nil
	})
	defer restore()

	// NTP is not synced yet and the (arbitrary selected) wait
	// time of 12h since the device manager got started is not
	// over yet
	syncedOrWaited := devicestate.DeviceManagerNTPSyncedOrWaitedLongerThan(s.mgr, 12*time.Hour)
	c.Check(syncedOrWaited, Equals, false)

	// NTP is also not synced here but the wait time of 1
	// Nanosecond since the device manager got started is
	// certainly over
	syncedOrWaited = devicestate.DeviceManagerNTPSyncedOrWaitedLongerThan(s.mgr, 1*time.Nanosecond)
	c.Check(syncedOrWaited, Equals, true)
}

func (s *deviceMgrSuite) TestNTPSyncedOrWaitedNoTimedate1(c *C) {
	n := 0
	restore := devicestate.MockTimeutilIsNTPSynchronized(func() (bool, error) {
		n++
		// no timedate1
		return false, timeutil.NoTimedate1Error{Err: fmt.Errorf("boom")}
	})
	defer restore()

	// There is no timedate1 dbus service, no point in waiting
	syncedOrWaited := devicestate.DeviceManagerNTPSyncedOrWaitedLongerThan(s.mgr, 12*time.Hour)
	c.Check(syncedOrWaited, Equals, true)
	c.Check(n, Equals, 1)

	// and the result was cached
	syncedOrWaited = devicestate.DeviceManagerNTPSyncedOrWaitedLongerThan(s.mgr, 12*time.Hour)
	c.Check(syncedOrWaited, Equals, true)
	c.Check(n, Equals, 1)
}

func (s *deviceMgrSuite) TestVoidDirPermissionsGetFixed(c *C) {
	// create /var/lib/snapd/void with the wrong permissions
	err := os.MkdirAll(dirs.SnapVoidDir, 0755)
	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	defer restore()

	mgr, err := devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore)
	c.Assert(err, IsNil)
	err = mgr.StartUp()
	c.Assert(err, IsNil)

	st, err := os.Stat(dirs.SnapVoidDir)
	c.Assert(err, IsNil)
	c.Check(int(st.Mode().Perm()), Equals, 0111)
	msgs := strings.TrimSpace(logbuf.String())
	c.Check(msgs, Matches, "(?sm).*fixing permissions of .*/var/lib/snapd/void to 0111")
	c.Check(strings.Split(msgs, "\n"), HasLen, 1)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsurePostFactoryResetEncrypted(c *C) {
	defer release.MockOnClassic(false)

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()
	devicestate.SetBootOkRan(s.mgr, false)
	devicestate.SetSystemMode(s.mgr, "run")

	// encrypted system
	mockSnapFDEFile(c, "marker", nil)
	err := os.WriteFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"),
		[]byte("save-key"), 0644)
	c.Assert(err, IsNil)
	c.Assert(os.MkdirAll(boot.InitramfsSeedEncryptionKeyDir, 0755), IsNil)
	err = os.WriteFile(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
		[]byte("old"), 0644)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset"),
		[]byte("save"), 0644)
	c.Assert(err, IsNil)
	// matches the .factory key
	factoryResetMarkercontent := []byte(`{"fallback-save-key-sha3-384":"d192153f0a50e826c6eb400c8711750ed0466571df1d151aaecc8c73095da7ec104318e7bf74d5e5ae2940827bf8402b"}
`)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDeviceDir, "factory-reset"), factoryResetMarkercontent, 0644), IsNil)

	completeCalls := 0
	restore := devicestate.MockMarkFactoryResetComplete(func(encrypted bool) error {
		completeCalls++
		c.Check(encrypted, Equals, true)
		return nil
	})
	defer restore()
	transitionCalls := 0
	restore = devicestate.MockSecbootTransitionEncryptionKeyChange(func(mountpoint string, key keys.EncryptionKey) error {
		transitionCalls++
		c.Check(mountpoint, Equals, boot.InitramfsUbuntuSaveDir)
		c.Check(key, DeepEquals, keys.EncryptionKey([]byte("save-key")))
		return nil
	})
	defer restore()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Check(completeCalls, Equals, 1)
	c.Check(transitionCalls, Equals, 1)
	// factory reset marker is gone, the key was verified successfully
	c.Check(filepath.Join(dirs.SnapDeviceDir, "factory-reset"), testutil.FileAbsent)
	c.Check(filepath.Join(dirs.SnapFDEDir, "marker"), testutil.FilePresent)

	completeCalls = 0
	transitionCalls = 0
	// try again, no marker, nothing should happen
	devicestate.SetPostFactoryResetRan(s.mgr, false)
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)
	// nothing was called
	c.Check(completeCalls, Equals, 0)
	c.Check(transitionCalls, Equals, 0)

	// have the marker, but migrate the key as if boot code would do it and
	// try again, in this setup the marker hash matches the migrated key
	c.Check(os.Rename(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset"),
		filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key")),
		IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDeviceDir, "factory-reset"), factoryResetMarkercontent, 0644), IsNil)

	devicestate.SetPostFactoryResetRan(s.mgr, false)
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(completeCalls, Equals, 1)
	c.Check(transitionCalls, Equals, 1)
	// the marker was again removed
	c.Check(filepath.Join(dirs.SnapDeviceDir, "factory-reset"), testutil.FileAbsent)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsurePostFactoryResetEncryptedError(c *C) {
	defer release.MockOnClassic(false)

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()
	devicestate.SetBootOkRan(s.mgr, false)
	devicestate.SetSystemMode(s.mgr, "run")

	// encrypted system
	mockSnapFDEFile(c, "marker", nil)
	c.Assert(os.MkdirAll(boot.InitramfsSeedEncryptionKeyDir, 0755), IsNil)
	err := os.WriteFile(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key"),
		[]byte("old"), 0644)
	c.Check(err, IsNil)
	err = os.WriteFile(filepath.Join(boot.InitramfsSeedEncryptionKeyDir, "ubuntu-save.recovery.sealed-key.factory-reset"),
		[]byte("save"), 0644)
	c.Check(err, IsNil)
	// does not match the save key
	factoryResetMarkercontent := []byte(`{"fallback-save-key-sha3-384":"uh-oh"}
`)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDeviceDir, "factory-reset"), factoryResetMarkercontent, 0644), IsNil)

	completeCalls := 0
	restore := devicestate.MockMarkFactoryResetComplete(func(encrypted bool) error {
		completeCalls++
		c.Check(encrypted, Equals, true)
		return nil
	})
	defer restore()

	err = s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "devicemgr: cannot verify factory reset marker: fallback sealed key digest mismatch, got d192153f0a50e826c6eb400c8711750ed0466571df1d151aaecc8c73095da7ec104318e7bf74d5e5ae2940827bf8402b expected uh-oh")

	c.Check(completeCalls, Equals, 0)
	// factory reset marker is gone, the key was verified successfully
	c.Check(filepath.Join(dirs.SnapDeviceDir, "factory-reset"), testutil.FilePresent)
	c.Check(filepath.Join(dirs.SnapFDEDir, "marker"), testutil.FilePresent)

	// try again, no marker, hit the same error
	devicestate.SetPostFactoryResetRan(s.mgr, false)
	err = s.mgr.Ensure()
	c.Assert(err, ErrorMatches, "devicemgr: cannot verify factory reset marker: fallback sealed key digest mismatch, got d192153f0a50e826c6eb400c8711750ed0466571df1d151aaecc8c73095da7ec104318e7bf74d5e5ae2940827bf8402b expected uh-oh")
	c.Check(completeCalls, Equals, 0)

	// and again, but not resetting the 'ran' check, so nothing is checked or called
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)
	c.Check(completeCalls, Equals, 0)
}

func (s *deviceMgrSuite) TestDeviceManagerEnsurePostFactoryResetUnencrypted(c *C) {
	defer release.MockOnClassic(false)

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()
	devicestate.SetBootOkRan(s.mgr, false)
	devicestate.SetSystemMode(s.mgr, "run")

	// mock the factory reset marker of a system that isn't encrypted
	c.Assert(os.MkdirAll(dirs.SnapDeviceDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapDeviceDir, "factory-reset"), []byte("{}"), 0644), IsNil)

	completeCalls := 0
	restore := devicestate.MockMarkFactoryResetComplete(func(encrypted bool) error {
		completeCalls++
		c.Check(encrypted, Equals, false)
		return nil
	})
	defer restore()

	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	c.Check(completeCalls, Equals, 1)
	// factory reset marker is gone
	c.Check(filepath.Join(dirs.SnapDeviceDir, "factory-reset"), testutil.FileAbsent)

	// try again, no marker, nothing should happen
	devicestate.SetPostFactoryResetRan(s.mgr, false)
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)
	// nothing was called
	c.Check(completeCalls, Equals, 1)
}

func (s *deviceMgrSuite) mockSystemUser(c *C, username string, expiration time.Time) {
	_, err := auth.NewUser(s.state, auth.NewUserParams{
		Username:   username,
		Email:      "email1@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
		Expiration: expiration,
	})
	c.Assert(err, IsNil)
}

func (s *deviceMgrSuite) mockSystemMode(c *C, mode string) {
	modeEnv := &boot.Modeenv{Mode: mode}
	err := modeEnv.WriteTo("")
	c.Assert(err, IsNil)
	devicestate.SetSystemMode(s.mgr, mode)
}

func (s *deviceMgrSuite) testExpiredUserRemoved(c *C, userToRemove string, extraUsers bool) {
	// Mock the delete user callback to verify it's correctly called. On ubuntu core
	// systems ExtraUsers should be set, where on classic systems ExtraUsers should not
	// be set
	var delUserCalled bool
	r := devicestate.MockOsutilDelUser(func(name string, opts *osutil.DelUserOptions) error {
		delUserCalled = true
		c.Check(name, Equals, userToRemove)
		c.Check(opts, NotNil)
		c.Check(opts.ExtraUsers, Equals, extraUsers)
		return nil
	})
	defer r()

	s.state.Unlock()
	err := devicestate.EnsureExpiredUsersRemoved(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(delUserCalled, Equals, true)
}

func (s *deviceMgrSuite) testExpiredUserNotRemoved(c *C) {
	// Mock the delete user callback to verify it's correctly called. On ubuntu core
	// systems ExtraUsers should be set, where on classic systems ExtraUsers should not
	// be set
	var delUserCalled bool
	r := devicestate.MockOsutilDelUser(func(name string, opts *osutil.DelUserOptions) error {
		delUserCalled = true
		return nil
	})
	defer r()

	s.state.Unlock()
	err := devicestate.EnsureExpiredUsersRemoved(s.mgr)
	c.Assert(err, IsNil)
	c.Assert(delUserCalled, Equals, false)
}

func (s *deviceMgrSuite) TestEnsureExpiredUsersRemovedOnCore(c *C) {
	s.mockSystemMode(c, "run")
	s.state.Lock()

	// Set seeded otherwise we won't get very far in this test, as we don't
	// remove users when not seeded.
	s.state.Set("seeded", true)

	// We mock a few users, with one of them having expired. We then run
	// the code to see that it correctly removes it from the system. Important
	// to also test with a user that has no expiration to make sure the previous
	// users are unaffected.
	s.mockSystemUser(c, "user1", time.Time{})
	s.mockSystemUser(c, "expires-soon", time.Now().Add(time.Minute*5))
	s.mockSystemUser(c, "remove-me", time.Now().Add(-(time.Minute * 5)))
	s.testExpiredUserRemoved(c, "remove-me", true)
}

func (s *deviceMgrSuite) TestEnsureExpiredUsersRemovedOnClassic(c *C) {
	// Mock being on classic, then the EnsureExpiredUsersRemoved should be a no-op
	r := release.MockOnClassic(true)
	defer r()
	s.state.Lock()

	// It's not really needed to set seeded here as the check comes after
	// we check for classic - however in the future, if someone were to reorder
	// checks this would still pass if we didn't set this
	s.state.Set("seeded", true)

	// We mock a few users, with one of them having expired. We then run
	// the code to see that it correctly removes it from the system. Important
	// to also test with a user that has no expiration to make sure the previous
	// users are unaffected.
	s.mockSystemUser(c, "user1", time.Time{})
	s.mockSystemUser(c, "expires-soon", time.Now().Add(time.Minute*5))
	s.mockSystemUser(c, "remove-me", time.Now().Add(-(time.Minute * 5)))
	s.testExpiredUserRemoved(c, "remove-me", false)
}

func (s *deviceMgrSuite) TestEnsureExpiredUsersRemovedNotRecoverMode(c *C) {
	// mock recovery mode
	s.mockSystemMode(c, "recover")

	s.state.Lock()

	// It's not really needed to set seeded here as the check comes after
	// we check for classic - however in the future, if someone were to reorder
	// checks this would still pass if we didn't set this
	s.state.Set("seeded", true)

	// Mock a user that would be expired
	s.mockSystemUser(c, "remove-me", time.Now().Add(-(time.Minute * 5)))
	s.testExpiredUserNotRemoved(c)
}

func (s *deviceMgrSuite) TestEnsureExpiredUsersRemovedNotInstallMode(c *C) {
	// mock install mode
	s.mockSystemMode(c, "install")

	s.state.Lock()

	// It's not really needed to set seeded here as the check comes after
	// we check for classic - however in the future, if someone were to reorder
	// checks this would still pass if we didn't set this
	s.state.Set("seeded", true)

	// Mock a user that would be expired, but expect it not to be removed
	s.mockSystemUser(c, "remove-me", time.Now().Add(-(time.Minute * 5)))
	s.testExpiredUserNotRemoved(c)
}

func (s *deviceMgrSuite) TestEnsureExpiredUsersRemovedNotUnseeded(c *C) {
	s.mockSystemMode(c, "run")
	s.state.Lock()

	// The default seems to be false, but lets be explicit about setting
	// this to false.
	s.state.Set("seeded", false)

	// Mock a user that would be expired, but expect it not to be removed
	s.mockSystemUser(c, "remove-me", time.Now().Add(-(time.Minute * 5)))
	s.testExpiredUserNotRemoved(c)
}

func (s *deviceMgrSuite) cacheDeviceCore20Seed(c *C) {

	// now create a minimal uc20 seed dir with snaps/assertions
	ss := &seedtest.SeedSnaps{
		StoreSigning: s.storeSigning,
		Brands:       s.brands,
	}

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{
		SeedSnaps: *ss,
		SeedDir:   dirs.SnapSeedDir,
	}

	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
        structure:
        - name: ubuntu-seed
          role: system-seed
          type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
          size: 1G
        - name: ubuntu-data
          role: system-data
          type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
          size: 2G
`
	makeSnap := func(yamlKey string) {
		var files [][]string
		if yamlKey == "pc=20" {
			files = append(files, []string{"meta/gadget.yaml", gadgetYaml})
		}
		seed20.MakeAssertedSnap(c, seedtest.SampleSnapYaml[yamlKey], files, snap.R(1), "canonical", seed20.StoreSigning.Database)
	}

	makeSnap("snapd")
	makeSnap("pc-kernel=20")
	makeSnap("core20")
	makeSnap("pc=20")
	optSnapPath := snaptest.MakeTestSnapWithFiles(c, seedtest.SampleSnapYaml["optional20-a"], nil)

	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              seed20.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              seed20.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   seed20.AssertedSnapID("snapd"),
				"type": "snapd",
			},
			map[string]interface{}{
				"name": "core20",
				"id":   seed20.AssertedSnapID("core20"),
				"type": "base",
			}},
	}

	modelAs := seed20.MakeSeed(c, "20220401", "my-brand", "my-model", model, []*seedwriter.OptionsSnap{{Path: optSnapPath}})
	c.Assert(modelAs, NotNil)

	// mock /he modeenv file
	m := boot.Modeenv{
		Mode:           "install",
		RecoverySystem: "20220401",
	}
	err := m.WriteTo("")
	c.Assert(err, IsNil)

	// reload device seed
	_, _, err = devicestate.ReloadEarlyDeviceSeed(s.mgr, state.ErrNoState)
	c.Assert(err, IsNil)

	// not fully realistic but avoids more mocking
	devicestate.SetBootOkRan(s.mgr, true)
}

func (s *deviceMgrSuite) TestHandleAutoImportAssertionClassic(c *C) {
	a := devicestate.MockProcessAutoImportAssertion(func(*state.State, seed.Seed, asserts.RODatabase, func(batch *asserts.Batch) error) error {
		panic("trying to process auto-import-assertion on classic system")
	})
	defer a()

	release.OnClassic = true
	s.mockSystemMode(c, "run")

	s.state.Lock()
	defer s.state.Unlock()

	err := devicestate.EnsureAutoImportAssertions(s.mgr)
	c.Check(err, IsNil)

	// ensure state has not been changed
	var autoImported bool
	err = s.state.Get("asserts-early-auto-imported", &autoImported)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
	c.Assert(autoImported, Equals, false)
}

func (s *deviceMgrSuite) testHandleAutoImportAssertionInstallModes(c *C, mode string) {
	a := devicestate.MockProcessAutoImportAssertion(func(*state.State, seed.Seed, asserts.RODatabase, func(batch *asserts.Batch) error) error {
		panic("trying to process auto-import-assertion in install modes")
	})
	defer a()

	s.mockSystemMode(c, mode)
	s.state.Lock()
	s.cacheDeviceCore20Seed(c)
	s.state.Set("seeded", nil)
	s.state.Unlock()

	err := devicestate.EnsureAutoImportAssertions(s.mgr)
	c.Check(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// ensure state has not been changed
	var autoImported bool
	err = s.state.Get("asserts-early-auto-imported", &autoImported)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
	c.Assert(autoImported, Equals, false)
}

func (s *deviceMgrSuite) TestHandleAutoImportAssertionInstallMode(c *C) {
	s.testHandleAutoImportAssertionInstallModes(c, "install")
}

func (s *deviceMgrSuite) TestHandleAutoImportAssertionFactoryResetMode(c *C) {
	s.testHandleAutoImportAssertionInstallModes(c, "factory-reset")
}

func (s *deviceMgrSuite) TestHandleAutoImportAssertionWhenDone(c *C) {
	a := devicestate.MockProcessAutoImportAssertion(func(*state.State, seed.Seed, asserts.RODatabase, func(batch *asserts.Batch) error) error {
		panic("trying to process auto-import-assertion after it was already processed")
	})
	defer a()

	s.mockSystemMode(c, "run")

	// set state as processed
	s.state.Lock()
	s.state.Set("asserts-early-auto-imported", true)
	s.cacheDeviceCore20Seed(c)
	s.seeding()
	s.state.Unlock()

	err := devicestate.EnsureAutoImportAssertions(s.mgr)
	c.Check(err, IsNil)

	// check state has not changed
	s.state.Lock()
	defer s.state.Unlock()
	var autoImported bool
	err = s.state.Get("asserts-early-auto-imported", &autoImported)
	c.Assert(err, IsNil)
	c.Assert(autoImported, Equals, true)
}

func (s *deviceMgrSuite) TestHandleAutoImportAssertionNoSeedCache(c *C) {
	a := devicestate.MockProcessAutoImportAssertion(func(*state.State, seed.Seed, asserts.RODatabase, func(batch *asserts.Batch) error) error {
		panic("trying to process auto-import-assertion without cached system seed")
	})
	defer a()

	s.mockSystemMode(c, "run")

	err := devicestate.EnsureAutoImportAssertions(s.mgr)
	c.Check(err, IsNil)

	// ensure state has not been changed
	s.state.Lock()
	defer s.state.Unlock()
	var autoImported bool
	err = s.state.Get("asserts-early-auto-imported", &autoImported)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
	c.Assert(autoImported, Equals, false)
}

func (s *deviceMgrSuite) TestHandleAutoImportAssertionFailed(c *C) {
	a := devicestate.MockProcessAutoImportAssertion(func(*state.State, seed.Seed, asserts.RODatabase, func(batch *asserts.Batch) error) error {
		return fmt.Errorf("failed to add user from system user assertions")
	})
	defer a()

	s.mockSystemMode(c, "run")

	s.state.Lock()
	s.cacheDeviceCore20Seed(c)
	s.seeding()
	s.state.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	err := s.mgr.Ensure()
	c.Check(err, IsNil)

	// ensure state has not been changed
	s.state.Lock()
	defer s.state.Unlock()
	var autoImported bool
	err = s.state.Get("asserts-early-auto-imported", &autoImported)
	c.Assert(err, IsNil)
	c.Assert(autoImported, Equals, true)
	c.Assert(logbuf.String(), testutil.Contains, `failed to add user from system user assertions`)
}

func (s *deviceMgrSuite) TestHandleAutoImportAssertionAlreadySeeded(c *C) {
	a := devicestate.MockProcessAutoImportAssertion(func(*state.State, seed.Seed, asserts.RODatabase, func(batch *asserts.Batch) error) error {
		return fmt.Errorf("failed to process auto import assertion")
	})
	defer a()

	s.mockSystemMode(c, "run")

	s.state.Lock()
	s.cacheDeviceCore20Seed(c)
	s.state.Set("seeded", true)
	s.state.Unlock()

	err := s.mgr.Ensure()
	c.Check(err, IsNil)

	// ensure state has not been changed
	s.state.Lock()
	defer s.state.Unlock()
	var autoImported bool
	err = s.state.Get("asserts-early-auto-imported", &autoImported)
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
	c.Assert(autoImported, Equals, false)
}

func (s *deviceMgrSuite) TestHandleAutoImportAssertionHappy(c *C) {

	s.mockSystemMode(c, "run")

	s.state.Lock()
	s.cacheDeviceCore20Seed(c)
	s.seeding()
	s.state.Unlock()

	err := s.mgr.Ensure()
	c.Check(err, IsNil)

	// check state is set as done
	s.state.Lock()
	defer s.state.Unlock()
	var autoImported bool
	err = s.state.Get("asserts-early-auto-imported", &autoImported)
	c.Assert(err, IsNil)
	c.Assert(autoImported, Equals, true)
}

func (s *deviceMgrSuite) TestDefaultRecoverySystem(c *C) {
	// no recovery system set
	s.state.Lock()
	s.state.Set("default-recovery-system", nil)
	s.state.Unlock()

	_, err := s.mgr.DefaultRecoverySystem()
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)

	expectedSystem := devicestate.DefaultRecoverySystem{
		System:   "label",
		Model:    "model",
		BrandID:  "brand",
		Revision: 1,
	}

	// recovery system set
	s.state.Lock()
	s.state.Set("default-recovery-system", expectedSystem)
	s.state.Unlock()

	system, err := s.mgr.DefaultRecoverySystem()
	c.Assert(err, IsNil)
	c.Check(*system, Equals, expectedSystem)
}
