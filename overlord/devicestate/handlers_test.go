// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

// TODO: should we move this into a new handlers suite?
func (s *deviceMgrSuite) TestSetModelHandlerNewRevision(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"revision":       "1",
		"required-snaps": []interface{}{"foo", "bar"},
	})
	// foo and bar
	fooSI := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
	}
	barSI := &snap.SideInfo{
		RealName: "foo",
		Revision: snap.R(1),
	}
	pcKernelSI := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(1),
	}
	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{fooSI}),
		Current:  fooSI.Revision,
		Flags:    snapstate.Flags{Required: true},
	})
	snapstate.Set(s.state, "bar", &snapstate.SnapState{
		SnapType: "app",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{barSI}),
		Current:  barSI.Revision,
		Flags:    snapstate.Flags{Required: true},
	})
	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{pcKernelSI}),
		Current:  pcKernelSI.Revision,
		Flags:    snapstate.Flags{Required: true},
	})
	s.state.Unlock()

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "other-kernel",
		"gadget":         "pc",
		"revision":       "2",
		"required-snaps": []interface{}{"foo"},
	})

	s.state.Lock()
	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("sample", "...")
	chg.Set("new-model", string(asserts.Encode(newModel)))
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	m := mylog.Check2(s.mgr.Model())

	c.Assert(m, DeepEquals, newModel)

	c.Assert(chg.Err(), IsNil)

	// check required
	var fooState snapstate.SnapState
	var barState snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "foo", &fooState))

	mylog.Check(snapstate.Get(s.state, "bar", &barState))

	c.Check(fooState.Flags.Required, Equals, true)
	c.Check(barState.Flags.Required, Equals, false)
	// the kernel is no longer required
	var kernelState snapstate.SnapState
	mylog.Check(snapstate.Get(s.state, "pc-kernel", &kernelState))

	c.Check(kernelState.Flags.Required, Equals, false)
}

func (s *deviceMgrSuite) TestSetModelHandlerValidationSets(c *C) {
	s.state.Lock()
	const accountID = "canonical"
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: accountID,
		Model: "pc-model",
	})
	s.makeModelAssertionInState(c, accountID, "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
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

	pcKernelSI := &snap.SideInfo{
		RealName: "pc-kernel",
		Revision: snap.R(1),
	}

	snapstate.Set(s.state, "pc-kernel", &snapstate.SnapState{
		SnapType: "kernel",
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{pcKernelSI}),
		Current:  pcKernelSI.Revision,
		Flags:    snapstate.Flags{Required: true},
	})

	signer := s.brands.Signing(accountID)

	vsetOne := mylog.Check2(signer.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": accountID,
		"series":       "16",
		"account-id":   accountID,
		"name":         "vset-1",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-1",
				"id":       snaptest.AssertedSnapID("snap-1"),
				"presence": "optional",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))


	assertstate.Add(s.state, vsetOne)

	vsetTwo := mylog.Check2(signer.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": accountID,
		"series":       "16",
		"account-id":   accountID,
		"name":         "vset-2",
		"sequence":     "2",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "snap-2",
				"id":       snaptest.AssertedSnapID("snap-2"),
				"presence": "optional",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))


	assertstate.Add(s.state, vsetTwo)

	newModel := s.brands.Model(accountID, "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"revision":     "2",
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
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": accountID,
				"name":       "vset-1",
				"mode":       "enforce",
			},
			map[string]interface{}{
				"account-id": accountID,
				"name":       "vset-2",
				"sequence":   "2",
				"mode":       "enforce",
			},
		},
	})

	newSystemLabel := time.Now().Format("20060102")
	s.state.Set("tried-systems", []string{newSystemLabel})

	t := s.state.NewTask("set-model", "set-model test")

	systemDirectory := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", newSystemLabel)

	t.Set("recovery-system-setup", &devicestate.RecoverySystemSetup{
		Label:     newSystemLabel,
		Directory: systemDirectory,
	})

	modeenv := &boot.Modeenv{
		Mode:                   "run",
		CurrentRecoverySystems: []string{newSystemLabel},
		GoodRecoverySystems:    []string{newSystemLabel},
		CurrentKernels:         []string{},
		Model:                  newModel.Model(),
		BrandID:                newModel.BrandID(),
		Grade:                  string(newModel.Grade()),
		ModelSignKeyID:         newModel.SignKeyID(),
	}

	c.Assert(modeenv.WriteTo(""), IsNil)

	chg := s.state.NewChange("new-model-change", "...")
	chg.Set("new-model", string(asserts.Encode(newModel)))

	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	m := mylog.Check2(s.mgr.Model())

	c.Assert(m, DeepEquals, newModel)

	c.Assert(chg.Err(), IsNil)

	vsets := mylog.Check2(assertstate.TrackedEnforcedValidationSets(s.state))

	c.Check(vsets.Keys(), testutil.DeepUnsortedMatches, []snapasserts.ValidationSetKey{
		"16/canonical/vset-1/1",
		"16/canonical/vset-2/2",
	})
}

