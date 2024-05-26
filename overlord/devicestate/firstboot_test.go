// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type firstBootBaseTest struct {
	testutil.BaseTest

	systemctl *testutil.MockCmd

	devAcct *asserts.Account

	overlord *overlord.Overlord

	perfTimings timings.Measurer
}

func (t *firstBootBaseTest) setupBaseTest(c *C, s *seedtest.SeedSnaps) {
	t.BaseTest.SetUpTest(c)

	// TODO: temporary: skip due to timeouts on riscv64
	if runtime.GOARCH == "riscv64" || os.Getenv("SNAPD_SKIP_SLOW_TESTS") != "" {
		c.Skip("skipping slow test")
	}

	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)
	t.AddCleanup(func() { dirs.SetRootDir("/") })

	t.AddCleanup(release.MockOnClassic(false))

	restore := osutil.MockMountInfo("")
	t.AddCleanup(restore)
	mylog.

		// mock the world!
		Check(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "snaps"), 0755))

	mylog.Check(os.MkdirAll(dirs.SnapServicesDir, 0755))

	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	t.AddCleanup(func() { os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS") })
	t.systemctl = testutil.MockCommand(c, "systemctl", "")
	t.AddCleanup(t.systemctl.Restore)

	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})

	t.devAcct = assertstest.NewAccount(s.StoreSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")

	t.AddCleanup(sysdb.InjectTrusted([]asserts.Assertion{s.StoreSigning.TrustedKey}))
	t.AddCleanup(ifacestate.MockSecurityBackends(nil))

	t.perfTimings = timings.New(nil)

	r := devicestate.MockCloudInitStatus(func() (sysconfig.CloudInitState, error) {
		return sysconfig.CloudInitRestrictedBySnapd, nil
	})
	t.AddCleanup(r)
}

// startOverlord will setup and create a new overlord, note that it will not
// stop any pre-existing overlord and it will be overwritten, for more fine
// control create your own overlord
// also note that as long as you don't run overlord.Loop() this is safe to call
// multiple times to clear overlord state, if you call Loop() call Stop() on
// your own before calling this again
func (t *firstBootBaseTest) startOverlord(c *C) {
	ovld := mylog.Check2(overlord.New(nil))

	ovld.InterfaceManager().DisableUDevMonitor()
	// avoid gadget preload in the general tests cases
	// it requires a proper seed to be available
	devicestate.EarlyConfig = func(st *state.State, preloadGadget func() (sysconfig.Device, *gadget.Info, error)) error {
		return nil
	}
	t.overlord = ovld
	t.AddCleanup(func() {
		devicestate.EarlyConfig = nil
		if t.overlord == nil {
			return
		}
		t.overlord.Stop()
		t.overlord = nil
	})
	c.Assert(ovld.StartUp(), IsNil)

	// don't actually try to talk to the store on snapstate.Ensure
	// needs doing after the call to devicestate.Manager (which happens in
	// overlord.New)
	snapstate.CanAutoRefresh = nil
}

type firstBoot16BaseTest struct {
	*firstBootBaseTest
	// TestingSeed16 helps populating seeds (it provides
	// MakeAssertedSnap, WriteAssertions etc.) for tests.
	*seedtest.TestingSeed16
}

func (t *firstBoot16BaseTest) setup16BaseTest(c *C, bt *firstBootBaseTest) {
	t.firstBootBaseTest = bt
	t.setupBaseTest(c, &t.TestingSeed16.SeedSnaps)
}

type firstBoot16Suite struct {
	firstBootBaseTest
	firstBoot16BaseTest
}

var _ = Suite(&firstBoot16Suite{})

func (s *firstBoot16Suite) SetUpTest(c *C) {
	s.TestingSeed16 = &seedtest.TestingSeed16{}
	s.setup16BaseTest(c, &s.firstBootBaseTest)

	s.SeedDir = dirs.SnapSeedDir
	mylog.Check(os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "assertions"), 0755))


	// mock the snap mapper as core here to make sure that other tests don't
	// set it inadvertently to the snapd mapper and break the 16 tests
	s.AddCleanup(ifacestate.MockSnapMapper(&ifacestate.CoreCoreSystemMapper{}))
}

func checkTrivialSeeding(c *C, tsAll []*state.TaskSet) {
	// run internal core config and  mark seeded
	c.Check(tsAll, HasLen, 2)
	tasks := tsAll[0].Tasks()
	c.Check(tasks, HasLen, 1)
	c.Assert(tasks[0].Kind(), Equals, "run-hook")
	var hooksup hookstate.HookSetup
	mylog.Check(tasks[0].Get("hook-setup", &hooksup))

	c.Check(hooksup.Hook, Equals, "configure")
	c.Check(hooksup.Snap, Equals, "core")
	tasks = tsAll[1].Tasks()
	c.Check(tasks, HasLen, 1)
	c.Check(tasks[0].Kind(), Equals, "mark-seeded")
}

func modelHeaders(modelStr string, reqSnaps ...string) map[string]interface{} {
	headers := map[string]interface{}{
		"architecture": "amd64",
		"store":        "canonical",
	}
	if strings.HasSuffix(modelStr, "-classic") {
		headers["classic"] = "true"
	} else if strings.HasSuffix(modelStr, "-classic-modes") {
		headers["classic"] = "true"
		headers["distribution"] = "ubuntu"
		headers["base"] = "core22"
		headers["snaps"] = []interface{}{
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
		}
	} else {
		headers["kernel"] = "pc-kernel"
		headers["gadget"] = "pc"
	}
	if len(reqSnaps) != 0 {
		reqs := make([]interface{}, len(reqSnaps))
		for i, req := range reqSnaps {
			reqs[i] = req
		}
		headers["required-snaps"] = reqs
	}
	return headers
}

func (s *firstBoot16BaseTest) makeModelAssertionChain(c *C, modName string, extraHeaders map[string]interface{}, reqSnaps ...string) []asserts.Assertion {
	return s.MakeModelAssertionChain("my-brand", modName, modelHeaders(modName, reqSnaps...), extraHeaders)
}

func (s *firstBoot16Suite) TestPopulateFromSeedOnClassicNoop(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()
	mylog.Check(os.Remove(filepath.Join(dirs.SnapSeedDir, "assertions")))


	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	mgr := s.overlord.DeviceManager()
	_, _ = mylog.Check3(devicestate.EarlyPreloadGadget(mgr))
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(mgr, s.perfTimings))

	checkTrivialSeeding(c, tsAll)

	// already set the fallback model

	// verify that the model was added
	db := assertstate.DB(st)
	as := mylog.Check2(db.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "generic",
		"model":    "generic-classic",
	}))

	_, ok := as.(*asserts.Model)
	c.Check(ok, Equals, true)

	ds := mylog.Check2(devicestatetest.Device(st))

	c.Check(ds.Brand, Equals, "generic")
	c.Check(ds.Model, Equals, "generic-classic")
}

func (s *firstBoot16Suite) TestPopulateFromSeedOnClassicNoSeedYaml(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	ovld := mylog.Check2(overlord.New(nil))
	defer ovld.Stop()

	st := ovld.State()

	// add the model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	st.Lock()
	defer st.Unlock()

	mgr := ovld.DeviceManager()
	_, _ = mylog.Check3(devicestate.EarlyPreloadGadget(mgr))
	c.Check(err, testutil.ErrorIs, state.ErrNoState)

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(mgr, s.perfTimings))

	checkTrivialSeeding(c, tsAll)

	ds := mylog.Check2(devicestatetest.Device(st))

	c.Check(ds.Brand, Equals, "my-brand")
	c.Check(ds.Model, Equals, "my-model-classic")
}

