// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type FirstBootTestSuite struct {
	aa          *testutil.MockCmd
	systemctl   *testutil.MockCmd
	mockUdevAdm *testutil.MockCmd
	snapSeccomp *testutil.MockCmd

	storeSigning *assertstest.StoreStack
	restore      func()

	brandPrivKey asserts.PrivateKey
	brandSigning *assertstest.SigningDB

	overlord *overlord.Overlord

	restoreOnClassic func()
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)
	s.restoreOnClassic = release.MockOnClassic(false)

	// mock the world!
	err := os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "snaps"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "assertions"), 0755)
	c.Assert(err, IsNil)

	err = os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	s.aa = testutil.MockCommand(c, "apparmor_parser", "")
	s.systemctl = testutil.MockCommand(c, "systemctl", "")
	s.mockUdevAdm = testutil.MockCommand(c, "udevadm", "")

	snapSeccompPath := filepath.Join(dirs.DistroLibExecDir, "snap-seccomp")
	err = os.MkdirAll(filepath.Dir(snapSeccompPath), 0755)
	c.Assert(err, IsNil)
	s.snapSeccomp = testutil.MockCommand(c, snapSeccompPath, "")

	err = ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), nil, 0644)
	c.Assert(err, IsNil)

	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
	s.restore = sysdb.InjectTrusted(s.storeSigning.Trusted)

	s.brandPrivKey, _ = assertstest.GenerateKey(752)
	s.brandSigning = assertstest.NewSigningDB("my-brand", s.brandPrivKey)

	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	s.overlord = ovld
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	s.aa.Restore()
	s.systemctl.Restore()
	s.mockUdevAdm.Restore()
	s.snapSeccomp.Restore()

	s.restore()
	s.restoreOnClassic()
	dirs.SetRootDir("/")
}

func (s *FirstBootTestSuite) TestPopulateFromSeedOnClassicNoop(c *C) {
	release.OnClassic = true

	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	err := os.Remove(filepath.Join(dirs.SnapSeedDir, "assertions"))
	c.Assert(err, IsNil)

	tsAll, err := devicestate.PopulateStateFromSeedImpl(st)
	c.Assert(err, IsNil)
	// only mark seeded
	c.Check(tsAll, HasLen, 1)
	tasks := tsAll[0].Tasks()
	c.Check(tasks, HasLen, 1)
	c.Check(tasks[0].Kind(), Equals, "mark-seeded")
}

func (s *FirstBootTestSuite) TestPopulateFromSeedOnClassicNoSeedYaml(c *C) {
	release.OnClassic = true

	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	st := ovld.State()

	// add a bunch of assert files
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic")
	for i, as := range assertsChain {
		fn := filepath.Join(dirs.SnapSeedDir, "assertions", strconv.Itoa(i))
		err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
		c.Assert(err, IsNil)
	}

	err = os.Remove(filepath.Join(dirs.SnapSeedDir, "seed.yaml"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	tsAll, err := devicestate.PopulateStateFromSeedImpl(st)
	c.Assert(err, IsNil)
	// only mark seeded
	c.Check(tsAll, HasLen, 1)
	tasks := tsAll[0].Tasks()
	c.Check(tasks, HasLen, 1)
	c.Check(tasks[0].Kind(), Equals, "mark-seeded")

	ds, err := auth.Device(st)
	c.Assert(err, IsNil)
	c.Check(ds.Brand, Equals, "my-brand")
	c.Check(ds.Model, Equals, "my-model-classic")
}

func (s *FirstBootTestSuite) TestPopulateFromSeedErrorsOnState(c *C) {
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	st.Set("seeded", true)

	_, err := devicestate.PopulateStateFromSeedImpl(st)
	c.Assert(err, ErrorMatches, "cannot populate state: already seeded")
}

func (s *FirstBootTestSuite) makeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID string) (snapFname string, snapDecl *asserts.SnapDeclaration, snapRev *asserts.SnapRevision) {
	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	snapName := info.Name()

	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, files)
	snapFname = filepath.Base(mockSnapFile)

	targetFile := filepath.Join(dirs.SnapSeedDir, "snaps", snapFname)
	err = os.Rename(mockSnapFile, targetFile)
	c.Assert(err, IsNil)

	declA, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapName + "-snap-id",
		"publisher-id": developerID,
		"snap-name":    snapName,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	sha3_384, size, err := asserts.SnapFileSHA3_384(targetFile)
	c.Assert(err, IsNil)

	revA, err := s.storeSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": sha3_384,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-id":       snapName + "-snap-id",
		"developer-id":  developerID,
		"snap-revision": revision.String(),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	return snapFname, declA.(*asserts.SnapDeclaration), revA.(*asserts.SnapRevision)
}