func (s *deviceMgrSuite) TestSetModelHandlerSameRevisionNoError(c *C) {
	model := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "1",
	})

	s.state.Lock()

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	mylog.Check(assertstate.Add(s.state, model))


	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("sample", "...")
	chg.Set("new-model", string(asserts.Encode(model)))
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.Err(), IsNil)
}

func (s *deviceMgrSuite) TestSetModelHandlerStoreSwitch(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "canonical",
		Model: "pc-model",
	})
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"revision":     "1",
	})
	s.state.Unlock()

	newModel := s.brands.Model("canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"store":        "switched-store",
		"revision":     "2",
	})

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod := mylog.Check2(devBE.Model())
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, newModel)
		}
		return &freshSessionStore{}
	}

	s.state.Lock()
	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("sample", "...")
	chg.Set("new-model", string(asserts.Encode(newModel)))
	chg.Set("device", auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		SessionMacaroon: "switched-store-session",
	})
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.Err(), IsNil)

	m := mylog.Check2(s.mgr.Model())

	c.Assert(m, DeepEquals, newModel)

	device := mylog.Check2(devicestatetest.Device(s.state))

	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		SessionMacaroon: "switched-store-session",
	})

	// cleanup
	_, ok := devicestate.CachedRemodelCtx(chg)
	c.Check(ok, Equals, true)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	_, ok = devicestate.CachedRemodelCtx(chg)
	c.Check(ok, Equals, false)
}

func (s *deviceMgrSuite) TestSetModelHandlerRereg(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "orig-serial",
	})
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "orig-serial")
	s.state.Unlock()

	newModel := s.brands.Model("canonical", "rereg-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
	})

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod := mylog.Check2(devBE.Model())
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, newModel)
		}
		return &freshSessionStore{}
	}

	s.state.Lock()
	t := s.state.NewTask("set-model", "set-model test")
	chg := s.state.NewChange("sample", "...")
	chg.Set("new-model", string(asserts.Encode(newModel)))
	chg.Set("device", auth.DeviceState{
		Brand:           "canonical",
		Model:           "rereg-model",
		Serial:          "orig-serial",
		SessionMacaroon: "switched-store-session",
	})
	chg.AddTask(t)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.Err(), IsNil)

	m := mylog.Check2(s.mgr.Model())

	c.Assert(m, DeepEquals, newModel)

	device := mylog.Check2(devicestatetest.Device(s.state))

	c.Check(device, DeepEquals, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "rereg-model",
		Serial:          "orig-serial",
		SessionMacaroon: "switched-store-session",
	})
}