func (s *firstBoot16Suite) TestPopulateFromSeedOnClassicEmptySeedYaml(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	ovld := mylog.Check2(overlord.New(nil))
	defer ovld.Stop()

	st := ovld.State()

	// add the model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)
	mylog.

		// create an empty seed.yaml
		Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), nil, 0644))


	st.Lock()
	defer st.Unlock()

	_ = mylog.Check2(devicestate.PopulateStateFromSeedImpl(ovld.DeviceManager(), s.perfTimings))
	c.Assert(err, ErrorMatches, "cannot proceed, no snaps to seed")
	st.Unlock()
	st.Lock()
	// note, cannot use st.Tasks() here as it filters out tasks with no change
	c.Check(st.TaskCount(), Equals, 0)
}

func (s *firstBoot16Suite) TestPopulateFromSeedOnClassicNoSeedYamlWithCloudInstanceData(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// add the model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	s.startOverlord(c)
	st := s.overlord.State()

	// write cloud instance data
	const instData = `{
 "v1": {
  "availability-zone": "us-east-2b",
  "cloud-name": "aws",
  "instance-id": "i-03bdbe0d89f4c8ec9",
  "local-hostname": "ip-10-41-41-143",
  "region": "us-east-2"
 }
}`
	mylog.Check(os.MkdirAll(filepath.Dir(dirs.CloudInstanceDataFile), 0755))

	mylog.Check(os.WriteFile(dirs.CloudInstanceDataFile, []byte(instData), 0600))


	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))

	checkTrivialSeeding(c, tsAll)

	ds := mylog.Check2(devicestatetest.Device(st))

	c.Check(ds.Brand, Equals, "my-brand")
	c.Check(ds.Model, Equals, "my-model-classic")

	// now run the change and check the result
	// use the expected kind otherwise settle will start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()
	c.Assert(chg.Err(), IsNil)


	// check marked seeded
	var seeded bool
	mylog.Check(st.Get("seeded", &seeded))

	c.Check(seeded, Equals, true)

	// check captured cloud information
	tr := config.NewTransaction(st)
	var cloud auth.CloudInfo
	mylog.Check(tr.Get("core", "cloud", &cloud))

	c.Check(cloud.Name, Equals, "aws")
	c.Check(cloud.Region, Equals, "us-east-2")
	c.Check(cloud.AvailabilityZone, Equals, "us-east-2b")
}

func (s *firstBoot16Suite) TestPopulateFromSeedErrorsOnState(c *C) {
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	st.Set("seeded", true)

	_ := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))
	c.Assert(err, ErrorMatches, "cannot populate state: already seeded")
	// note, cannot use st.Tasks() here as it filters out tasks with no change
	c.Check(st.TaskCount(), Equals, 0)
}

func (s *firstBoot16BaseTest) makeCoreSnaps(c *C, extraGadgetYaml string) (coreFname, kernelFname, gadgetFname string) {
	files := [][]string{}
	if strings.Contains(extraGadgetYaml, "defaults:") {
		files = [][]string{{"meta/hooks/configure", ""}}
	}

	// put core snap into the SnapBlobDir
	snapYaml := `name: core
version: 1.0
type: os`
	coreFname, coreDecl, coreRev := s.MakeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")
	s.WriteAssertions("core.asserts", coreRev, coreDecl)

	// put kernel snap into the SnapBlobDir
	snapYaml = `name: pc-kernel
version: 1.0
type: kernel`
	kernelFname, kernelDecl, kernelRev := s.MakeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")
	s.WriteAssertions("kernel.asserts", kernelRev, kernelDecl)

	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
	gadgetYaml += extraGadgetYaml

	// put gadget snap into the SnapBlobDir
	files = append(files, []string{"meta/gadget.yaml", gadgetYaml})

	snapYaml = `name: pc
version: 1.0
type: gadget`
	gadgetFname, gadgetDecl, gadgetRev := s.MakeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")
	s.WriteAssertions("gadget.asserts", gadgetRev, gadgetDecl)

	return coreFname, kernelFname, gadgetFname
}

func checkOrder(c *C, tsAll []*state.TaskSet, snaps ...string) {
	matched := 0
	var prevTask *state.Task
	for i, ts := range tsAll {
		task0 := ts.Tasks()[0]
		waitTasks := task0.WaitTasks()
		if i == 0 {
			c.Check(waitTasks, HasLen, 0)
		} else {
			c.Check(waitTasks, testutil.Contains, prevTask)
		}
		prevTask = task0
		if task0.Kind() != "prerequisites" {
			continue
		}
		snapsup := mylog.Check2(snapstate.TaskSnapSetup(task0))
		c.Assert(err, IsNil, Commentf("%#v", task0))
		c.Check(snapsup.InstanceName(), Equals, snaps[matched])
		matched++
	}
	c.Check(matched, Equals, len(snaps))
}

func checkSeedTasks(c *C, tsAll []*state.TaskSet) {
	// the last taskset is just mark-seeded
	lastTasks := tsAll[len(tsAll)-1].Tasks()
	c.Check(lastTasks, HasLen, 1)
	markSeededTask := lastTasks[0]
	c.Check(markSeededTask.Kind(), Equals, "mark-seeded")
	// and mark-seeded must wait for the other tasks
	prevTasks := tsAll[len(tsAll)-2].Tasks()
	otherTask := prevTasks[len(prevTasks)-1]
	c.Check(markSeededTask.WaitTasks(), testutil.Contains, otherTask)
}

func (s *firstBoot16BaseTest) makeSeedChange(c *C, st *state.State,
	checkTasks func(c *C, tsAll []*state.TaskSet), checkOrder func(c *C, tsAll []*state.TaskSet, snaps ...string),
) (*state.Change, *asserts.Model) {
	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, "")

	s.WriteAssertions("developer.account", s.devAcct)

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("foo.snap-declaration", fooDecl)
	s.WriteAssertions("foo.snap-revision", fooRev)

	// put a firstboot local snap into the SnapBlobDir
	snapYaml = `name: local
version: 1.0`
	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)
	targetSnapFile2 := filepath.Join(dirs.SnapSeedDir, "snaps", filepath.Base(mockSnapFile))
	mylog.Check(os.Rename(mockSnapFile, targetSnapFile2))


	// add a model assertion and its chain
	var model *asserts.Model = nil
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil, "foo")
	for i, as := range assertsChain {
		if as.Type() == asserts.ModelType {
			model = as.(*asserts.Model)
		}
		s.WriteAssertions(strconv.Itoa(i), as)
	}
	c.Assert(model, NotNil)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: core
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: foo
   file: %s
   devmode: true
   contact: mailto:some.guy@example.com
 - name: local
   unasserted: true
   file: %s