func (s *FirstBootTestSuite) makeCoreSnaps(c *C, withConfigure bool) (coreFname, kernelFname, gadgetFname string) {
	files := [][]string{}
	if withConfigure {
		files = [][]string{{"meta/hooks/configure", ""}}
	}

	// put core snap into the SnapBlobDir
	snapYaml := `name: core
version: 1.0
type: os`
	coreFname, coreDecl, coreRev := s.makeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")

	writeAssertionsToFile("core.asserts", []asserts.Assertion{coreRev, coreDecl})

	// put kernel snap into the SnapBlobDir
	snapYaml = `name: pc-kernel
version: 1.0
type: kernel`
	kernelFname, kernelDecl, kernelRev := s.makeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")

	writeAssertionsToFile("kernel.asserts", []asserts.Assertion{kernelRev, kernelDecl})

	gadgetYaml := `
volumes:
    volume-id:
        bootloader: grub
`
	if withConfigure {
		gadgetYaml += `
defaults:
    foo-snap-id:
       foo-cfg: foo.
    core-snap-id:
       core-cfg: core_cfg_defl
    pc-kernel-snap-id:
       pc-kernel-cfg: pc-kernel_cfg_defl
    pc-snap-id:
       pc-cfg: pc_cfg_defl
`
	}

	// put gadget snap into the SnapBlobDir
	files = append(files, []string{"meta/gadget.yaml", gadgetYaml})

	snapYaml = `name: pc
version: 1.0
type: gadget`
	gadgetFname, gadgetDecl, gadgetRev := s.makeAssertedSnap(c, snapYaml, files, snap.R(1), "canonical")

	writeAssertionsToFile("gadget.asserts", []asserts.Assertion{gadgetRev, gadgetDecl})

	return coreFname, kernelFname, gadgetFname
}

