// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package bind_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/bind"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendSuite struct {
	backend *bind.Backend
	repo    *interfaces.Repository
	iface   *interfaces.TestInterface
	rootDir string
}

var _ = Suite(&backendSuite{backend: &bind.Backend{}})

func (s *backendSuite) SetUpTest(c *C) {
	s.rootDir = c.MkDir()
	dirs.SetRootDir(s.rootDir)

	err := os.MkdirAll(dirs.SnapBindMountPolicyDir, 0700)
	c.Assert(err, IsNil)

	s.repo = interfaces.NewRepository()
	s.iface = &interfaces.TestInterface{InterfaceName: "iface"}
	err = s.repo.AddInterface(s.iface)
	c.Assert(err, IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *backendSuite) TestName(c *C) {
	c.Check(s.backend.Name(), Equals, "bind")
}

func (s *backendSuite) TestRemove(c *C) {
	canaryToGo := filepath.Join(dirs.SnapBindMountPolicyDir, "snap.hello-world.hello-world.bind")
	err := ioutil.WriteFile(canaryToGo, []byte("ni! ni! ni!"), 0644)
	c.Assert(err, IsNil)

	canaryToStay := filepath.Join(dirs.SnapBindMountPolicyDir, "snap.i-stay.really.bind")
	err = ioutil.WriteFile(canaryToStay, []byte("stay!"), 0644)
	c.Assert(err, IsNil)

	err = s.backend.Remove("hello-world")
	c.Assert(err, IsNil)

	c.Assert(osutil.FileExists(canaryToGo), Equals, false)
	content, err := ioutil.ReadFile(canaryToStay)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "stay!")
}

var mockSnapYaml = `name: snap-name
version: 1
apps:
    app1:
    app2:
slots:
    iface:
`

func (s *backendSuite) TestSetupSetsup(c *C) {
	fsEntry := "/src-1 /dst-1 none bind,ro 0 0"
	s.iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(fsEntry), nil
	}

	// devMode is irrelevant for this security backend
	s.installSnap(c, false, mockSnapYaml)

	// FIXME: test combineSnipets implicitely somehow too
	fn1 := filepath.Join(dirs.SnapBindMountPolicyDir, "snap.snap-name.app1.bind")
	content, err := ioutil.ReadFile(fn1)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, fsEntry+"\n")

	fn2 := filepath.Join(dirs.SnapBindMountPolicyDir, "snap.snap-name.app2.bind")
	content, err = ioutil.ReadFile(fn2)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, fsEntry+"\n")

}

// COPIED CODE OMG
func (s *backendSuite) installSnap(c *C, devMode bool, snapYaml string) *snap.Info {
	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, IsNil)
	s.addPlugsSlots(c, snapInfo)
	err = s.backend.Setup(snapInfo, devMode, s.repo)
	c.Assert(err, IsNil)
	return snapInfo
}

func (s *backendSuite) addPlugsSlots(c *C, snapInfo *snap.Info) {
	for _, plugInfo := range snapInfo.Plugs {
		plug := &interfaces.Plug{PlugInfo: plugInfo}
		err := s.repo.AddPlug(plug)
		c.Assert(err, IsNil)
	}
	for _, slotInfo := range snapInfo.Slots {
		slot := &interfaces.Slot{SlotInfo: slotInfo}
		err := s.repo.AddSlot(slot)
		c.Assert(err, IsNil)
	}
}

func (s *backendSuite) removePlugsSlots(c *C, snapInfo *snap.Info) {
	for _, plug := range s.repo.Plugs(snapInfo.Name()) {
		err := s.repo.RemovePlug(plug.Snap.Name(), plug.Name)
		c.Assert(err, IsNil)
	}
	for _, slot := range s.repo.Slots(snapInfo.Name()) {
		err := s.repo.RemoveSlot(slot.Snap.Name(), slot.Name)
		c.Assert(err, IsNil)
	}
}