func (s *deviceMgrSuite) TestDoPrepareRemodeling(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
	s.state.Set("refresh-privacy-key", "some-privacy-key")

	snapstatetest.InstallEssentialSnaps(c, s.state, "core18", nil, nil)

	var testStore snapstate.StoreService

	restore := devicestate.MockSnapstateInstallWithDeviceContext(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags, prqt snapstate.PrereqTracker, deviceCtx snapstate.DeviceContext, fromChange string) (*state.TaskSet, error) {
		c.Check(flags.Required, Equals, true)
		c.Check(deviceCtx, NotNil)
		c.Check(deviceCtx.ForRemodeling(), Equals, true)

		tDownload := s.state.NewTask("fake-download", fmt.Sprintf("Download %s", name))
		tDownload.Set("snap-setup", &snapstate.SnapSetup{
			SideInfo: &snap.SideInfo{
				RealName: name,
			},
		})
		tValidate := s.state.NewTask("validate-snap", fmt.Sprintf("Validate %s", name))
		tValidate.WaitFor(tDownload)
		tInstall := s.state.NewTask("fake-install", fmt.Sprintf("Install %s", name))
		tInstall.WaitFor(tValidate)
		ts := state.NewTaskSet(tDownload, tValidate, tInstall)
		ts.MarkEdge(tValidate, snapstate.LastBeforeLocalModificationsEdge)
		return ts, nil
	})
	defer restore()

	// set a model assertion
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.makeSerialAssertionInState(c, "canonical", "pc-model", "orig-serial")
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:           "canonical",
		Model:           "pc-model",
		Serial:          "orig-serial",
		SessionMacaroon: "old-session",
	})

	new := s.brands.Model("canonical", "rereg-model", map[string]interface{}{
		"architecture":   "amd64",
		"kernel":         "pc-kernel",
		"gadget":         "pc",
		"base":           "core18",
		"required-snaps": []interface{}{"new-required-snap-1", "new-required-snap-2"},
	})

	freshStore := &freshSessionStore{}
	testStore = freshStore

	s.newFakeStore = func(devBE storecontext.DeviceBackend) snapstate.StoreService {
		mod := mylog.Check2(devBE.Model())
		c.Check(err, IsNil)
		if err == nil {
			c.Check(mod, DeepEquals, new)
		}
		return testStore
	}

	cur := mylog.Check2(s.mgr.Model())


	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, cur, new))


	c.Check(remodCtx.Kind(), Equals, devicestate.ReregRemodel)

	chg := s.state.NewChange("remodel", "...")
	remodCtx.Init(chg)
	t := s.state.NewTask("prepare-remodeling", "...")
	chg.AddTask(t)

	// set new serial
	s.makeSerialAssertionInState(c, "canonical", "rereg-model", "orig-serial")
	chg.Set("device", auth.DeviceState{
		Brand:           "canonical",
		Model:           "rereg-model",
		Serial:          "orig-serial",
		SessionMacaroon: "switched-store-session",
	})

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()
	c.Assert(chg.Err(), IsNil)

	c.Check(freshStore.ensureDeviceSession, Equals, 1)

	// check that the expected tasks were injected
	tl := chg.Tasks()
	// 1 prepare-remodeling
	// 2 snaps * 3 tasks (from the mock install above) +
	// 1 "set-model" task at the end
	c.Assert(tl, HasLen, 1+2*3+1)

	// validity
	c.Check(tl[1].Kind(), Equals, "fake-download")
	c.Check(tl[1+2*3].Kind(), Equals, "set-model")

	// cleanup
	// fake completion
	for _, t := range tl[1:] {
		t.SetStatus(state.DoneStatus)
	}
	_, ok := devicestate.CachedRemodelCtx(chg)
	c.Check(ok, Equals, true)

	s.state.Unlock()

	s.se.Ensure()
	s.se.Wait()

	s.state.Lock()

	_, ok = devicestate.CachedRemodelCtx(chg)
	c.Check(ok, Equals, false)
}

// TODO: move to preseeding_test.go
type preseedingBaseSuite struct {
	deviceMgrBaseSuite

	cmdUmount    *testutil.MockCmd
	cmdSystemctl *testutil.MockCmd
}

func (s *preseedingBaseSuite) SetUpTest(c *C, preseed, classic bool) {
	// preseed mode helper needs to be mocked before setting up
	// deviceMgrBaseSuite due to device Manager init.
	r := snapdenv.MockPreseeding(preseed)
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)

	// can use cleanup only after having called base SetUpTest
	s.AddCleanup(r)

	s.AddCleanup(interfaces.MockSystemKey(`{"build-id":"abcde"}`))
	c.Assert(interfaces.WriteSystemKey(), IsNil)

	s.cmdUmount = testutil.MockCommand(c, "umount", "")
	s.cmdSystemctl = testutil.MockCommand(c, "systemctl", "")
	s.AddCleanup(func() {
		s.cmdUmount.Restore()
		s.cmdSystemctl.Restore()
	})

	st := s.state
	st.Lock()
	defer st.Unlock()

	si := &snap.SideInfo{RealName: "test-snap", Revision: snap.R(3), SnapID: "test-snap-id"}
	snaptest.MockSnap(c, `name: test-snap
version: 1.0
apps:
 srv:
  command: bin/service
  daemon: simple
`, si)

	snapstate.Set(st, "test-snap", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		SnapType: "app",
	})
}

type preseedingClassicSuite struct {
	preseedingBaseSuite
	now time.Time
}

var _ = Suite(&preseedingClassicSuite{})