`, coreFname, kernelFname, gadgetFname, fooFname, filepath.Base(targetSnapFile2)))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	if st == nil {
		s.startOverlord(c)
		st = s.overlord.State()
	}

	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	// now run the change and check the result
	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	checkOrder(c, tsAll, "core", "pc-kernel", "pc", "foo", "local")
	checkTasks(c, tsAll)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	return chg, model
}

func (s *firstBoot16Suite) TestPopulateFromSeedHappy(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	chg, model := s.makeSeedChange(c, nil, checkSeedTasks, checkOrder)
	mylog.Check(s.overlord.Settle(settleTimeout))


	st := chg.State()
	st.Lock()
	defer st.Unlock()

	c.Assert(chg.Err(), IsNil)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "local", "x1", "meta", "snap.yaml")), Equals, true)

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	state := mylog.Check2(state.ReadState(nil, r))


	state.Lock()
	defer state.Unlock()
	// check core, kernel, gadget
	_ = mylog.Check2(snapstate.CurrentInfo(state, "core"))

	_ = mylog.Check2(snapstate.CurrentInfo(state, "pc-kernel"))

	_ = mylog.Check2(snapstate.CurrentInfo(state, "pc"))


	// ensure required flag is set on all essential snaps
	var snapst snapstate.SnapState
	for _, reqName := range []string{"core", "pc-kernel", "pc"} {
		mylog.Check(snapstate.Get(state, reqName, &snapst))

		c.Assert(snapst.Required, Equals, true, Commentf("required not set for %v", reqName))
	}

	// check foo
	info := mylog.Check2(snapstate.CurrentInfo(state, "foo"))

	c.Assert(info.SnapID, Equals, "foodidididididididididididididid")
	c.Assert(info.Revision, Equals, snap.R(128))
	c.Assert(info.Contact(), Equals, "mailto:some.guy@example.com")
	pubAcct := mylog.Check2(assertstate.Publisher(st, info.SnapID))

	c.Check(pubAcct.AccountID(), Equals, "developerid")
	mylog.Check(snapstate.Get(state, "foo", &snapst))

	c.Assert(snapst.DevMode, Equals, true)
	c.Assert(snapst.Required, Equals, true)

	// check local
	info = mylog.Check2(snapstate.CurrentInfo(state, "local"))

	c.Assert(info.SnapID, Equals, "")
	c.Assert(info.Revision, Equals, snap.R("x1"))

	var snapst2 snapstate.SnapState
	mylog.Check(snapstate.Get(state, "local", &snapst2))

	c.Assert(snapst2.Required, Equals, false)

	// and ensure state is now considered seeded
	var seeded bool
	mylog.Check(state.Get("seeded", &seeded))

	c.Check(seeded, Equals, true)

	// check we set seed-time
	var seedTime time.Time
	mylog.Check(state.Get("seed-time", &seedTime))

	c.Check(seedTime.IsZero(), Equals, false)

	var whatseeded []devicestate.SeededSystem
	mylog.Check(state.Get("seeded-systems", &whatseeded))

	c.Assert(whatseeded, DeepEquals, []devicestate.SeededSystem{{
		System:    "",
		Model:     "my-model",
		BrandID:   "my-brand",
		Revision:  model.Revision(),
		Timestamp: model.Timestamp(),
		SeedTime:  seedTime,
	}})
}

func (s *firstBoot16Suite) TestPopulateFromSeedMissingBootloader(c *C) {
	s.startOverlord(c)
	st0 := s.overlord.State()
	st0.Lock()
	db := assertstate.DB(st0)
	st0.Unlock()

	// we run only with the relevant managers to produce the error
	// situation
	o := overlord.Mock()
	st := o.State()
	snapmgr := mylog.Check2(snapstate.Manager(st, o.TaskRunner()))

	o.AddManager(snapmgr)

	ifacemgr := mylog.Check2(ifacestate.Manager(st, nil, o.TaskRunner(), nil, nil))

	o.AddManager(ifacemgr)
	c.Assert(o.StartUp(), IsNil)

	hookMgr := mylog.Check2(hookstate.Manager(st, o.TaskRunner()))

	deviceMgr := mylog.Check2(devicestate.Manager(st, hookMgr, o.TaskRunner(), nil))

	o.AddManager(deviceMgr)

	st.Lock()
	assertstate.ReplaceDB(st, db.(*asserts.Database))
	st.Unlock()

	o.AddManager(o.TaskRunner())

	s.overlord = o

	chg, _ := s.makeSeedChange(c, st, checkSeedTasks, checkOrder)

	se := o.StateEngine()
	// we cannot use Settle because the Change will not become Clean
	// under the subset of managers
	for i := 0; i < 25 && !chg.IsReady(); i++ {
		se.Ensure()
		se.Wait()
	}

	st.Lock()
	defer st.Unlock()
	c.Assert(chg.Err(), ErrorMatches, `(?s).* cannot determine bootloader.*`)
}

func (s *firstBoot16Suite) TestPopulateFromSeedHappyMultiAssertsFiles(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, "")

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("foo.asserts", s.devAcct, fooRev, fooDecl)

	// put a 2nd firstboot snap into the SnapBlobDir
	snapYaml = `name: bar
version: 1.0`
	barFname, barDecl, barRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(65), "developerid")
	s.WriteAssertions("bar.asserts", s.devAcct, barDecl, barRev)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: core
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: foo
   file: %s
 - name: bar
   file: %s
`, coreFname, kernelFname, gadgetFname, fooFname, barFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))

	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()
	c.Assert(chg.Err(), IsNil)


	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "bar", "65", "meta", "snap.yaml")), Equals, true)

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	state := mylog.Check2(state.ReadState(nil, r))


	state.Lock()
	defer state.Unlock()
	// check foo
	info := mylog.Check2(snapstate.CurrentInfo(state, "foo"))

	c.Check(info.SnapID, Equals, "foodidididididididididididididid")
	c.Check(info.Revision, Equals, snap.R(128))
	pubAcct := mylog.Check2(assertstate.Publisher(st, info.SnapID))

	c.Check(pubAcct.AccountID(), Equals, "developerid")

	// check bar
	info = mylog.Check2(snapstate.CurrentInfo(state, "bar"))

	c.Check(info.SnapID, Equals, "bardidididididididididididididid")
	c.Check(info.Revision, Equals, snap.R(65))
	pubAcct = mylog.Check2(assertstate.Publisher(st, info.SnapID))

	c.Check(pubAcct.AccountID(), Equals, "developerid")
}

func (s *firstBoot16Suite) TestPopulateFromSeedConfigureHappy(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	const defaultsYaml = `
defaults:
    foodidididididididididididididid:
       foo-cfg: foo.
    99T7MUlRhtI3U0QFgl5mXXESAiSwt776:  # core
       core-cfg: core_cfg_defl
    pckernelidididididididididididid:
       pc-kernel-cfg: pc-kernel_cfg_defl
    pcididididididididididididididid:
       pc-cfg: pc_cfg_defl
`
	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, defaultsYaml)

	s.WriteAssertions("developer.account", s.devAcct)

	// put a firstboot snap into the SnapBlobDir
	files := [][]string{{"meta/hooks/configure", ""}}
	snapYaml := `name: foo
version: 1.0`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, files, snap.R(128), "developerid")
	s.WriteAssertions("foo.asserts", fooDecl, fooRev)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil, "foo")
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: core
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: foo
   file: %s
