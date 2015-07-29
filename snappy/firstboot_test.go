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

	"launchpad.net/snappy/pkg"
	"launchpad.net/snappy/systemd"
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
	sctlargs  []string
	sctlerr   error
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) systemctl(args ...string) (out []byte, err error) {
	s.sctlargs = args
	return nil, s.sctlerr
}

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	s.sctlerr = nil
	s.sctlargs = nil
	systemd.SystemctlCmd = s.systemctl
	stampFile = filepath.Join(c.MkDir(), "stamp")

	configMyApp := make(SystemConfig)
	configMyApp["hostname"] = "myhostname"

	s.oemConfig = make(SystemConfig)
	s.oemConfig["myapp"] = configMyApp

	s.mockActiveSnapNamesByType()

	s.globs = globs
	globs = nil
	s.ethdir = ethdir
	ethdir = c.MkDir()
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	activeSnapByName = ActiveSnapByName
	activeSnapsByType = ActiveSnapsByType
	globs = s.globs
	ethdir = s.ethdir
}

func (s *FirstBootTestSuite) mockActiveSnapNamesByType() *fakePart {
	fakeOem := fakePart{oemConfig: s.oemConfig, snapType: pkg.TypeOem}
	activeSnapsByType = func(snapsTs ...pkg.Type) ([]Part, error) {
		return []Part{&fakeOem}, nil
	}

	return &fakeOem
}

func (s *FirstBootTestSuite) mockActiveSnapByName() *fakePart {
	fakeMyApp := fakePart{snapType: pkg.TypeApp}
	activeSnapByName = func(needle string) Part {
		return &fakeMyApp
	}

	return &fakeMyApp
}

func (s *FirstBootTestSuite) TestFirstBootConfigure(c *C) {
	fakeMyApp := s.mockActiveSnapByName()

	c.Assert(FirstBoot(), IsNil)
	myAppConfig := fmt.Sprintf("config:\n  myapp:\n    hostname: myhostname\n")
	c.Assert(string(fakeMyApp.config), Equals, myAppConfig)

	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestTwoRuns(c *C) {
	s.mockActiveSnapByName()

	c.Assert(FirstBoot(), IsNil)
	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)

	c.Assert(FirstBoot(), Equals, ErrNotFirstBoot)
}

func (s *FirstBootTestSuite) TestNoErrorWhenNoOEM(c *C) {
	activeSnapsByType = func(snapsTs ...pkg.Type) ([]Part, error) {
		return nil, nil
	}

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
	c.Check(string(bs), Equals, "auto eth42\n\nallow-hotplug eth42\niface eth42 inet dhcp\n")

	c.Check(s.sctlargs, DeepEquals, []string{"restart", "networking", "--no-block"})
}

func (s *FirstBootTestSuite) TestEnableFirstEtherSomeEthFailsIfSystemctlFails(c *C) {
	dir := c.MkDir()
	_, err := os.Create(filepath.Join(dir, "eth42"))
	c.Assert(err, IsNil)
	s.sctlerr = fmt.Errorf("Error")

	globs = []string{filepath.Join(dir, "eth*")}
	c.Check(enableFirstEther(), Equals, s.sctlerr)
	fs, _ := filepath.Glob(filepath.Join(ethdir, "*"))
	c.Check(fs, HasLen, 1)
	c.Check(s.sctlargs, DeepEquals, []string{"restart", "networking", "--no-block"})
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