func (s *preseedingClassicSuite) SetUpTest(c *C) {
	classic := true
	preseed := true
	s.now = time.Now()
	r := devicestate.MockTimeNow(func() time.Time {
		return s.now
	})
	s.preseedingBaseSuite.SetUpTest(c, preseed, classic)
	// can use cleanup only after having called base SetUpTest
	s.AddCleanup(r)
}

func (s *preseedingClassicSuite) TestDoMarkPreseeded(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("firstboot seeding", "...")
	t := st.NewTask("mark-preseeded", "...")
	chg.AddTask(t)

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	// mark-preseeded task is left in Doing, meaning it will be re-executed
	// after restart in normal (not preseeding) mode.
	c.Check(t.Status(), Equals, state.DoingStatus)

	var preseeded bool
	c.Check(t.Get("preseeded", &preseeded), IsNil)
	c.Check(preseeded, Equals, true)

	c.Assert(st.Get("preseeded", &preseeded), IsNil)
	c.Check(preseeded, Equals, true)

	var systemKey map[string]interface{}
	c.Assert(st.Get("seed-restart-system-key", &systemKey), testutil.ErrorIs, state.ErrNoState)
	c.Assert(st.Get("preseed-system-key", &systemKey), IsNil)
	c.Check(systemKey["build-id"], Equals, "abcde")

	var preseededTime time.Time
	c.Assert(st.Get("preseed-time", &preseededTime), IsNil)
	c.Check(preseededTime.Equal(s.now), Equals, true)

	// core snap was "manually" unmounted
	c.Check(s.cmdUmount.Calls(), DeepEquals, [][]string{
		{"umount", "-d", "-l", filepath.Join(dirs.SnapMountDir, "test-snap/3")},
	})

	// and snapd stop was requested
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.StopDaemon})

	s.cmdUmount.ForgetCalls()

	// re-trying mark-preseeded task has no effect
	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	c.Check(s.cmdUmount.Calls(), HasLen, 0)
	c.Check(t.Status(), Equals, state.DoingStatus)
}

func (s *preseedingClassicSuite) TestEnsureSeededPreseedFlag(c *C) {
	called := false
	restore := devicestate.MockPopulateStateFromSeed(s.mgr, func(sLabel, sMode string, tm timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		return nil, nil
	})
	defer restore()
	mylog.Check(devicestate.EnsureSeeded(s.mgr))

	c.Check(called, Equals, true)

	s.state.Lock()
	defer s.state.Unlock()

	var preseedStartTime time.Time
	c.Assert(s.state.Get("preseed-start-time", &preseedStartTime), IsNil)
	c.Check(preseedStartTime.Equal(s.now), Equals, true)
}

type preseedingClassicDoneSuite struct {
	preseedingBaseSuite
}

var _ = Suite(&preseedingClassicDoneSuite{})

func (s *preseedingClassicDoneSuite) SetUpTest(c *C) {
	classic := true
	preseed := false
	s.preseedingBaseSuite.SetUpTest(c, preseed, classic)
}

func (s *preseedingClassicDoneSuite) TestDoMarkPreseededAfterFirstboot(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("firstboot seeding", "...")
	t := st.NewTask("mark-preseeded", "...")
	chg.AddTask(t)
	t.SetStatus(state.DoingStatus)

	st.Unlock()
	s.se.Ensure()
	s.se.Wait()
	st.Lock()

	// no umount calls expected, just transitioned to Done status.
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(s.cmdUmount.Calls(), HasLen, 0)
	c.Check(s.restartRequests, HasLen, 0)

	var systemKey map[string]interface{}
	// in real world preseed-system-key would be present at this point because
	// mark-preseeded would be run twice (before & after preseeding); this is
	// not the case in this test.
	c.Assert(st.Get("preseed-system-key", &systemKey), testutil.ErrorIs, state.ErrNoState)
	c.Assert(st.Get("seed-restart-system-key", &systemKey), IsNil)
	c.Check(systemKey["build-id"], Equals, "abcde")

	var seedRestartTime time.Time
	c.Assert(st.Get("seed-restart-time", &seedRestartTime), IsNil)
	c.Check(seedRestartTime.Equal(devicestate.StartTime()), Equals, true)
}

type preseedingUC20Suite struct {
	preseedingBaseSuite
	*seedtest.TestingSeed20
}

var _ = Suite(&preseedingUC20Suite{})

