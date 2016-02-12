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
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
)

type fakeOverlord struct {
	configs map[string]string
}

func (o *fakeOverlord) Configure(s *SnapPart, c []byte) (string, error) {
	o.configs[s.Name()] = string(c)
	return string(c), nil
}

type FirstBootTestSuite struct {
	gadgetConfig map[string]interface{}
	globs        []string
	ethdir       string
	ifup         string
	m            *snapYaml
	e            error
	partMap      map[string]Part
	partMapErr   error
	verifyCmd    string
	fakeOverlord *fakeOverlord
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)
	os.MkdirAll(dirs.SnapSnapsDir, 0755)
	stampFile = filepath.Join(c.MkDir(), "stamp")

	// mock the world!
	makeMockSecurityEnv(c)
	runAppArmorParser = mockRunAppArmorParser
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	err := os.MkdirAll(filepath.Join(tempdir, "etc", "systemd", "system", "multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)

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
	newOverlord = s.newOverlord
	s.fakeOverlord = &fakeOverlord{
		configs: map[string]string{},
	}

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
}

func (s *FirstBootTestSuite) getGadget() (*snapYaml, error) {
	return s.m, s.e
}

func (s *FirstBootTestSuite) newPartMap() (map[string]Part, error) {
	return s.partMap, s.partMapErr
}

func (s *FirstBootTestSuite) newOverlord() configurator {
	return s.fakeOverlord
}

func (s *FirstBootTestSuite) newFakeApp() *SnapPart {
	fakeMyApp := SnapPart{
		m: &snapYaml{
			Name: "myapp",
			Type: snap.TypeApp,
		},
	}
	s.partMap = make(map[string]Part)
	s.partMap["myapp"] = &fakeMyApp

	return &fakeMyApp
}

func (s *FirstBootTestSuite) TestFirstBootConfigure(c *C) {
	s.m = &snapYaml{Config: s.gadgetConfig}
	s.newFakeApp()
	c.Assert(FirstBoot(), IsNil)
	myAppConfig := fmt.Sprintf("config:\n  myapp:\n    hostname: myhostname\n")
	c.Assert(s.fakeOverlord.configs["myapp"], Equals, myAppConfig)

	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestSoftwareActivate(c *C) {
	yamlPath, err := makeInstalledMockSnap(dirs.GlobalRootDir, "")
	c.Assert(err, IsNil)

	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(part.IsActive(), Equals, false)
	name := part.Name()

	s.m = &snapYaml{Gadget: Gadget{Software: Software{BuiltIn: []string{name}}}}

	all, err := NewLocalSnapRepository().All()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].Name(), Equals, name)
	c.Check(all[0].IsInstalled(), Equals, true)
	c.Check(all[0].IsActive(), Equals, false)

	s.partMap = map[string]Part{name: all[0]}
	c.Assert(FirstBoot(), IsNil)

	all, err = NewLocalSnapRepository().All()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].Name(), Equals, name)
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
	s.m = &snapYaml{Gadget: Gadget{SkipIfupProvisioning: true}}
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

var mockOSYaml = `
name: ubuntu-core
version: 1.0
type: os
`

var mockKernelYaml = `
name: canonical-linux-pc
version: 1.0
type: kernel
`

func (s *FirstBootTestSuite) ensureSystemSnapIsEnabledOnFirstBoot(c *C, yaml string, expectActivated bool) {
	_, err := makeInstalledMockSnap(dirs.GlobalRootDir, yaml)
	c.Assert(err, IsNil)

	all, err := NewLocalSnapRepository().All()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsInstalled(), Equals, true)
	c.Check(all[0].IsActive(), Equals, false)

	c.Assert(FirstBoot(), IsNil)

	all, err = NewLocalSnapRepository().All()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsInstalled(), Equals, true)
	c.Check(all[0].IsActive(), Equals, expectActivated)
}

func (s *FirstBootTestSuite) TestSystemSnapsEnablesOS(c *C) {
	s.ensureSystemSnapIsEnabledOnFirstBoot(c, mockOSYaml, true)
}

func (s *FirstBootTestSuite) TestSystemSnapsEnablesKernel(c *C) {
	s.m = &snapYaml{Gadget: Gadget{Hardware: Hardware{Bootloader: "grub"}}}

	s.ensureSystemSnapIsEnabledOnFirstBoot(c, mockKernelYaml, true)
}

func (s *FirstBootTestSuite) TestSystemSnapsDoesNotEnableApps(c *C) {
	s.ensureSystemSnapIsEnabledOnFirstBoot(c, "", false)
}