`, coreFname, kernelFname, gadgetFname, fooFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	mgr := s.overlord.DeviceManager()
	dev, gi := mylog.Check3(devicestate.EarlyPreloadGadget(mgr))

	c.Check(gi.Defaults, HasLen, 4)
	c.Check(dev.RunMode(), Equals, true)
	c.Check(dev.Classic(), Equals, false)
	c.Check(dev.HasModeenv(), Equals, false)
	c.Check(dev.Kernel(), Equals, "pc-kernel")

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(mgr, s.perfTimings))


	checkSeedTasks(c, tsAll)

	// now run the change and check the result
	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	var configured []string
	hookInvoke := func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()
		// we have a gadget at this point(s)
		ok := mylog.Check2(snapstate.HasSnapOfType(st, snap.TypeGadget))
		c.Check(err, IsNil)
		c.Check(ok, Equals, true)
		configured = append(configured, ctx.InstanceName())
		return nil, nil
	}

	rhk := hookstate.MockRunHook(hookInvoke)
	defer rhk()

	// ensure we have something that captures the core config
	restore := configstate.MockConfigcoreRun(func(sysconfig.Device, configcore.RunTransaction) error {
		configured = append(configured, "configcore")
		return nil
	})
	defer restore()

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()
	c.Assert(chg.Err(), IsNil)


	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	state := mylog.Check2(state.ReadState(nil, r))


	state.Lock()
	defer state.Unlock()
	tr := config.NewTransaction(state)
	var val string

	// check core, kernel, gadget
	_ = mylog.Check2(snapstate.CurrentInfo(state, "core"))

	mylog.Check(tr.Get("core", "core-cfg", &val))

	c.Check(val, Equals, "core_cfg_defl")

	_ = mylog.Check2(snapstate.CurrentInfo(state, "pc-kernel"))

	mylog.Check(tr.Get("pc-kernel", "pc-kernel-cfg", &val))

	c.Check(val, Equals, "pc-kernel_cfg_defl")

	_ = mylog.Check2(snapstate.CurrentInfo(state, "pc"))

	mylog.Check(tr.Get("pc", "pc-cfg", &val))

	c.Check(val, Equals, "pc_cfg_defl")

	// check foo
	info := mylog.Check2(snapstate.CurrentInfo(state, "foo"))

	c.Assert(info.SnapID, Equals, "foodidididididididididididididid")
	c.Assert(info.Revision, Equals, snap.R(128))
	pubAcct := mylog.Check2(assertstate.Publisher(st, info.SnapID))

	c.Check(pubAcct.AccountID(), Equals, "developerid")
	mylog.

		// check foo config
		Check(tr.Get("foo", "foo-cfg", &val))

	c.Check(val, Equals, "foo.")

	c.Check(configured, DeepEquals, []string{"configcore", "pc-kernel", "pc", "foo"})

	// and ensure state is now considered seeded
	var seeded bool
	mylog.Check(state.Get("seeded", &seeded))

	c.Check(seeded, Equals, true)
}

func (s *firstBoot16Suite) TestPopulateFromSeedGadgetConnectHappy(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	const connectionsYaml = `
connections:
  - plug: foodidididididididididididididid:network-control
`
	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, connectionsYaml)

	s.WriteAssertions("developer.account", s.devAcct)

	snapYaml := `name: foo
version: 1.0
plugs:
  network-control:
`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("foo.asserts", fooDecl, fooRev)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil, "foo")
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: core
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: foo
   file: %s
`, coreFname, kernelFname, gadgetFname, fooFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	checkSeedTasks(c, tsAll)

	// now run the change and check the result
	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()
	c.Assert(chg.Err(), IsNil)


	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	state := mylog.Check2(state.ReadState(nil, r))


	state.Lock()
	defer state.Unlock()

	// check foo
	info := mylog.Check2(snapstate.CurrentInfo(state, "foo"))

	c.Assert(info.SnapID, Equals, "foodidididididididididididididid")
	c.Assert(info.Revision, Equals, snap.R(128))
	pubAcct := mylog.Check2(assertstate.Publisher(st, info.SnapID))

	c.Check(pubAcct.AccountID(), Equals, "developerid")

	// check connection
	var conns map[string]interface{}
	mylog.Check(state.Get("conns", &conns))

	c.Check(conns, HasLen, 1)
	c.Check(conns, DeepEquals, map[string]interface{}{
		"foo:network-control core:network-control": map[string]interface{}{
			"interface": "network-control", "auto": true, "by-gadget": true,
		},
	})

	// and ensure state is now considered seeded
	var seeded bool
	mylog.Check(state.Get("seeded", &seeded))

	c.Check(seeded, Equals, true)
}

func (s *firstBoot16Suite) TestImportAssertionsFromSeedClassicModelMismatch(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	ovld := mylog.Check2(overlord.New(nil))
	defer ovld.Stop()

	st := ovld.State()

	// add the odel assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// import them
	st.Lock()
	defer st.Unlock()

	isCoreBoot := true
	_ = mylog.Check2(devicestate.ImportAssertionsFromSeed(ovld.DeviceManager(), isCoreBoot))
	c.Assert(err, ErrorMatches, "cannot seed a classic system with an all-snaps model")
}

func (s *firstBoot16Suite) TestImportAssertionsFromSeedClassicWithModes(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	ovld := mylog.Check2(overlord.New(nil))
	defer ovld.Stop()

	st := ovld.State()

	// add the model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic-modes", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// import them
	st.Lock()
	defer st.Unlock()

	isCoreBoot := true
	_ = mylog.Check2(devicestate.ImportAssertionsFromSeed(ovld.DeviceManager(), isCoreBoot))

}

func (s *firstBoot16Suite) TestImportAssertionsFromSeedAllSnapsModelMismatch(c *C) {
	ovld := mylog.Check2(overlord.New(nil))
	defer ovld.Stop()

	st := ovld.State()

	// add the model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// import them
	st.Lock()
	defer st.Unlock()

	isCoreBoot := true
	_ = mylog.Check2(devicestate.ImportAssertionsFromSeed(ovld.DeviceManager(), isCoreBoot))
	c.Assert(err, ErrorMatches, "cannot seed an all-snaps system with a classic model")
}

func (s *firstBoot16Suite) TestLoadDeviceSeed(c *C) {
	ovld := mylog.Check2(overlord.New(nil))
	defer ovld.Stop()

	st := ovld.State()

	// add a bunch of assertions (model assertion and its chain)
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	for i, as := range assertsChain {
		fname := strconv.Itoa(i)
		if as.Type() == asserts.ModelType {
			fname = "model"
		}
		s.WriteAssertions(fname, as)
	}

	// load them
	st.Lock()
	defer st.Unlock()

	deviceSeed := mylog.Check2(devicestate.LoadDeviceSeed(st, ""))


	c.Check(deviceSeed.Model().BrandID(), Equals, "my-brand")
	c.Check(deviceSeed.Model().Model(), Equals, "my-model")

	// verify that the model was added
	db := assertstate.DB(st)
	as := mylog.Check2(db.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "my-brand",
		"model":    "my-model",
	}))

	c.Check(as, DeepEquals, deviceSeed.Model())
}

