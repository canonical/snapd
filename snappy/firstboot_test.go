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

package snappy

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/clickdeb"
)

type fakePart struct {
	SnapPart
	config       []byte
	gadgetConfig SystemConfig
	snapType     pkg.Type
}

func (p *fakePart) Config(b []byte) (string, error) {
	p.config = b
	return "", nil
}

func (p *fakePart) GadgetConfig() SystemConfig {
	return p.gadgetConfig
}

func (p *fakePart) Type() pkg.Type {
	return p.snapType
}

type FirstBootTestSuite struct {
	gadgetConfig map[string]interface{}
	globs        []string
	ethdir       string
	ifup         string
	m            *packageYaml
	e            error
	partMap      map[string]Part
	partMapErr   error
	verifyCmd    string
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	s.verifyCmd = clickdeb.VerifyCmd
	clickdeb.VerifyCmd = "true"

	dirs.SetRootDir(c.MkDir())
	stampFile = filepath.Join(c.MkDir(), "stamp")

	configMyApp := make(SystemConfig)
	configMyApp["hostname"] = "myhostname"

	s.gadgetConfig = make(SystemConfig)
	s.gadgetConfig["myapp"] = configMyApp

	s.globs = globs
	globs = nil
	s.ethdir = ethdir
	ethdir = c.MkDir()
	s.ifup = ifup
	ifup = "/bin/true"
	getGadget = s.getGadget
	newPartMap = s.newPartMap

	s.m = nil
	s.e = nil
	s.partMap = nil
	s.partMapErr = nil
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	globs = s.globs
	ethdir = s.ethdir
	ifup = s.ifup
	getGadget = getGadgetImpl
	newPartMap = newPartMapImpl
	clickdeb.VerifyCmd = s.verifyCmd
}

func (s *FirstBootTestSuite) getGadget() (*packageYaml, error) {
	return s.m, s.e
}

func (s *FirstBootTestSuite) newPartMap() (map[string]Part, error) {
	return s.partMap, s.partMapErr
}

func (s *FirstBootTestSuite) newFakeApp() *fakePart {
	fakeMyApp := fakePart{snapType: pkg.TypeApp}
	s.partMap = make(map[string]Part)
	s.partMap["myapp"] = &fakeMyApp

	return &fakeMyApp
}

func (s *FirstBootTestSuite) TestFirstBootConfigure(c *C) {
	s.m = &packageYaml{Config: s.gadgetConfig}
	fakeMyApp := s.newFakeApp()

	c.Assert(FirstBoot(), IsNil)
	myAppConfig := fmt.Sprintf("config:\n  myapp:\n    hostname: myhostname\n")
	c.Assert(string(fakeMyApp.config), Equals, myAppConfig)

	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestSoftwareActivate(c *C) {
	snapFile := makeTestSnapPackage(c, "")
	name, err := Install(snapFile, AllowUnauthenticated|DoInstallGC|InhibitHooks, &MockProgressMeter{})
	c.Check(err, IsNil)

	s.m = &packageYaml{Gadget: Gadget{Software: Software{BuiltIn: []string{name}}}}

	repo := NewMetaLocalRepository()
	all, err := repo.All()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsInstalled(), Equals, true)
	c.Check(all[0].IsActive(), Equals, false)

	s.partMap = map[string]Part{name: all[0]}
	c.Assert(FirstBoot(), IsNil)

	repo = NewMetaLocalRepository()
	all, err = repo.All()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsInstalled(), Equals, true)
	c.Check(all[0].IsActive(), Equals, true)
}

func (s *FirstBootTestSuite) TestTwoRuns(c *C) {
	c.Assert(FirstBoot(), IsNil)
	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)

	c.Assert(FirstBoot(), Equals, ErrNotFirstBoot)
}

func (s *FirstBootTestSuite) TestNoErrorWhenNoGadget(c *C) {
	c.Assert(FirstBoot(), IsNil)
	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestEnableFirstEther(c *C) {
	c.Check(enableFirstEther(), IsNil)
	fs, _ := filepath.Glob(filepath.Join(ethdir, "*"))
	c.Assert(fs, HasLen, 0)
}

func (s *FirstBootTestSuite) TestEnableFirstEtherSomeEth(c *C) {
	dir := c.MkDir()
	_, err := os.Create(filepath.Join(dir, "eth42"))
	c.Assert(err, IsNil)

	globs = []string{filepath.Join(dir, "eth*")}
	c.Check(enableFirstEther(), IsNil)
	fs, _ := filepath.Glob(filepath.Join(ethdir, "*"))
	c.Assert(fs, HasLen, 1)
	bs, err := ioutil.ReadFile(fs[0])
	c.Assert(err, IsNil)
	c.Check(string(bs), Equals, "allow-hotplug eth42\niface eth42 inet dhcp\n")

}

func (s *FirstBootTestSuite) TestEnableFirstEtherGadgetNoIfup(c *C) {
	s.m = &packageYaml{Gadget: Gadget{SkipIfupProvisioning: true}}
	dir := c.MkDir()
	_, err := os.Create(filepath.Join(dir, "eth42"))
	c.Assert(err, IsNil)

	globs = []string{filepath.Join(dir, "eth*")}
	c.Check(enableFirstEther(), IsNil)
	fs, _ := filepath.Glob(filepath.Join(ethdir, "*"))
	c.Assert(fs, HasLen, 0)
}

func (s *FirstBootTestSuite) TestEnableFirstEtherBadEthDir(c *C) {
	dir := c.MkDir()
	_, err := os.Create(filepath.Join(dir, "eth42"))
	c.Assert(err, IsNil)

	ethdir = "/no/such/thing"
	globs = []string{filepath.Join(dir, "eth*")}
	err = enableFirstEther()
	c.Check(err, NotNil)
	c.Check(os.IsNotExist(err), Equals, true)
}
