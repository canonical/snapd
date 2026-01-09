// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package symlinks_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/symlinks"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendSuite struct {
	Backend         interfaces.SecurityBackend
	Repo            *interfaces.Repository
	RootDir         string
	restoreSanitize func()

	testutil.BaseTest
}

func (s *backendSuite) SetUpTest(c *C) {
	// Isolate this test to a temporary directory
	s.RootDir = c.MkDir()
	os.Mkdir(filepath.Join(s.RootDir, "/snap"), 0o755)
	dirs.SetRootDir(s.RootDir)

	// Create a fresh repository for each test
	s.Repo = interfaces.NewRepository()

	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})

	s.Backend = &symlinks.Backend{}
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	s.restoreSanitize()
}

var _ = Suite(&backendSuite{})

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecuritySymlinks)
}

func (s *backendSuite) mockSlot(c *C, yaml string, slotName string) (*interfaces.SnapAppSet, *snap.SlotInfo) {
	info := snaptest.MockInfo(c, yaml, nil)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.Repo.AddAppSet(set)
	c.Assert(err, IsNil)

	if slotInfo, ok := info.Slots[slotName]; ok {
		return set, slotInfo
	}
	panic(fmt.Sprintf("cannot find slot %q in snap %q", slotName, info.InstanceName()))
}

func (s *backendSuite) mockPlugs(c *C, yaml string, plugNames []string) (*interfaces.SnapAppSet, []*snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, nil)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.Repo.AddAppSet(set)
	c.Assert(err, IsNil)

	plugInfos := make([]*snap.PlugInfo, 0, len(plugNames))
	for _, plug := range plugNames {
		if plugInfo, ok := info.Plugs[plug]; ok {
			plugInfos = append(plugInfos, plugInfo)
			continue
		}
		panic(fmt.Sprintf("cannot find plug %q in snap %q", plug, info.InstanceName()))
	}
	return set, plugInfos
}

const someProvider1 = `name: some1
version: 0
type: app
slots:
  some-driver-libs:
`

const someProvider2 = `name: some2
version: 0
type: app
slots:
  some-driver-libs:
`

const otherProvider = `name: other
version: 0
type: app
slots:
  other-driver-libs:
`

const someConsumer = `name: snapd
version: 0
type: snapd
apps:
  app:
    plugs: [some-driver-libs, other-driver-libs]
`

func checkSymlink(c *C, target, name string) {
	t, err := os.Readlink(filepath.Join(dirs.GlobalRootDir, name))
	c.Assert(err, IsNil)
	c.Assert(t, Equals, filepath.Join(dirs.GlobalRootDir, target))
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"mediated-symlinks"})
}

func (s *backendSuite) TestConnectDisconnect(c *C) {
	controlledDir := filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo")
	// Add callback and register the interface
	iface := &ifacetest.TestSymlinksInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName: "some-driver-libs",
			SymlinksConnectedPlugCallback: func(spec *symlinks.Specification,
				plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				switch slot.Snap().InstanceName() {
				case "some1":
					return spec.AddSymlink(
						filepath.Join(dirs.GlobalRootDir, "/snap/somesnap1/1/target.so"),
						filepath.Join(controlledDir, "bar.so"))
				case "some2":
					return spec.AddSymlink(
						filepath.Join(dirs.GlobalRootDir, "/snap/somesnap2/1/target2.so"),
						filepath.Join(controlledDir, "bar2.so"))
				}
				return errors.New("unexpected snap")
			},
		},
		DirectoriesCallback: func() []string {
			return []string{controlledDir}
		},
	}
	c.Assert(s.Repo.AddInterface(iface), IsNil)

	// Create some un-controlled files
	c.Assert(os.MkdirAll(controlledDir, 0o755), IsNil)
	noSnapdLinkPath := filepath.Join(controlledDir, "nosnapd-link")
	noSnapdFilePath := filepath.Join(controlledDir, "nosnapd-file")
	c.Assert(os.Symlink("/var/lib/target", noSnapdLinkPath), IsNil)
	c.Assert(os.WriteFile(noSnapdFilePath, []byte{}, 0o644), IsNil)

	// Mock plug/slots
	appSet, plugInfos := s.mockPlugs(c, someConsumer, []string{"some-driver-libs"})
	_, slotInfo1 := s.mockSlot(c, someProvider1, "some-driver-libs")
	_, slotInfo2 := s.mockSlot(c, someProvider2, "some-driver-libs")

	// Connect them
	connRef1 := interfaces.NewConnRef(plugInfos[0], slotInfo1)
	_, err := s.Repo.Connect(connRef1, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	connRef2 := interfaces.NewConnRef(plugInfos[0], slotInfo2)
	_, err = s.Repo.Connect(connRef2, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Set-up the backend
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, nil), IsNil)

	checkSymlink(c, "/snap/somesnap1/1/target.so", "/usr/lib/foo/bar.so")
	checkSymlink(c, "/snap/somesnap2/1/target2.so", "/usr/lib/foo/bar2.so")

	// Now disconnect the first slot and set-up backends again
	c.Assert(s.Repo.Disconnect(plugInfos[0].Snap.InstanceName(), plugInfos[0].Name,
		slotInfo1.Snap.InstanceName(), slotInfo1.Name), IsNil)
	s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, nil)

	// Only symlinks for the connected slots are around
	c.Check(filepath.Join(controlledDir, "bar.so"), testutil.LFileAbsent)
	checkSymlink(c, "/snap/somesnap2/1/target2.so", "/usr/lib/foo/bar2.so")

	// Uncontrolled files are around
	c.Check(noSnapdLinkPath, testutil.LFilePresent)
	c.Check(noSnapdFilePath, testutil.LFilePresent)
}