func (s *firstBoot16Suite) TestImportAssertionsFromSeedHappy(c *C) {
	ovld := mylog.Check2(overlord.New(nil))
	defer ovld.Stop()

	st := ovld.State()

	// add a bunch of assertions (model assertion and its chain)
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	for i, as := range assertsChain {
		fname := strconv.Itoa(i)
		if as.Type() == asserts.ModelType {
			fname = "model"
		}
		s.WriteAssertions(fname, as)
	}

	// import them
	st.Lock()
	defer st.Unlock()

	isCoreBoot := true
	deviceSeed := mylog.Check2(devicestate.ImportAssertionsFromSeed(ovld.DeviceManager(), isCoreBoot))

	c.Assert(deviceSeed, NotNil)

	model := deviceSeed.Model()

	// verify that the model was added
	db := assertstate.DB(st)
	as := mylog.Check2(db.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "my-brand",
		"model":    "my-model",
	}))

	_, ok := as.(*asserts.Model)
	c.Check(ok, Equals, true)

	ds := mylog.Check2(devicestatetest.Device(st))

	c.Check(ds.Brand, Equals, "my-brand")
	c.Check(ds.Model, Equals, "my-model")

	c.Check(model.BrandID(), Equals, "my-brand")
	c.Check(model.Model(), Equals, "my-model")
}

func (s *firstBoot16Suite) TestImportAssertionsFromSeedMissingSig(c *C) {
	// write out only the model assertion
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	for _, as := range assertsChain {
		if as.Type() == asserts.ModelType {
			s.WriteAssertions("model", as)
			break
		}
	}

	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	// try import and verify that its rejects because other assertions are
	// missing
	isCoreBoot := true
	_ := mylog.Check2(devicestate.ImportAssertionsFromSeed(s.overlord.DeviceManager(), isCoreBoot))
	c.Assert(err, ErrorMatches, "cannot resolve prerequisite assertion: account-key .*")
}

func (s *firstBoot16Suite) TestImportAssertionsFromSeedTwoModelAsserts(c *C) {
	// write out two model assertions
	model := s.Brands.Model("my-brand", "my-model", modelHeaders("my-model"))
	s.WriteAssertions("model", model)

	model2 := s.Brands.Model("my-brand", "my-second-model", modelHeaders("my-second-model"))
	s.WriteAssertions("model2", model2)

	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	// try import and verify that its rejects because other assertions are
	// missing
	isCoreBoot := true
	_ := mylog.Check2(devicestate.ImportAssertionsFromSeed(s.overlord.DeviceManager(), isCoreBoot))
	c.Assert(err, ErrorMatches, "cannot have multiple model assertions in seed")
}

func (s *firstBoot16Suite) TestImportAssertionsFromSeedNoModelAsserts(c *C) {
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	for i, as := range assertsChain {
		if as.Type() != asserts.ModelType {
			s.WriteAssertions(strconv.Itoa(i), as)
		}
	}

	// try import and verify that its rejects because other assertions are
	// missing
	isCoreBoot := true
	_ := mylog.Check2(devicestate.ImportAssertionsFromSeed(s.overlord.DeviceManager(), isCoreBoot))
	c.Assert(err, ErrorMatches, "seed must have a model assertion")
}

type core18SnapsOpts struct {
	classic bool
	gadget  bool
}

func (s *firstBoot16BaseTest) makeCore18Snaps(c *C, opts *core18SnapsOpts) (core18Fn, snapdFn, kernelFn, gadgetFn string) {
	if opts == nil {
		opts = &core18SnapsOpts{}
	}

	files := [][]string{}

	core18Yaml := `name: core18
version: 1.0
type: base`
	core18Fname, core18Decl, core18Rev := s.MakeAssertedSnap(c, core18Yaml, files, snap.R(1), "canonical")
	s.WriteAssertions("core18.asserts", core18Rev, core18Decl)

	snapdYaml := `name: snapd
version: 1.0
`
	// the info file is needed by the Ensure() loop of snapstate manager
	snapdSnapFiles := [][]string{
		{"usr/lib/snapd/info", `
VERSION=2.54.3+git1.g479e745-dirty
SNAPD_APPARMOR_REEXEC=1
`},
	}
	snapdFname, snapdDecl, snapdRev := s.MakeAssertedSnap(c, snapdYaml, snapdSnapFiles, snap.R(2), "canonical")
	s.WriteAssertions("snapd.asserts", snapdRev, snapdDecl)

	var kernelFname string
	if !opts.classic {
		kernelYaml := `name: pc-kernel
version: 1.0
type: kernel`
		fname, kernelDecl, kernelRev := s.MakeAssertedSnap(c, kernelYaml, files, snap.R(1), "canonical")
		s.WriteAssertions("kernel.asserts", kernelRev, kernelDecl)
		kernelFname = fname
	}

	if !opts.classic {
		gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
		files = append(files, []string{"meta/gadget.yaml", gadgetYaml})
	}

	var gadgetFname string
	if !opts.classic || opts.gadget {
		gaYaml := `name: pc
version: 1.0
type: gadget
base: core18
`
		fname, gadgetDecl, gadgetRev := s.MakeAssertedSnap(c, gaYaml, files, snap.R(1), "canonical")
		s.WriteAssertions("gadget.asserts", gadgetRev, gadgetDecl)
		gadgetFname = fname
	}

	return core18Fname, snapdFname, kernelFname, gadgetFname
}

func (s *firstBoot16Suite) TestPopulateFromSeedWithBaseHappy(c *C) {
	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core18_1.snap")

	core18Fname, snapdFname, kernelFname, gadgetFname := s.makeCore18Snaps(c, nil)

	s.WriteAssertions("developer.account", s.devAcct)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", map[string]interface{}{"base": "core18"})
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: core18
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
`, snapdFname, core18Fname, kernelFname, gadgetFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	checkOrder(c, tsAll, "snapd", "pc-kernel", "core18", "pc")

	// now run the change and check the result
	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	c.Assert(chg.Err(), IsNil)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	// run change until it wants to restart
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()


	// at this point the system is "restarting", pretend the restart has
	// happened
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	restart.MockPending(st, restart.RestartUnset)
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	state := mylog.Check2(state.ReadState(nil, r))


	state.Lock()
	defer state.Unlock()
	// check snapd, core18, kernel, gadget
	_ = mylog.Check2(snapstate.CurrentInfo(state, "snapd"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(state, "core18"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(state, "pc-kernel"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(state, "pc"))
	c.Check(err, IsNil)

	// ensure required flag is set on all essential snaps
	var snapst snapstate.SnapState
	for _, reqName := range []string{"snapd", "core18", "pc-kernel", "pc"} {
		mylog.Check(snapstate.Get(state, reqName, &snapst))

		c.Assert(snapst.Required, Equals, true, Commentf("required not set for %v", reqName))
	}

	// the right systemd commands were run
	c.Check(sysdLog, testutil.DeepContains, []string{"start", "usr-lib-snapd.mount"})

	// and ensure state is now considered seeded
	var seeded bool
	mylog.Check(state.Get("seeded", &seeded))

	c.Check(seeded, Equals, true)

	// check we set seed-time
	var seedTime time.Time
	mylog.Check(state.Get("seed-time", &seedTime))

	c.Check(seedTime.IsZero(), Equals, false)
}

func (s *firstBoot16Suite) TestPopulateFromSeedOrdering(c *C) {
	s.WriteAssertions("developer.account", s.devAcct)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", map[string]interface{}{"base": "core18"})
	s.WriteAssertions("model.asserts", assertsChain...)

	core18Fname, snapdFname, kernelFname, gadgetFname := s.makeCore18Snaps(c, nil)

	snapYaml := `name: snap-req-other-base
version: 1.0
base: other-base
`
	snapFname, snapDecl, snapRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("snap-req-other-base.asserts", s.devAcct, snapRev, snapDecl)
	baseYaml := `name: other-base
version: 1.0
type: base
`
	baseFname, baseDecl, baseRev := s.MakeAssertedSnap(c, baseYaml, nil, snap.R(127), "developerid")
	s.WriteAssertions("other-base.asserts", s.devAcct, baseRev, baseDecl)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: core18
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: snap-req-other-base
   file: %s
 - name: other-base
   file: %s
`, snapdFname, core18Fname, kernelFname, gadgetFname, snapFname, baseFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	checkOrder(c, tsAll, "snapd", "pc-kernel", "core18", "pc", "other-base", "snap-req-other-base")
}

