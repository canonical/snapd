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
	"os"
	"path/filepath"

	. "launchpad.net/gocheck"
)

type fakePart struct {
	SnapPart
	config    []byte
	oemConfig SystemConfig
	snapType  SnapType
}

func (p *fakePart) Config(b []byte) (string, error) {
	p.config = b
	return "", nil
}

func (p *fakePart) OemConfig() SystemConfig {
	return p.oemConfig
}

func (p *fakePart) Type() SnapType {
	return p.snapType
}

type FirstBootTestSuite struct {
	oemConfig map[string]interface{}
}

var _ = Suite(&FirstBootTestSuite{})

func (s *FirstBootTestSuite) SetUpTest(c *C) {
	stampFile = filepath.Join(c.MkDir(), "stamp")

	configMyApp := make(SystemConfig)
	configMyApp["hostname"] = "myhostname"

	s.oemConfig = make(SystemConfig)
	s.oemConfig["myapp"] = configMyApp

	s.mockInstalledSnapNamesByType()
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	activeSnapByName = ActiveSnapByName
	installedSnapsByType = InstalledSnapsByType
}

func (s *FirstBootTestSuite) mockInstalledSnapNamesByType() *fakePart {
	fakeOem := fakePart{oemConfig: s.oemConfig, snapType: SnapTypeOem}
	installedSnapsByType = func(snapsTs ...SnapType) ([]Part, error) {
		return []Part{&fakeOem, &fakeOem}, nil
	}

	return &fakeOem
}

func (s *FirstBootTestSuite) mockActiveSnapByName() *fakePart {
	fakeMyApp := fakePart{snapType: SnapTypeApp}
	activeSnapByName = func(needle string) Part {
		return &fakeMyApp
	}

	return &fakeMyApp
}

func (s *FirstBootTestSuite) TestFirstBootConfigure(c *C) {
	fakeMyApp := s.mockActiveSnapByName()

	c.Assert(OemConfig(), IsNil)
	myAppConfig := fmt.Sprintf("config:\n  myapp:\n    hostname: myhostname\n")
	c.Assert(string(fakeMyApp.config), Equals, myAppConfig)

	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestTwoRuns(c *C) {
	s.mockActiveSnapByName()

	c.Assert(OemConfig(), IsNil)
	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)

	c.Assert(OemConfig(), Equals, ErrNotFirstBoot)
}