func (s *FirstBootTestSuite) makeBecomeOpertionalChange(c *C) *state.Change {
	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, false)

	devAcct := assertstest.NewAccount(s.storeSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")
	devAcctFn := filepath.Join(dirs.SnapSeedDir, "assertions", "developer.account")
	err := ioutil.WriteFile(devAcctFn, asserts.Encode(devAcct), 0644)
	c.Assert(err, IsNil)

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0`
	fooFname, fooDecl, fooRev := s.makeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")

	// put a firstboot local snap into the SnapBlobDir
	snapYaml = `name: local
version: 1.0`
	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)
	targetSnapFile2 := filepath.Join(dirs.SnapSeedDir, "snaps", filepath.Base(mockSnapFile))
	err = os.Rename(mockSnapFile, targetSnapFile2)
	c.Assert(err, IsNil)

	declFn := filepath.Join(dirs.SnapSeedDir, "assertions", "foo.snap-declaration")
	err = ioutil.WriteFile(declFn, asserts.Encode(fooDecl), 0644)
	c.Assert(err, IsNil)

	revFn := filepath.Join(dirs.SnapSeedDir, "assertions", "foo.snap-revision")
	err = ioutil.WriteFile(revFn, asserts.Encode(fooRev), 0644)
	c.Assert(err, IsNil)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", "foo")
	for i, as := range assertsChain {
		fn := filepath.Join(dirs.SnapSeedDir, "assertions", strconv.Itoa(i))
		err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
		c.Assert(err, IsNil)
	}

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
	err = ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll, err := devicestate.PopulateStateFromSeedImpl(st)
	c.Assert(err, IsNil)

	// the last task of the last taskset must be mark-seeded
	markSeededTask := tsAll[len(tsAll)-1].Tasks()[0]
	c.Check(markSeededTask.Kind(), Equals, "mark-seeded")
	// and the markSeededTask must wait for the other tasks
	prevTasks := tsAll[len(tsAll)-2].Tasks()
	otherTask := prevTasks[len(prevTasks)-1]
	c.Check(markSeededTask.WaitTasks(), testutil.Contains, otherTask)

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

	return chg
}

func (s *FirstBootTestSuite) TestPopulateFromSeedHappy(c *C) {
	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)
	bootloader.SetBootVars(map[string]string{
		"snap_core":   "core_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})

	chg := s.makeBecomeOpertionalChange(c)
	s.overlord.Settle()

	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	c.Assert(chg.Err(), IsNil)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "local", "x1", "meta", "snap.yaml")), Equals, true)

	// verify
	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	state, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	state.Lock()
	defer state.Unlock()
	// check core, kernel, gadget
	_, err = snapstate.CurrentInfo(state, "core")
	c.Assert(err, IsNil)
	_, err = snapstate.CurrentInfo(state, "pc-kernel")
	c.Assert(err, IsNil)
	_, err = snapstate.CurrentInfo(state, "pc")
	c.Assert(err, IsNil)

	// check foo
	info, err := snapstate.CurrentInfo(state, "foo")
	c.Assert(err, IsNil)
	c.Assert(info.SnapID, Equals, "foo-snap-id")
	c.Assert(info.Revision, Equals, snap.R(128))
	c.Assert(info.Contact, Equals, "mailto:some.guy@example.com")
	pubAcct, err := assertstate.Publisher(st, info.SnapID)
	c.Assert(err, IsNil)
	c.Check(pubAcct.AccountID(), Equals, "developerid")

	var snapst snapstate.SnapState
	err = snapstate.Get(state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.DevMode, Equals, true)
	c.Assert(snapst.Required, Equals, true)

	// check local
	info, err = snapstate.CurrentInfo(state, "local")
	c.Assert(err, IsNil)
	c.Assert(info.SnapID, Equals, "")
	c.Assert(info.Revision, Equals, snap.R("x1"))

	var snapst2 snapstate.SnapState
	err = snapstate.Get(state, "local", &snapst2)
	c.Assert(err, IsNil)
	c.Assert(snapst2.Required, Equals, false)

	// and ensure state is now considered seeded
	var seeded bool
	err = state.Get("seeded", &seeded)
	c.Assert(err, IsNil)
	c.Check(seeded, Equals, true)
}

func (s *FirstBootTestSuite) TestPopulateFromSeedMissingBootloader(c *C) {
	chg := s.makeBecomeOpertionalChange(c)
	st := s.overlord.State()

	// run the change, we cannot use s.overlord.Settle() here as each
	// "DeviceManager.Ensure()" will call ensureSeedYaml which will
	// create a new "seed" change so things never settle down
	s.overlord.Loop()
	defer s.overlord.Stop()
	for !chg.IsReady() {
		// nothing
	}

	st.Lock()
	defer st.Unlock()
	c.Assert(chg.Err(), ErrorMatches, `(?s).* cannot determine bootloader.*`)
}

func writeAssertionsToFile(fn string, assertions []asserts.Assertion) {
	multifn := filepath.Join(dirs.SnapSeedDir, "assertions", fn)
	f, err := os.Create(multifn)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	enc := asserts.NewEncoder(f)
	for _, a := range assertions {
		err := enc.Encode(a)
		if err != nil {
			panic(err)
		}
	}
}

func (s *FirstBootTestSuite) TestPopulateFromSeedHappyMultiAssertsFiles(c *C) {
	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)
	bootloader.SetBootVars(map[string]string{
		"snap_core":   "core_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})

	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, false)

	devAcct := assertstest.NewAccount(s.storeSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")

	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0`
	fooFname, fooDecl, fooRev := s.makeAssertedSnap(c, snapYaml, nil, snap.R(128), "developerid")

	writeAssertionsToFile("foo.asserts", []asserts.Assertion{devAcct, fooRev, fooDecl})

	// put a 2nd firstboot snap into the SnapBlobDir
	snapYaml = `name: bar
version: 1.0`
	barFname, barDecl, barRev := s.makeAssertedSnap(c, snapYaml, nil, snap.R(65), "developerid")

	writeAssertionsToFile("bar.asserts", []asserts.Assertion{devAcct, barDecl, barRev})

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model")
	writeAssertionsToFile("model.asserts", assertsChain)

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
	err := ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	tsAll, err := devicestate.PopulateStateFromSeedImpl(st)
	c.Assert(err, IsNil)
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
	s.overlord.Settle()
	st.Lock()
	c.Assert(chg.Err(), IsNil)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "bar", "65", "meta", "snap.yaml")), Equals, true)

	// verify
	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	state, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	state.Lock()
	defer state.Unlock()
	// check foo
	info, err := snapstate.CurrentInfo(state, "foo")
	c.Assert(err, IsNil)
	c.Check(info.SnapID, Equals, "foo-snap-id")
	c.Check(info.Revision, Equals, snap.R(128))
	pubAcct, err := assertstate.Publisher(st, info.SnapID)
	c.Assert(err, IsNil)
	c.Check(pubAcct.AccountID(), Equals, "developerid")

	// check bar
	info, err = snapstate.CurrentInfo(state, "bar")
	c.Assert(err, IsNil)
	c.Check(info.SnapID, Equals, "bar-snap-id")
	c.Check(info.Revision, Equals, snap.R(65))
	pubAcct, err = assertstate.Publisher(st, info.SnapID)
	c.Assert(err, IsNil)
	c.Check(pubAcct.AccountID(), Equals, "developerid")
}