func (s *firstBoot16Suite) TestFirstbootGadgetBaseModelBaseMismatch(c *C) {
	s.WriteAssertions("developer.account", s.devAcct)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", map[string]interface{}{"base": "core18"})
	s.WriteAssertions("model.asserts", assertsChain...)

	core18Fname, snapdFname, kernelFname, _ := s.makeCore18Snaps(c, nil)
	// take the gadget without "base: core18"
	_, _, gadgetFname := s.makeCoreSnaps(c, "")

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: core18
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
`, snapdFname, core18Fname, kernelFname, gadgetFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	_ = mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))
	c.Assert(err, ErrorMatches, `cannot use gadget snap because its base "core" is different from model base "core18"`)
	// note, cannot use st.Tasks() here as it filters out tasks with no change
	c.Check(st.TaskCount(), Equals, 0)
}

func (s *firstBoot16Suite) TestPopulateFromSeedWrongContentProviderOrder(c *C) {
	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, "")

	// a snap that uses content providers
	snapYaml := `name: gnome-calculator
version: 1.0
plugs:
 gtk-3-themes:
  interface: content
  default-provider: gtk-common-themes
  target: $SNAP/data-dir/themes
`
	calcFname, calcDecl, calcRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("calc.asserts", s.devAcct, calcRev, calcDecl)

	// put a 2nd firstboot snap into the SnapBlobDir
	snapYaml = `name: gtk-common-themes
version: 1.0
slots:
 gtk-3-themes:
  interface: content
  source:
   read:
    - $SNAP/share/themes/Adawaita
`
	themesFname, themesDecl, themesRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(65), "developerid")
	s.WriteAssertions("themes.asserts", s.devAcct, themesDecl, themesRev)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: core
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: gnome-calculator
   file: %s
 - name: gtk-common-themes
   file: %s
`, coreFname, kernelFname, gadgetFname, calcFname, themesFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))

	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()

	c.Assert(chg.Err(), IsNil)


	// verify the result
	var conns map[string]interface{}
	mylog.Check(st.Get("conns", &conns))

	c.Check(conns, HasLen, 1)
	conn, hasConn := conns["gnome-calculator:gtk-3-themes gtk-common-themes:gtk-3-themes"]
	c.Check(hasConn, Equals, true)
	c.Check(conn.(map[string]interface{})["auto"], Equals, true)
	c.Check(conn.(map[string]interface{})["interface"], Equals, "content")
}

func (s *firstBoot16Suite) TestPopulateFromSeedAlternativeContentProviderAndOrder(c *C) {
	logbuf, restore := logger.MockLogger()
	defer restore()

	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core_1.snap")

	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, "")

	// a snap that uses content providers
	snapYaml := `name: gnome-calculator
version: 1.0
plugs:
 gtk-3-themes:
  interface: content
  default-provider: gtk-common-themes
  target: $SNAP/data-dir/themes
`
	calcFname, calcDecl, calcRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("calc.asserts", s.devAcct, calcRev, calcDecl)

	// put a 2nd firstboot snap into the SnapBlobDir
	snapYaml = `name: gtk-common-themes-alt
version: 1.0
slots:
 gtk-3-themes:
  interface: content
  source:
   read:
    - $SNAP/share/themes/Adawaita
`
	themesFname, themesDecl, themesRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(65), "developerid")
	s.WriteAssertions("themes.asserts", s.devAcct, themesDecl, themesRev)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: core
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: gnome-calculator
   file: %s
 - name: gtk-common-themes-alt
   file: %s
`, coreFname, kernelFname, gadgetFname, calcFname, themesFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))

	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()

	c.Assert(chg.Err(), IsNil)


	// verify the result
	var conns map[string]interface{}
	mylog.Check(st.Get("conns", &conns))

	c.Check(conns, HasLen, 1)
	conn, hasConn := conns["gnome-calculator:gtk-3-themes gtk-common-themes-alt:gtk-3-themes"]
	c.Check(hasConn, Equals, true)
	c.Check(conn.(map[string]interface{})["auto"], Equals, true)
	c.Check(conn.(map[string]interface{})["interface"], Equals, "content")

	c.Check(logbuf.String(), Matches, `(?sm).*seed prerequisites: snap "gnome-calculator" requires a provider for content "gtk-3-themes", a candidate slot is available \(gtk-common-themes-alt:gtk-3-themes\) but not the default-provider, ensure a single auto-connection \(or possibly a connection\) is in-place.*`)
}

func (s *firstBoot16Suite) TestPopulateFromSeedMissingBase(c *C) {
	s.WriteAssertions("developer.account", s.devAcct)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, "")

	// TODO: this test doesn't particularly need to use a local snap
	// local snap with unknown base
	snapYaml = `name: local
base: foo
version: 1.0`
	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)
	localFname := filepath.Base(mockSnapFile)
	targetSnapFile2 := filepath.Join(dirs.SnapSeedDir, "snaps", localFname)
	c.Assert(os.Rename(mockSnapFile, targetSnapFile2), IsNil)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: core
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
 - name: local
   unasserted: true
   file: %s
`, coreFname, kernelFname, gadgetFname, localFname))

	c.Assert(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644), IsNil)

	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	_ := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))
	c.Assert(err, ErrorMatches, `cannot use snap "local": base "foo" is missing`)
}

