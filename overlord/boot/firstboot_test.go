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

package boot_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/boot"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type FirstBootTestSuite struct {
	systemctl   *testutil.MockCmd
	mockUdevAdm *testutil.MockCmd

	storeSigning *assertstest.StoreStack
	restore      func()

	brandPrivKey asserts.PrivateKey
	brandSigning *assertstest.SigningDB
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)

	// mock the world!
	err := os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "snaps"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "assertions"), 0755)
	c.Assert(err, IsNil)

	err = os.MkdirAll(dirs.SnapServicesDir, 0755)
	c.Assert(err, IsNil)
	os.Setenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS", "1")
	s.systemctl = testutil.MockCommand(c, "systemctl", "")
	s.mockUdevAdm = testutil.MockCommand(c, "udevadm", "")

	err = ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), nil, 0644)
	c.Assert(err, IsNil)

	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)
	s.storeSigning = assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)
	s.restore = sysdb.InjectTrusted(s.storeSigning.Trusted)

	s.brandPrivKey, _ = assertstest.GenerateKey(752)
	s.brandSigning = assertstest.NewSigningDB("my-brand", s.brandPrivKey)
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	os.Unsetenv("SNAPPY_SQUASHFS_UNPACK_FOR_TESTS")
	s.systemctl.Restore()
	s.mockUdevAdm.Restore()

	s.restore()
}

func (s *FirstBootTestSuite) TestTwoRuns(c *C) {
	c.Assert(boot.FirstBoot(), IsNil)
	_, err := os.Stat(dirs.SnapFirstBootStamp)
	c.Assert(err, IsNil)

	c.Assert(boot.FirstBoot(), Equals, boot.ErrNotFirstBoot)
}

func (s *FirstBootTestSuite) TestNoErrorWhenNoGadget(c *C) {
	c.Assert(boot.FirstBoot(), IsNil)
	_, err := os.Stat(dirs.SnapFirstBootStamp)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestPopulateFromSeedErrorsOnState(c *C) {
	err := os.MkdirAll(filepath.Dir(dirs.SnapStateFile), 0755)
	err = ioutil.WriteFile(dirs.SnapStateFile, nil, 0644)
	c.Assert(err, IsNil)

	err = boot.PopulateStateFromSeed()
	c.Assert(err, ErrorMatches, "cannot create state: state .* already exists")
}

func (s *FirstBootTestSuite) TestPopulateFromSeedHappy(c *C) {
	// put a firstboot snap into the SnapBlobDir
	snapYaml := `name: foo
version: 1.0`
	mockSnapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)
	targetSnapFile := filepath.Join(dirs.SnapSeedDir, "snaps", filepath.Base(mockSnapFile))
	err := os.Rename(mockSnapFile, targetSnapFile)
	c.Assert(err, IsNil)

	// create a seed.yaml
	content := []byte(fmt.Sprintf(`
snaps:
 - name: foo
   revision: 128
   snap-id: snapidsnapid
   developer-id: developerid
   file: %s
   devmode: true
`, filepath.Base(targetSnapFile)))
	err = ioutil.WriteFile(filepath.Join(dirs.SnapSeedDir, "seed.yaml"), content, 0644)
	c.Assert(err, IsNil)

	// run the firstboot stuff
	err = boot.PopulateStateFromSeed()
	c.Assert(err, IsNil)

	// and check the snap got correctly installed
	c.Check(osutil.FileExists(filepath.Join(dirs.SnapMountDir, "foo", "128", "meta", "snap.yaml")), Equals, true)

	// verify
	r, err := os.Open(dirs.SnapStateFile)
	c.Assert(err, IsNil)
	state, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	state.Lock()
	defer state.Unlock()
	info, err := snapstate.CurrentInfo(state, "foo")
	c.Assert(err, IsNil)
	c.Assert(info.SideInfo.SnapID, Equals, "snapidsnapid")
	c.Assert(info.SideInfo.DeveloperID, Equals, "developerid")

	var snapst snapstate.SnapState
	err = snapstate.Get(state, "foo", &snapst)
	c.Assert(err, IsNil)
	c.Assert(snapst.DevMode(), Equals, true)
}