func (s *FirstBootTestSuite) makeModelAssertion(c *C, modelStr string, reqSnaps ...string) *asserts.Model {
	headers := map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        modelStr,
		"architecture": "amd64",
		"store":        "canonical",
		"gadget":       "pc",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	if strings.HasSuffix(modelStr, "-classic") {
		headers["classic"] = "true"
	} else {
		headers["kernel"] = "pc-kernel"
	}
	if len(reqSnaps) != 0 {
		reqs := make([]interface{}, len(reqSnaps))
		for i, req := range reqSnaps {
			reqs[i] = req
		}
		headers["required-snaps"] = reqs
	}
	model, err := s.brandSigning.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, IsNil)
	return model.(*asserts.Model)
}

func (s *FirstBootTestSuite) makeModelAssertionChain(c *C, modName string, reqSnaps ...string) []asserts.Assertion {
	assertChain := []asserts.Assertion{}

	brandAcct := assertstest.NewAccount(s.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "certified",
	}, "")
	assertChain = append(assertChain, brandAcct)

	brandAccKey := assertstest.NewAccountKey(s.storeSigning, brandAcct, nil, s.brandPrivKey.PublicKey(), "")
	assertChain = append(assertChain, brandAccKey)

	model := s.makeModelAssertion(c, modName, reqSnaps...)
	assertChain = append(assertChain, model)

	storeAccountKey := s.storeSigning.StoreAccountKey("")
	assertChain = append(assertChain, storeAccountKey)
	return assertChain
}