func (s *firstBoot16Suite) TestPopulateFromSeedOnClassicWithSnapdOnlyHappy(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	core18Fname, snapdFname, _, _ := s.makeCore18Snaps(c, &core18SnapsOpts{
		classic: true,
	})

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0
base: core18
`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")
	s.WriteAssertions("foo.asserts", s.devAcct, fooRev, fooDecl)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: foo
   file: %s
 - name: core18
   file: %s
`, snapdFname, fooFname, core18Fname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	checkOrder(c, tsAll, "snapd", "core18", "foo")

	// now run the change and check the result
	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	c.Assert(chg.Err(), IsNil)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	// run change until it wants to restart
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()


	// at this point the system is "restarting", pretend the restart has
	// happened
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	restart.MockPending(st, restart.RestartUnset)
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus)

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	state := mylog.Check2(state.ReadState(nil, r))


	state.Lock()
	defer state.Unlock()
	// check snapd, core18, kernel, gadget
	_ = mylog.Check2(snapstate.CurrentInfo(state, "snapd"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(state, "core18"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(state, "foo"))
	c.Check(err, IsNil)

	// and ensure state is now considered seeded
	var seeded bool
	mylog.Check(state.Get("seeded", &seeded))

	c.Check(seeded, Equals, true)

	// check we set seed-time
	var seedTime time.Time
	mylog.Check(state.Get("seed-time", &seedTime))

	c.Check(seedTime.IsZero(), Equals, false)
}

func (s *firstBoot16Suite) TestPopulateFromSeedMissingAssertions(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	core18Fname, snapdFname, _, _ := s.makeCore18Snaps(c, &core18SnapsOpts{})

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: core18
   file: %s
`, snapdFname, core18Fname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	_ = mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))
	c.Assert(err, NotNil)
	// note, cannot use st.Tasks() here as it filters out tasks with no change
	c.Check(st.TaskCount(), Equals, 0)
}

func (s *firstBoot16Suite) TestPopulateFromSeedOnClassicWithSnapdOnlyAndGadgetHappy(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	core18Fname, snapdFname, _, gadgetFname := s.makeCore18Snaps(c, &core18SnapsOpts{
		classic: true,
		gadget:  true,
	})

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0
base: core18
`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")

	s.WriteAssertions("foo.asserts", s.devAcct, fooRev, fooDecl)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", map[string]interface{}{"gadget": "pc"})
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: foo
   file: %s
 - name: core18
   file: %s
 - name: pc
   file: %s
`, snapdFname, fooFname, core18Fname, gadgetFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	checkOrder(c, tsAll, "snapd", "core18", "pc", "foo")

	// now run the change and check the result
	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	c.Assert(chg.Err(), IsNil)

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	// run change until it wants to restart
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()


	// at this point the system is "restarting", pretend the restart has
	// happened
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	restart.MockPending(st, restart.RestartUnset)
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%s", chg.Err()))

	// verify
	r := mylog.Check2(os.Open(dirs.SnapStateFile))

	state := mylog.Check2(state.ReadState(nil, r))


	state.Lock()
	defer state.Unlock()
	// check snapd, core18, kernel, gadget
	_ = mylog.Check2(snapstate.CurrentInfo(state, "snapd"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(state, "core18"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(state, "pc"))
	c.Check(err, IsNil)
	_ = mylog.Check2(snapstate.CurrentInfo(state, "foo"))
	c.Check(err, IsNil)

	// and ensure state is now considered seeded
	var seeded bool
	mylog.Check(state.Get("seeded", &seeded))

	c.Check(seeded, Equals, true)

	// check we set seed-time
	var seedTime time.Time
	mylog.Check(state.Get("seed-time", &seedTime))

	c.Check(seedTime.IsZero(), Equals, false)
}

func (s *firstBoot16Suite) TestCriticalTaskEdgesForPreseed(c *C) {
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("task1", "")
	t2 := st.NewTask("task2", "")
	t3 := st.NewTask("task2", "")

	ts := state.NewTaskSet(t1, t2, t3)
	ts.MarkEdge(t1, snapstate.BeginEdge)
	ts.MarkEdge(t2, snapstate.BeforeHooksEdge)
	ts.MarkEdge(t3, snapstate.HooksEdge)

	beginEdge, beforeHooksEdge, hooksEdge := mylog.Check4(devicestate.CriticalTaskEdges(ts))

	c.Assert(beginEdge, NotNil)
	c.Assert(beforeHooksEdge, NotNil)
	c.Assert(hooksEdge, NotNil)

	c.Check(beginEdge.Kind(), Equals, "task1")
	c.Check(beforeHooksEdge.Kind(), Equals, "task2")
	c.Check(hooksEdge.Kind(), Equals, "task2")
}

func (s *firstBoot16Suite) TestCriticalTaskEdgesForPreseedMissing(c *C) {
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	t1 := st.NewTask("task1", "")
	t2 := st.NewTask("task2", "")
	t3 := st.NewTask("task2", "")

	ts := state.NewTaskSet(t1, t2, t3)
	ts.MarkEdge(t1, snapstate.BeginEdge)

	_, _, _ := mylog.Check4(devicestate.CriticalTaskEdges(ts))
	c.Assert(err, NotNil)

	ts = state.NewTaskSet(t1, t2, t3)
	ts.MarkEdge(t1, snapstate.BeginEdge)
	ts.MarkEdge(t2, snapstate.BeforeHooksEdge)
	_, _, _ = mylog.Check4(devicestate.CriticalTaskEdges(ts))
	c.Assert(err, NotNil)

	ts = state.NewTaskSet(t1, t2, t3)
	ts.MarkEdge(t1, snapstate.BeginEdge)
	ts.MarkEdge(t3, snapstate.HooksEdge)
	_, _, _ = mylog.Check4(devicestate.CriticalTaskEdges(ts))
	c.Assert(err, NotNil)
}

func (s *firstBoot16Suite) TestPopulateFromSeedWithConnectHook(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	hooksCalled := []*hookstate.Context{}
	restore = hookstate.MockRunHook(func(ctx *hookstate.Context, tomb *tomb.Tomb) ([]byte, error) {
		ctx.Lock()
		defer ctx.Unlock()

		hooksCalled = append(hooksCalled, ctx)
		return nil, nil
	})
	defer restore()

	core18Fname, snapdFname, _, _ := s.makeCore18Snaps(c, &core18SnapsOpts{
		classic: true,
	})

	snapFilesWithHook := [][]string{
		{"bin/bar", ``},
		{"meta/hooks/connect-plug-network", ``},
	}

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
base: core18
version: 1.0
plugs:
 shared-data-plug:
  interface: content
  target: import
  content: mylib
apps:
 bar:
  command: bin/bar
  plugs: [network]
`
	fooFname, fooDecl, fooRev := s.MakeAssertedSnap(c, snapYaml, snapFilesWithHook, snap.R(128), "developerid")
	s.WriteAssertions("foo.asserts", s.devAcct, fooRev, fooDecl)

	// put a 2nd firstboot snap into the SnapBlobDir
	snapYaml = `name: bar
base: core18
version: 1.0
slots:
 shared-data-slot:
  interface: content
  content: mylib
  read:
   - /
apps:
 bar:
  command: bin/bar
`
	snapFiles := [][]string{
		{"bin/bar", ``},
	}
	barFname, barDecl, barRev := s.MakeAssertedSnap(c, snapYaml, snapFiles, snap.R(65), "developerid")
	s.WriteAssertions("bar.asserts", s.devAcct, barDecl, barRev)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic", nil)
	s.WriteAssertions("model.asserts", assertsChain...)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: core18
   file: %s
 - name: foo
   file: %s
 - name: bar
   file: %s
`, snapdFname, core18Fname, fooFname, barFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))

	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	checkOrder(c, tsAll, "snapd", "core18", "foo", "bar")

	mockServer := s.mockServer(c, "REQID-1")
	defer mockServer.Close()

	restore = devicestate.MockBaseStoreURL(mockServer.URL)
	defer restore()

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()
	c.Check(err, IsNil)
	c.Check(chg.Err(), IsNil)

	// at this point the system is "restarting", pretend the restart has
	// happened
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	restart.MockPending(st, restart.RestartUnset)
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()

	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%s", chg.Err()))

	c.Assert(hooksCalled, HasLen, 1)
	c.Check(hooksCalled[0].HookName(), Equals, "connect-plug-network")

	// verify that connections was made
	var conns map[string]interface{}
	c.Assert(st.Get("conns", &conns), IsNil)
	c.Assert(conns, DeepEquals, map[string]interface{}{
		"foo:network snapd:network": map[string]interface{}{
			"auto": true, "interface": "network",
		},
		"foo:shared-data-plug bar:shared-data-slot": map[string]interface{}{
			"auto": true, "interface": "content",
			"plug-static": map[string]interface{}{
				"content": "mylib", "target": "import",
			},
			"slot-static": map[string]interface{}{
				"content": "mylib",
				"read": []interface{}{
					"/",
				},
			},
		},
	})
}

func (s *firstBoot16Suite) mockServer(c *C, reqID string) *httptest.Server {
	bhv := &devicestatetest.DeviceServiceBehavior{
		ReqID:                reqID,
		SignSerial:           s.signSerial,
		ExpectedCapabilities: "serial-stream",
	}

	mockServer, extraCerts := devicestatetest.MockDeviceService(c, bhv)
	fname := filepath.Join(dirs.SnapdStoreSSLCertsDir, "test-server-certs.pem")
	mylog.Check(os.MkdirAll(filepath.Dir(fname), 0755))

	mylog.Check(os.WriteFile(fname, extraCerts, 0644))

	return mockServer
}

func (s *firstBoot16Suite) signSerial(c *C, bhv *devicestatetest.DeviceServiceBehavior, headers map[string]interface{}, body []byte) (serial asserts.Assertion, ancillary []asserts.Assertion, err error) {
	signing := assertstest.NewStoreStack("canonical", nil)
	a := mylog.Check2(signing.Sign(asserts.SerialType, headers, body, ""))
	return a, nil, err
}

func (s *firstBoot16Suite) testPopulateFromSeedCore18ValidationSetTracking(c *C, vsAsserts []asserts.Assertion, vsHeaders []interface{}) *state.Change {
	var sysdLog [][]string
	systemctlRestorer := systemd.MockSystemctl(func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=inactive\n"), nil
	})
	defer systemctlRestorer()

	bloader := boottest.MockUC16Bootenv(bootloadertest.Mock("mock", c.MkDir()))
	bootloader.Force(bloader)
	defer bootloader.Force(nil)
	bloader.SetBootKernel("pc-kernel_1.snap")
	bloader.SetBootBase("core18_1.snap")

	s.WriteAssertions("developer.account", s.devAcct)

	core18Fname, snapdFname, kernelFname, gadgetFname := s.makeCore18Snaps(c, &core18SnapsOpts{})

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", map[string]interface{}{"base": "core18", "validation-sets": vsHeaders})
	s.WriteAssertions("model.asserts", assertsChain...)

	// write validation set assertions
	for i, a := range vsAsserts {
		s.WriteAssertions(fmt.Sprintf("vs-%d.validation-set", i), a)
	}

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: snapd
   file: %s
 - name: core18
   file: %s
 - name: pc-kernel
   file: %s
 - name: pc
   file: %s
`, snapdFname, core18Fname, kernelFname, gadgetFname))
	mylog.Check(os.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644))


	// run the firstboot stuff
	s.startOverlord(c)
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll := mylog.Check2(devicestate.PopulateStateFromSeedImpl(s.overlord.DeviceManager(), s.perfTimings))


	// ensure the validation-set tracking task is present
	tsEnd := tsAll[len(tsAll)-1]
	c.Assert(tsEnd.Tasks(), HasLen, 2)
	t := tsEnd.Tasks()[0]
	c.Check(t.Kind(), Equals, "enforce-validation-sets")

	expectedSeqs := make(map[string]int)
	expectedVss := make(map[string][]string)
	for _, vs := range vsHeaders {
		hdrs := vs.(map[string]interface{})
		seq := mylog.Check2(strconv.Atoi(hdrs["sequence"].(string)))

		key := fmt.Sprintf("%s/%s/%s", release.Series, hdrs["account-id"].(string), hdrs["name"].(string))
		expectedSeqs[key] = seq
		expectedVss[key] = []string{release.Series, hdrs["account-id"].(string), hdrs["name"].(string), hdrs["sequence"].(string)}
	}

	// verify that the correct data is provided to the task
	var pinnedSeqs map[string]int
	mylog.Check(t.Get("pinned-sequence-numbers", &pinnedSeqs))

	c.Check(pinnedSeqs, DeepEquals, expectedSeqs)
	var vsKeys map[string][]string
	mylog.Check(t.Get("validation-set-keys", &vsKeys))

	c.Check(vsKeys, DeepEquals, expectedVss)

	// use the expected kind otherwise settle with start another one
	chg := st.NewChange("seed", "run the populate from seed changes")
	for _, ts := range tsAll {
		chg.AddAll(ts)
	}
	c.Assert(st.Changes(), HasLen, 1)

	checkOrder(c, tsAll, "snapd", "pc-kernel", "core18", "pc")

	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()
	c.Check(err, IsNil)
	c.Check(chg.Err(), IsNil)

	// at this point the system is "restarting", pretend the restart has
	// happened
	c.Assert(chg.Status(), Equals, state.DoingStatus)
	restart.MockPending(st, restart.RestartUnset)
	st.Unlock()
	mylog.Check(s.overlord.Settle(settleTimeout))
	st.Lock()

	return chg
}

