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

package configfiles_test

import (
	"fmt"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/configfiles"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
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
	dirs.SetRootDir(s.RootDir)

	// Create a fresh repository for each test
	s.Repo = interfaces.NewRepository()

	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})

	s.Backend = &configfiles.Backend{}
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	s.restoreSanitize()
}

var _ = Suite(&backendSuite{})

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityConfigfiles)
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

const eglProvider1 = `name: egl1
version: 0
type: app
slots:
  egl-driver-libs:
`

const eglProvider2 = `name: egl2
version: 0
type: app
slots:
  egl-driver-libs:
`

const otherProvider = `name: other
version: 0
type: app
slots:
  other-driver-libs:
`

const eglConsumer = `name: snapd
version: 0
type: snapd
apps:
  app:
    plugs: [egl-driver-libs, other-driver-libs]
`

func checkConfigfilesFile(c *C, path, content string) {
	c.Assert(filepath.Join(dirs.GlobalRootDir, path), testutil.FileEquals, content)
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{"mediated-configfiles"})
}

func (s *backendSuite) TestConnectDisconnect(c *C) {
	// Add callback and register the interface
	iface := &ifacetest.TestConfigFilesInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName: "egl-driver-libs",
			ConfigfilesConnectedPlugCallback: func(spec *configfiles.Specification,
				plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				switch slot.Snap().InstanceName() {
				case "egl1":
					spec.AddPathContent(
						filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.a.conf"),
						&osutil.MemoryFileState{Content: []byte("aaaa"), Mode: 0655})
				case "egl2":
					spec.AddPathContent(
						filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.b.conf"),
						&osutil.MemoryFileState{Content: []byte("bbbb"), Mode: 0655})
				}
				return nil
			},
		},
		PathPatternsCallback: func() []string {
			return []string{filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.*.conf")}
		},
	}
	c.Assert(s.Repo.AddInterface(iface), IsNil)

	// Mock plug/slots
	appSet, plugInfos := s.mockPlugs(c, eglConsumer, []string{"egl-driver-libs"})
	_, slotInfo1 := s.mockSlot(c, eglProvider1, "egl-driver-libs")
	_, slotInfo2 := s.mockSlot(c, eglProvider2, "egl-driver-libs")

	// Connect them
	connRef1 := interfaces.NewConnRef(plugInfos[0], slotInfo1)
	_, err := s.Repo.Connect(connRef1, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	connRef2 := interfaces.NewConnRef(plugInfos[0], slotInfo2)
	_, err = s.Repo.Connect(connRef2, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Set-up the backend
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), IsNil)

	checkConfigfilesFile(c, "/etc/conf1.d/snap.a.conf", "aaaa")
	checkConfigfilesFile(c, "/etc/conf1.d/snap.b.conf", "bbbb")

	// Now disconnect the first slot and set-up backends again
	c.Assert(s.Repo.Disconnect(plugInfos[0].Snap.InstanceName(), plugInfos[0].Name,
		slotInfo1.Snap.InstanceName(), slotInfo1.Name), IsNil)
	s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil)

	// Only files for the connected slots are around
	c.Check(filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.a.conf"), testutil.FileAbsent)
	checkConfigfilesFile(c, "/etc/conf1.d/snap.b.conf", "bbbb")
}

func (s *backendSuite) TestTwoPlugs(c *C) {
	// Add interfaces
	iface1 := &ifacetest.TestConfigFilesInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName: "egl-driver-libs",
			ConfigfilesConnectedPlugCallback: func(spec *configfiles.Specification,
				plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				spec.AddPathContent(
					filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.a.conf"),
					&osutil.MemoryFileState{Content: []byte("a"), Mode: 0655})
				return nil
			},
		},
		PathPatternsCallback: func() []string {
			return []string{filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.*.conf")}
		},
	}
	c.Assert(s.Repo.AddInterface(iface1), IsNil)
	iface2 := &ifacetest.TestConfigFilesInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName: "other-driver-libs",
			ConfigfilesConnectedPlugCallback: func(spec *configfiles.Specification,
				plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				spec.AddPathContent(
					filepath.Join(dirs.GlobalRootDir, "/etc/conf2.d/snap.a.conf"),
					&osutil.MemoryFileState{Content: []byte("a"), Mode: 0655})
				return nil
			},
		},
		PathPatternsCallback: func() []string {
			return []string{filepath.Join(dirs.GlobalRootDir, "/etc/conf2.d/snap.*.conf")}
		},
	}
	c.Assert(s.Repo.AddInterface(iface2), IsNil)

	// Mock plugs/slots
	appSet, plugInfos := s.mockPlugs(c, eglConsumer, []string{"egl-driver-libs", "other-driver-libs"})
	_, slotInfo1 := s.mockSlot(c, eglProvider1, "egl-driver-libs")
	_, slotInfo2 := s.mockSlot(c, otherProvider, "other-driver-libs")

	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), IsNil)

	// Connect them
	connRef1 := interfaces.NewConnRef(plugInfos[0], slotInfo1)
	_, err := s.Repo.Connect(connRef1, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	connRef2 := interfaces.NewConnRef(plugInfos[1], slotInfo2)
	_, err = s.Repo.Connect(connRef2, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Set-up the backend
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), IsNil)

	checkConfigfilesFile(c, "/etc/conf1.d/snap.a.conf", "a")
	checkConfigfilesFile(c, "/etc/conf2.d/snap.a.conf", "a")

	// Now disconnect the first slot and set-up backends again
	c.Assert(s.Repo.Disconnect(plugInfos[0].Snap.InstanceName(), plugInfos[0].Name,
		slotInfo1.Snap.InstanceName(), slotInfo1.Name), IsNil)
	s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil)

	// Only files for the connected slots are around
	c.Check(filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.a.conf"), testutil.FileAbsent)
	checkConfigfilesFile(c, "/etc/conf2.d/snap.a.conf", "a")
}

func (s *backendSuite) TestUnmatchedPattern(c *C) {
	// Add callback and register the interface
	iface := &ifacetest.TestConfigFilesInterface{
		TestInterface: ifacetest.TestInterface{
			InterfaceName: "egl-driver-libs",
			ConfigfilesConnectedPlugCallback: func(spec *configfiles.Specification,
				plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
				spec.AddPathContent(filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.a.txt"),
					&osutil.MemoryFileState{Content: []byte("a"), Mode: 0655})
				return nil
			},
		},
		PathPatternsCallback: func() []string {
			return []string{filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/snap.*.conf")}
		},
	}
	c.Assert(s.Repo.AddInterface(iface), IsNil)

	// Mock plug/slots
	appSet, plugInfos := s.mockPlugs(c, eglConsumer, []string{"egl-driver-libs"})
	_, slotInfo1 := s.mockSlot(c, eglProvider1, "egl-driver-libs")

	// Connect
	connRef1 := interfaces.NewConnRef(plugInfos[0], slotInfo1)
	_, err := s.Repo.Connect(connRef1, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)
	// Set-up the backend
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{}, interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), ErrorMatches,
		`internal error: .*/etc/conf1.d/snap.a.txt\] not in any registered configfiles pattern`)

	c.Check(filepath.Join(dirs.GlobalRootDir, "/etc/conf1.d/a.txt"), testutil.FileAbsent)
}