func (s *FirstBootTestSuite) TestPopulateFromSeedConfigureHappy(c *C) {
	bootloader := boottest.NewMockBootloader("mock", c.MkDir())
	partition.ForceBootloader(bootloader)
	defer partition.ForceBootloader(nil)
	bootloader.SetBootVars(map[string]string{
		"snap_core":   "core_1.snap",
		"snap_kernel": "pc-kernel_1.snap",
	})

	coreFname, kernelFname, gadgetFname := s.makeCoreSnaps(c, true)

	devAcct := assertstest.NewAccount(s.storeSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")

	devAcctFn := filepath.Join(dirs.SnapSeedDir, "assertions", "developer.account")
	err := ioutil.WriteFile(devAcctFn, asserts.Encode(devAcct), 0644)
	c.Assert(err, IsNil)

	// put a firstboot snap into the SnapBlobDir
	files := [][]string{{"meta/hooks/configure", ""}}
	snapYaml := `name: foo
version: 1.0`
	fooFname, fooDecl, fooRev := s.makeAssertedSnap(c, snapYaml, files, snap.R(128), "developerid")

	declFn := filepath.Join(dirs.SnapSeedDir, "assertions", "foo.snap-declaration")
	err = ioutil.WriteFile(declFn, asserts.Encode(fooDecl), 0644)
	c.Assert(err, IsNil)

	revFn := filepath.Join(dirs.SnapSeedDir, "assertions", "foo.snap-revision")
	err = ioutil.WriteFile(revFn, asserts.Encode(fooRev), 0644)
	c.Assert(err, IsNil)

	// add a model assertion and its chain
	assertsChain := s.makeModelAssertionChain(c, "my-model", "foo")
	for i, as := range assertsChain {
		fn := filepath.Join(dirs.SnapSeedDir, "assertions", strconv.Itoa(i))
		err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
		c.Assert(err, IsNil)
	}

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
	err = ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()
	tsAll, err := devicestate.PopulateStateFromSeedImpl(st)
	c.Assert(err, IsNil)

	// the last task of the last taskset must be mark-seeded
	markSeededTask := tsAll[len(tsAll)-1].Tasks()[0]
	c.Check(markSeededTask.Kind(), Equals, "mark-seeded")
	// and the markSeededTask must wait for the other tasks
	prevTasks := tsAll[len(tsAll)-2].Tasks()
	otherTask := prevTasks[len(prevTasks)-1]
	c.Check(markSeededTask.WaitTasks(), testutil.Contains, otherTask)

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
		_, err := snapstate.GadgetInfo(st)
		c.Check(err, IsNil)
		configured = append(configured, ctx.SnapName())
		return nil, nil
	}

	rhk := hookstate.MockRunHook(hookInvoke)
	defer rhk()

	// avoid device reg
	chg1 := st.NewChange("become-operational", "init device")
	chg1.SetStatus(state.DoingStatus)

	st.Unlock()
	s.overlord.Settle()
	st.Lock()
	c.Assert(chg.Err(), IsNil)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	// verify
	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	state, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	state.Lock()
	defer state.Unlock()
	tr := config.NewTransaction(state)
	var val string

	// check core, kernel, gadget
	_, err = snapstate.CurrentInfo(state, "core")
	c.Assert(err, IsNil)
	err = tr.Get("core", "core-cfg", &val)
	c.Assert(err, IsNil)
	c.Check(val, Equals, "core_cfg_defl")

	_, err = snapstate.CurrentInfo(state, "pc-kernel")
	c.Assert(err, IsNil)
	err = tr.Get("pc-kernel", "pc-kernel-cfg", &val)
	c.Assert(err, IsNil)
	c.Check(val, Equals, "pc-kernel_cfg_defl")

	_, err = snapstate.CurrentInfo(state, "pc")
	c.Assert(err, IsNil)
	err = tr.Get("pc", "pc-cfg", &val)
	c.Assert(err, IsNil)
	c.Check(val, Equals, "pc_cfg_defl")

	// check foo
	info, err := snapstate.CurrentInfo(state, "foo")
	c.Assert(err, IsNil)
	c.Assert(info.SnapID, Equals, "foo-snap-id")
	c.Assert(info.Revision, Equals, snap.R(128))
	pubAcct, err := assertstate.Publisher(st, info.SnapID)
	c.Assert(err, IsNil)
	c.Check(pubAcct.AccountID(), Equals, "developerid")

	// check foo config
	err = tr.Get("foo", "foo-cfg", &val)
	c.Assert(err, IsNil)
	c.Check(val, Equals, "foo.")

	c.Check(configured, DeepEquals, []string{"core", "pc-kernel", "pc", "foo"})

	// and ensure state is now considered seeded
	var seeded bool
	err = state.Get("seeded", &seeded)
	c.Assert(err, IsNil)
	c.Check(seeded, Equals, true)
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedClassicModelMismatch(c *C) {
	release.OnClassic = true

	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	st := ovld.State()

	// add a bunch of assert files
	assertsChain := s.makeModelAssertionChain(c, "my-model")
	for i, as := range assertsChain {
		fn := filepath.Join(dirs.SnapSeedDir, "assertions", strconv.Itoa(i))
		err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
		c.Assert(err, IsNil)
	}

	// import them
	st.Lock()
	defer st.Unlock()

	_, err = devicestate.ImportAssertionsFromSeed(st)
	c.Assert(err, ErrorMatches, "cannot seed a classic system with an all-snaps model")
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedAllSnapsModelMismatch(c *C) {
	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	st := ovld.State()

	// add a bunch of assert files
	assertsChain := s.makeModelAssertionChain(c, "my-model-classic")
	for i, as := range assertsChain {
		fn := filepath.Join(dirs.SnapSeedDir, "assertions", strconv.Itoa(i))
		err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
		c.Assert(err, IsNil)
	}

	// import them
	st.Lock()
	defer st.Unlock()

	_, err = devicestate.ImportAssertionsFromSeed(st)
	c.Assert(err, ErrorMatches, "cannot seed an all-snaps system with a classic model")
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedHappy(c *C) {
	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	st := ovld.State()

	// add a bunch of assert files
	assertsChain := s.makeModelAssertionChain(c, "my-model")
	for i, as := range assertsChain {
		fn := filepath.Join(dirs.SnapSeedDir, "assertions", strconv.Itoa(i))
		err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
		c.Assert(err, IsNil)
	}

	// import them
	st.Lock()
	defer st.Unlock()

	model, err := devicestate.ImportAssertionsFromSeed(st)
	c.Assert(err, IsNil)
	c.Assert(model, NotNil)

	// verify that the model was added
	db := assertstate.DB(st)
	as, err := db.Find(asserts.ModelType, map[string]string{
		"series":   "16",
		"brand-id": "my-brand",
		"model":    "my-model",
	})
	c.Assert(err, IsNil)
	_, ok := as.(*asserts.Model)
	c.Check(ok, Equals, true)

	ds, err := auth.Device(st)
	c.Assert(err, IsNil)
	c.Check(ds.Brand, Equals, "my-brand")
	c.Check(ds.Model, Equals, "my-model")

	c.Check(model.BrandID(), Equals, "my-brand")
	c.Check(model.Model(), Equals, "my-model")
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedMissingSig(c *C) {
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	// write out only the model assertion
	assertsChain := s.makeModelAssertionChain(c, "my-model")
	for _, as := range assertsChain {
		if as.Type() == asserts.ModelType {
			fn := filepath.Join(dirs.SnapSeedDir, "assertions", "model")
			err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
			c.Assert(err, IsNil)
			break
		}
	}

	// try import and verify that its rejects because other assertions are
	// missing
	_, err := devicestate.ImportAssertionsFromSeed(st)
	c.Assert(err, ErrorMatches, "cannot find account-key .*: assertion not found")
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedTwoModelAsserts(c *C) {
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	// write out two model assertions
	model := s.makeModelAssertion(c, "my-model")
	fn := filepath.Join(dirs.SnapSeedDir, "assertions", "model")
	err := ioutil.WriteFile(fn, asserts.Encode(model), 0644)
	c.Assert(err, IsNil)

	model2 := s.makeModelAssertion(c, "my-second-model")
	fn = filepath.Join(dirs.SnapSeedDir, "assertions", "model2")
	err = ioutil.WriteFile(fn, asserts.Encode(model2), 0644)
	c.Assert(err, IsNil)

	// try import and verify that its rejects because other assertions are
	// missing
	_, err = devicestate.ImportAssertionsFromSeed(st)
	c.Assert(err, ErrorMatches, "cannot add more than one model assertion")
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedNoModelAsserts(c *C) {
	st := s.overlord.State()
	st.Lock()
	defer st.Unlock()

	assertsChain := s.makeModelAssertionChain(c, "my-model")
	for _, as := range assertsChain {
		if as.Type() != asserts.ModelType {
			fn := filepath.Join(dirs.SnapSeedDir, "assertions", "model")
			err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
			c.Assert(err, IsNil)
			break
		}
	}

	// try import and verify that its rejects because other assertions are
	// missing
	_, err := devicestate.ImportAssertionsFromSeed(st)
	c.Assert(err, ErrorMatches, "need a model assertion")
}