func (s *firstBoot16Suite) TestPopulateFromSeedCore18ValidationSetTrackingHappy(c *C) {
	a := mylog.Check2(s.StoreSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "base-set",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       s.AssertedSnapID("pc-kernel"),
				"presence": "required",
				"revision": "1",
			},
			map[string]interface{}{
				"name":     "pc",
				"id":       s.AssertedSnapID("pc"),
				"presence": "required",
				"revision": "1",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))


	headers := map[string]interface{}{
		"account-id": "canonical",
		"name":       "base-set",
		"sequence":   "1",
		"mode":       "enforce",
	}
	chg := s.testPopulateFromSeedCore18ValidationSetTracking(c, []asserts.Assertion{a}, []interface{}{headers})

	s.overlord.State().Lock()
	defer s.overlord.State().Unlock()
	c.Assert(chg.Status(), Equals, state.DoneStatus, Commentf("%s", chg.Err()))

	// Ensure that we are now tracking the validation-set
	var tr assertstate.ValidationSetTracking
	mylog.Check(assertstate.GetValidationSet(s.overlord.State(), "canonical", "base-set", &tr))

	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: "canonical",
		Name:      "base-set",
		Mode:      assertstate.Enforce,
		Current:   1,
		PinnedAt:  1,
	})
}

func (s *firstBoot16Suite) TestPopulateFromSeedCore18ValidationSetTrackingUnmetCriteria(c *C) {
	a := mylog.Check2(s.StoreSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "base-set",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       s.AssertedSnapID("pc-kernel"),
				"presence": "required",
				// Set required revision of pc-kernel to 7, this should make it fail
				"revision": "7",
			},
			map[string]interface{}{
				"name":     "pc",
				"id":       s.AssertedSnapID("pc"),
				"presence": "required",
				"revision": "1",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, ""))


	headers := map[string]interface{}{
		"account-id": "canonical",
		"name":       "base-set",
		"sequence":   "1",
		"mode":       "enforce",
	}
	chg := s.testPopulateFromSeedCore18ValidationSetTracking(c, []asserts.Assertion{a}, []interface{}{headers})

	s.overlord.State().Lock()
	defer s.overlord.State().Unlock()
	c.Assert(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err().Error(), testutil.Contains, "pc-kernel (required at revision 7 by sets canonical/base-set))")
}