func (s *preseedingUC20Suite) SetUpTest(c *C) {
	// mock a system for preseeding to make deviceMgr happy on init; it is
	// intentionally restored inside SetUpTest. Tests use real systemForPreseeding
	// and mock system label via filesystem where needed.
	restore := devicestate.MockSystemForPreseeding(func() (string, error) {
		return "fake system label", nil
	})
	defer restore()

	preseed := true
	classic := false
	s.preseedingBaseSuite.SetUpTest(c, preseed, classic)

	s.TestingSeed20 = &seedtest.TestingSeed20{}
	s.SeedDir = dirs.SnapSeedDir
}

func (s *preseedingUC20Suite) setupCore20Seed(c *C, sysLabel string) *asserts.Model {
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
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["snapd"], nil, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["pc-kernel=20"], nil, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["core20"], nil, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["pc=20"], [][]string{{"meta/gadget.yaml", gadgetYaml}}, snap.R(1), "canonical", s.StoreSigning.Database)

	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   s.AssertedSnapID("snapd"),
				"type": "snapd",
			},
			map[string]interface{}{
				"name": "core20",
				"id":   s.AssertedSnapID("core20"),
				"type": "base",
			},
		},
	}

	return s.MakeSeed(c, sysLabel, "my-brand", "my-model", model, nil)
}

func (s *preseedingUC20Suite) TestEarlyPreloadGadgetPicksSystemOnCore20(c *C) {
	// validity
	c.Assert(snapdenv.Preseeding(), Equals, true)
	c.Assert(release.OnClassic, Equals, false)

	var readSysLabel string
	restore := devicestate.MockLoadDeviceSeed(func(st *state.State, sysLabel string) (seed.Seed, error) {
		readSysLabel = sysLabel
		// inject an error, we are only interested in verification of the syslabel
		return nil, fmt.Errorf("boom")
	})
	defer restore()

	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	_ = s.setupCore20Seed(c, "20220108")

	mgr := mylog.Check2(devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore))


	s.state.Lock()
	defer s.state.Unlock()

	_, _ = mylog.Check3(devicestate.EarlyPreloadGadget(mgr))
	// error from mocked loadDeviceSeed results in ErrNoState from preloadGadget
	c.Assert(err, testutil.ErrorIs, state.ErrNoState)
	c.Check(readSysLabel, Equals, "20220108")
}

func (s *preseedingUC20Suite) TestEnsureSeededPicksSystemOnCore20(c *C) {
	// validity
	c.Assert(snapdenv.Preseeding(), Equals, true)
	c.Assert(release.OnClassic, Equals, false)

	called := false

	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "systems", "20220105"), 0755), IsNil)

	mgr := mylog.Check2(devicestate.Manager(s.state, s.hookMgr, s.o.TaskRunner(), s.newStore))

	restore := devicestate.MockPopulateStateFromSeed(mgr, func(sLabel, sMode string, tm timings.Measurer) ([]*state.TaskSet, error) {
		called = true
		c.Check(sLabel, Equals, "20220105")
		c.Check(sMode, Equals, "run")
		return nil, nil
	})
	defer restore()
	mylog.Check(devicestate.EnsureSeeded(mgr))

	c.Check(called, Equals, true)
}

func (s *preseedingUC20Suite) TestSysModeIsRunWhenPreseeding(c *C) {
	// validity
	c.Assert(snapdenv.Preseeding(), Equals, true)
	c.Assert(release.OnClassic, Equals, false)

	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "systems", "20220105"), 0755), IsNil)

	runner := state.NewTaskRunner(s.state)
	mgr := mylog.Check2(devicestate.Manager(s.state, s.hookMgr, runner, nil))

	c.Check(devicestate.GetSystemMode(mgr), Equals, "run")
}

func (s *preseedingUC20Suite) TestSystemForPreseeding(c *C) {
	_ := mylog.Check2(devicestate.SystemForPreseeding())
	c.Assert(err, ErrorMatches, `no system to preseed`)

	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "systems", "20220105"), 0755), IsNil)
	systemLabel := mylog.Check2(devicestate.SystemForPreseeding())

	c.Check(systemLabel, Equals, "20220105")

	c.Assert(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "systems", "20210201"), 0755), IsNil)
	_ = mylog.Check2(devicestate.SystemForPreseeding())
	c.Assert(err, ErrorMatches, `expected a single system for preseeding, found 2`)
}
