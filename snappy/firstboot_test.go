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
	"time"

	. "launchpad.net/gocheck"
	"launchpad.net/snappy/progress"
)

type fakePart struct {
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

// if we ha smaller interfaces this wouldn't be needed
func (p *fakePart) Channel() string {
	return ""
}

func (p *fakePart) Date() time.Time {
	return time.Now()
}

func (p *fakePart) Description() string {
	return ""
}

func (p *fakePart) DownloadSize() int64 {
	return 0
}

func (p *fakePart) InstalledSize() int64 {
	return 0
}

func (p *fakePart) Version() string {
	return ""
}

func (p *fakePart) Name() string {
	return ""
}

func (p *fakePart) Hash() string {
	return ""
}

func (p *fakePart) Icon() string {
	return ""
}

func (p *fakePart) IsActive() bool {
	return true
}

func (p *fakePart) NeedsReboot() bool {
	return false
}

func (p *fakePart) IsInstalled() bool {
	return true
}

func (p *fakePart) SetActive() error {
	return nil
}

func (p *fakePart) Install(progress.Meter, InstallFlags) (string, error) {
	return "", nil
}

func (p *fakePart) Uninstall() error {
	return nil
}

// end of stubbing

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
}

func (s *FirstBootTestSuite) TearDownTest(c *C) {
	activeSnapByName = ActiveSnapByName
	installedSnapsByType = InstalledSnapsByType
}

func (s *FirstBootTestSuite) TestFirstBootConfigure(c *C) {
	fakeOem := fakePart{oemConfig: s.oemConfig, snapType: SnapTypeOem}
	installedSnapsByType = func(snapsTs ...SnapType) ([]Part, error) {
		return []Part{&fakeOem}, nil
	}

	fakeMyApp := fakePart{snapType: SnapTypeApp}
	activeSnapByName = func(needle string) Part {
		return &fakeMyApp
	}

	c.Assert(OemConfig(), IsNil)
	myAppConfig := fmt.Sprintf("config:\n  myapp:\n    hostname: myhostname\n")
	c.Assert(string(fakeMyApp.config), Equals, myAppConfig)

	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)
}

func (s *FirstBootTestSuite) TestTwoRuns(c *C) {
	fakeOem := fakePart{oemConfig: s.oemConfig, snapType: SnapTypeOem}
	installedSnapsByType = func(snapsTs ...SnapType) ([]Part, error) {
		return []Part{&fakeOem, &fakeOem}, nil
	}

	fakeMyApp := fakePart{snapType: SnapTypeApp}
	activeSnapByName = func(needle string) Part {
		return &fakeMyApp
	}

	c.Assert(OemConfig(), IsNil)
	_, err := os.Stat(stampFile)
	c.Assert(err, IsNil)

	c.Assert(OemConfig(), Equals, ErrFirstBootRan)
}
