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
)

type fakePart struct {
	SnapPart
	config    []byte
	oemConfig SystemConfig
	snapType  pkg.Type
}

func (p *fakePart) Config(b []byte) (string, error) {
	p.config = b
	return "", nil
}

func (p *fakePart) OemConfig() SystemConfig {
	return p.oemConfig
}

func (p *fakePart) Type() pkg.Type {
	return p.snapType
}

type FirstBootTestSuite struct {
	oemConfig map[string]interface{}
	globs     []string
	ethdir    string
	m         *packageYaml
	e         error
	pkgmap    map[string]Part
	pkgmaperr error
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	stampFile = filepath.Join(c.MkDir(), "stamp")

	configMyApp := make(SystemConfig)
	configMyApp["hostname"] = "myhostname"

	s.oemConfig = make(SystemConfig)
	s.oemConfig["myapp"] = configMyApp

	s.globs = globs
	globs = nil
	s.ethdir = ethdir
	ethdir = c.MkDir()
	getOem = s.getOem
	newPkgmap = s.newPkgmap

	s.m = nil
	s.e = nil
	s.pkgmap = nil
	s.pkgmaperr = nil
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	globs = s.globs
	ethdir = s.ethdir
	getOem = getOemImpl
	newPkgmap = newPkgmapImpl
}

func (s *FirstBootTestSuite) getOem() (*packageYaml, error) {
	return s.m, s.e
}

func (s *FirstBootTestSuite) newPkgmap() (map[string]Part, error) {
	return s.pkgmap, s.pkgmaperr
}

func (s *FirstBootTestSuite) newFakeApp() *fakePart {
	fakeMyApp := fakePart{snapType: pkg.TypeApp}
	s.pkgmap = make(map[string]Part)
	s.pkgmap["myapp"] = &fakeMyApp

	return &fakeMyApp
}

func (s *FirstBootTestSuite) TestFirstBootConfigure(c *C) {
	s.m = &packageYaml{Config: s.oemConfig}
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

	s.m = &packageYaml{OEM: OEM{Software: Software{BuiltIn: []string{name}}}}

	repo := NewMetaLocalRepository()
	all, err := repo.All()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsInstalled(), Equals, true)
	c.Check(all[0].IsActive(), Equals, false)

	s.pkgmap = map[string]Part{name: all[0]}
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

func (s *FirstBootTestSuite) TestNoErrorWhenNoOEM(c *C) {
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

func (s *FirstBootTestSuite) TestEnableFirstEtherOemNoIfup(c *C) {
	s.m = &packageYaml{OEM: OEM{SkipIfupProvisioning: true}}
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
