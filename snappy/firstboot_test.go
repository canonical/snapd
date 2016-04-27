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
	"github.com/ubuntu-core/snappy/snap/legacygadget"
	"github.com/ubuntu-core/snappy/systemd"
)

type fakeOverlord struct {
	configs map[string]string
}

func (o *fakeOverlord) Configure(s *Snap, c []byte) ([]byte, error) {
	o.configs[s.Name()] = string(c)
	return c, nil
}

type FirstBootTestSuite struct {
	gadgetConfig map[string]interface{}
	globs        []string
	ethdir       string
	ifup         string
	m            *snap.LegacyYaml
	e            error
	snapMap      map[string]*Snap
	snapMapErr   error
	fakeOverlord *fakeOverlord
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	tempdir := c.MkDir()
	dirs.SetRootDir(tempdir)
	os.MkdirAll(dirs.SnapSnapsDir, 0755)
	stampFile = filepath.Join(c.MkDir(), "stamp")

	// mock the world!
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		return []byte("ActiveState=inactive\n"), nil
	}

	err := os.MkdirAll(filepath.Join(tempdir, "etc", "systemd", "system", "multi-user.target.wants"), 0755)
	c.Assert(err, IsNil)

	configMyApp := make(legacygadget.SystemConfig)
	configMyApp["hostname"] = "myhostname"

	s.gadgetConfig = make(legacygadget.SystemConfig)
	s.gadgetConfig["myapp"] = configMyApp

	s.globs = globs
	globs = nil
	s.ethdir = ethdir
	ethdir = c.MkDir()
	s.ifup = ifup
	ifup = "/bin/true"
	getGadget = s.getGadget
	newSnapMap = s.newSnapMap
	newOverlord = s.newOverlord
	s.fakeOverlord = &fakeOverlord{
		configs: map[string]string{},
	}

	s.m = nil
	s.e = nil
	s.snapMap = nil
	s.snapMapErr = nil
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	globs = s.globs
	ethdir = s.ethdir
	ifup = s.ifup
	getGadget = getGadgetImpl
	newSnapMap = newSnapMapImpl
}

func (s *FirstBootTestSuite) getGadget() (*snap.Info, error) {
	if s.m != nil {
		info := &snap.Info{
			Legacy: s.m,
		}
		return info, nil
	}
	return nil, s.e
}

func (s *FirstBootTestSuite) newSnapMap() (map[string]*Snap, error) {
	return s.snapMap, s.snapMapErr
}

func (s *FirstBootTestSuite) newOverlord() configurator {
	return s.fakeOverlord
}

func (s *FirstBootTestSuite) newFakeApp() *Snap {
	fakeMyApp := Snap{
		info: &snap.Info{
			SuggestedName: "myapp",
			Type:          snap.TypeApp,
		},
	}
	s.snapMap = make(map[string]*Snap)
	s.snapMap["myapp"] = &fakeMyApp

	return &fakeMyApp
}

func (s *FirstBootTestSuite) TestFirstBootConfigure(c *C) {
	s.m = &snap.LegacyYaml{Config: s.gadgetConfig}
	s.newFakeApp()
	c.Assert(FirstBoot(), IsNil)
	myAppConfig := fmt.Sprintf("config:\n  myapp:\n    hostname: myhostname\n")
	c.Assert(s.fakeOverlord.configs["myapp"], Equals, myAppConfig)

	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestSoftwareActivate(c *C) {
	yamlPath, err := makeInstalledMockSnap("", 11)
	c.Assert(err, IsNil)

	snp, err := NewInstalledSnap(yamlPath)
	c.Assert(err, IsNil)
	c.Assert(snp.IsActive(), Equals, false)
	name := snp.Name()

	s.m = &snap.LegacyYaml{Gadget: legacygadget.Gadget{Software: legacygadget.Software{BuiltIn: []string{name}}}}

	all, err := (&Overlord{}).Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].Name(), Equals, name)
	c.Check(all[0].IsActive(), Equals, false)

	s.snapMap = map[string]*Snap{name: all[0]}
	c.Assert(FirstBoot(), IsNil)

	all, err = (&Overlord{}).Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].Name(), Equals, name)
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
	s.m = &snap.LegacyYaml{Gadget: legacygadget.Gadget{SkipIfupProvisioning: true}}
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
	_, err := makeInstalledMockSnap(yaml, 11)
	c.Assert(err, IsNil)

	all, err := (&Overlord{}).Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsActive(), Equals, false)

	c.Assert(FirstBoot(), IsNil)

	all, err = (&Overlord{}).Installed()
	c.Check(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Check(all[0].IsActive(), Equals, expectActivated)
}

func (s *FirstBootTestSuite) TestSystemSnapsEnablesOS(c *C) {
	s.ensureSystemSnapIsEnabledOnFirstBoot(c, mockOSYaml, true)
}

func (s *FirstBootTestSuite) TestSystemSnapsEnablesKernel(c *C) {
	s.m = &snap.LegacyYaml{Gadget: legacygadget.Gadget{Hardware: legacygadget.Hardware{Bootloader: "grub"}}}

	s.ensureSystemSnapIsEnabledOnFirstBoot(c, mockKernelYaml, true)
}

func (s *FirstBootTestSuite) TestSystemSnapsDoesEnableApps(c *C) {
	s.ensureSystemSnapIsEnabledOnFirstBoot(c, "", true)
}