func (s *backendSuite) TestTwoPlugs(c *C) {
	// Add interfaces
	iface1 := &ifacetest.TestSymlinksInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName: "some-driver-libs",
			SymlinksConnectedPlugCallback: func(spec *symlinks.Specification,
				plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				return spec.AddSymlink(
					filepath.Join(dirs.GlobalRootDir, "/snap/somesnap/1/target.so"),
					filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo/bar.so"))
			},
		},
		DirectoriesCallback: func() []string {
			return []string{filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo")}
		},
	}
	c.Assert(s.Repo.AddInterface(iface1), IsNil)
	iface2 := &ifacetest.TestSymlinksInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName: "other-driver-libs",
			SymlinksConnectedPlugCallback: func(spec *symlinks.Specification,
				plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				return spec.AddSymlink(
					filepath.Join(dirs.GlobalRootDir, "/snap/somesnap2/1/target2.so"),
					filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo2/bar2.so"))
			},
		},
		DirectoriesCallback: func() []string {
			return []string{filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo2")}
		},
	}
	c.Assert(s.Repo.AddInterface(iface2), IsNil)

	// Mock plugs/slots
	appSet, plugInfos := s.mockPlugs(c, someConsumer, []string{"some-driver-libs", "other-driver-libs"})
	_, slotInfo1 := s.mockSlot(c, someProvider1, "some-driver-libs")
	_, slotInfo2 := s.mockSlot(c, otherProvider, "other-driver-libs")

	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, nil), IsNil)

	// Connect them
	connRef1 := interfaces.NewConnRef(plugInfos[0], slotInfo1)
	_, err := s.Repo.Connect(connRef1, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	connRef2 := interfaces.NewConnRef(plugInfos[1], slotInfo2)
	_, err = s.Repo.Connect(connRef2, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Set-up the backend
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, nil), IsNil)

	checkSymlink(c, "/snap/somesnap/1/target.so", "/usr/lib/foo/bar.so")
	checkSymlink(c, "/snap/somesnap2/1/target2.so", "/usr/lib/foo2/bar2.so")

	// Now disconnect the first slot and set-up backends again
	c.Assert(s.Repo.Disconnect(plugInfos[0].Snap.InstanceName(), plugInfos[0].Name,
		slotInfo1.Snap.InstanceName(), slotInfo1.Name), IsNil)
	s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, nil)

	// Only symlinks for the connected slots are around
	c.Check(filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo/bar.so"), testutil.LFileAbsent)
	checkSymlink(c, "/snap/somesnap2/1/target2.so", "/usr/lib/foo2/bar2.so")
}

func (s *backendSuite) TestUnregisteredDirectory(c *C) {
	// Add callback and register the interface
	iface := &ifacetest.TestSymlinksInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName: "some-driver-libs",
			SymlinksConnectedPlugCallback: func(spec *symlinks.Specification,
				plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				return spec.AddSymlink(
					filepath.Join(dirs.GlobalRootDir, "/snap/somesnap2/1/target2.so"),
					filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo2/bar2.so"))
			},
		},
		DirectoriesCallback: func() []string {
			return []string{filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo")}
		},
	}
	c.Assert(s.Repo.AddInterface(iface), IsNil)

	// Mock plug/slots
	appSet, plugInfos := s.mockPlugs(c, someConsumer, []string{"some-driver-libs"})
	_, slotInfo1 := s.mockSlot(c, someProvider1, "some-driver-libs")

	// Connect
	connRef1 := interfaces.NewConnRef(plugInfos[0], slotInfo1)
	_, err := s.Repo.Connect(connRef1, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Set-up the backend
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, s.Repo, nil), ErrorMatches,
		`internal error: .*/usr/lib/foo2 not in any registered symlinks directory`)

	c.Check(filepath.Join(dirs.GlobalRootDir, "/usr/lib/foo2/bar2.so"), testutil.LFileAbsent)
}