func (s *FirstBootTestSuite) makeModelAssertion(c *C, modelStr string) *asserts.Model {
	headers := map[string]interface{}{
		"series":       "16",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        modelStr,
		"architecture": "amd64",
		"store":        "canonical",
		"gadget":       "pc",
		"kernel":       "pc-kernel",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	model, err := s.brandSigning.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, IsNil)
	return model.(*asserts.Model)
}

func (s *FirstBootTestSuite) makeModelAssertionChain(c *C) []asserts.Assertion {
	assertChain := []asserts.Assertion{}

	brandAcct := assertstest.NewAccount(s.storeSigning, "my-brand", map[string]interface{}{
		"account-id":   "my-brand",
		"verification": "certified",
	}, "")
	assertChain = append(assertChain, brandAcct)

	brandAccKey := assertstest.NewAccountKey(s.storeSigning, brandAcct, nil, s.brandPrivKey.PublicKey(), "")
	assertChain = append(assertChain, brandAccKey)

	model := s.makeModelAssertion(c, "my-model")
	assertChain = append(assertChain, model)

	storeAccountKey := s.storeSigning.StoreAccountKey("")
	assertChain = append(assertChain, storeAccountKey)
	return assertChain
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedHappy(c *C) {
	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	st := ovld.State()

	// add a bunch of assert files
	assertsChain := s.makeModelAssertionChain(c)
	for i, as := range assertsChain {
		fn := filepath.Join(dirs.SnapSeedDir, "assertions", strconv.Itoa(i))
		err := ioutil.WriteFile(fn, asserts.Encode(as), 0644)
		c.Assert(err, IsNil)
	}

	// import them
	err = boot.ImportAssertionsFromSeed(st)
	c.Assert(err, IsNil)

	// verify that the model was added
	st.Lock()
	defer st.Unlock()
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
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedMissingSig(c *C) {
	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	st := ovld.State()

	// write out only the model assertion
	assertsChain := s.makeModelAssertionChain(c)
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
	err = boot.ImportAssertionsFromSeed(st)
	c.Assert(err, ErrorMatches, "cannot find account-key/.*: assertion not found")
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedTwoModelAsserts(c *C) {
	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	st := ovld.State()

	// write out two model assertions
	model := s.makeModelAssertion(c, "my-model")
	fn := filepath.Join(dirs.SnapSeedDir, "assertions", "model")
	err = ioutil.WriteFile(fn, asserts.Encode(model), 0644)
	c.Assert(err, IsNil)

	model2 := s.makeModelAssertion(c, "my-second-model")
	fn = filepath.Join(dirs.SnapSeedDir, "assertions", "model2")
	err = ioutil.WriteFile(fn, asserts.Encode(model2), 0644)
	c.Assert(err, IsNil)

	// try import and verify that its rejects because other assertions are
	// missing
	err = boot.ImportAssertionsFromSeed(st)
	c.Assert(err, ErrorMatches, "cannot add more than one model assertion")
}

func (s *FirstBootTestSuite) TestImportAssertionsFromSeedNoModelAsserts(c *C) {
	ovld, err := overlord.New()
	c.Assert(err, IsNil)
	st := ovld.State()

	assertsChain := s.makeModelAssertionChain(c)
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
	err = boot.ImportAssertionsFromSeed(st)
	c.Assert(err, ErrorMatches, "need a model assertion")
}

func (s *FirstBootTestSuite) TestFirstBootOnClassicNoEnableEther(c *C) {
	release.MockOnClassic(true)
	firstBootEnableFirstEtherRun := false
	restore := boot.MockFirstbootInitialNetworkConfig(func() error {
		firstBootEnableFirstEtherRun = true
		return nil
	})
	defer restore()

	c.Assert(boot.FirstBoot(), IsNil)
	c.Assert(firstBootEnableFirstEtherRun, Equals, false)
}

func (s *FirstBootTestSuite) TestFirstBootnableEther(c *C) {
	release.MockOnClassic(false)
	firstBootEnableFirstEtherRun := false
	restore := boot.MockFirstbootInitialNetworkConfig(func() error {
		firstBootEnableFirstEtherRun = true
		return nil
	})
	defer restore()

	c.Assert(boot.FirstBoot(), IsNil)
	c.Assert(firstBootEnableFirstEtherRun, Equals, true)
}
