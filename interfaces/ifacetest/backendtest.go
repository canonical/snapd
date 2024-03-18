// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package ifacetest

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type BackendSuite struct {
	Backend         interfaces.SecurityBackend
	Repo            *interfaces.Repository
	Iface           *TestInterface
	RootDir         string
	restoreSanitize func()

	meas *timings.Span
	testutil.BaseTest
}

// CoreSnapInfo is set in SetupSuite
var DefaultInitializeOpts = &interfaces.SecurityBackendOptions{}

func (s *BackendSuite) SetUpTest(c *C) {
	coreSnapPlaceInfo := snap.MinimalPlaceInfo("core", snap.Revision{N: 123})
	snInfo, ok := coreSnapPlaceInfo.(*snap.Info)
	c.Assert(ok, Equals, true)
	DefaultInitializeOpts.CoreSnapInfo = snInfo

	snapdSnapPlaceInfo := snap.MinimalPlaceInfo("snapd", snap.Revision{N: 321})
	snInfo, ok = snapdSnapPlaceInfo.(*snap.Info)
	c.Assert(ok, Equals, true)
	DefaultInitializeOpts.SnapdSnapInfo = snInfo

	// Isolate this test to a temporary directory
	s.RootDir = c.MkDir()
	dirs.SetRootDir(s.RootDir)

	restore := osutil.MockMountInfo("")
	s.AddCleanup(restore)

	// Create a fresh repository for each test
	s.Repo = interfaces.NewRepository()
	s.Iface = &TestInterface{InterfaceName: "iface"}
	err := s.Repo.AddInterface(s.Iface)
	c.Assert(err, IsNil)

	perf := timings.New(nil)
	s.meas = perf.StartSpan("", "")

	s.restoreSanitize = snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
}

func (s *BackendSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
	s.restoreSanitize()
}

// Tests for Setup() and Remove()
const SambaYamlV1 = `
name: samba
version: 1
developer: acme
apps:
    smbd:
slots:
    slot:
        interface: iface
`
const SambaYamlV1Core20Base = `
name: samba
base: core20
version: 1
developer: acme
apps:
    smbd:
slots:
    slot:
        interface: iface
`
const SambaYamlV1WithNmbd = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
slots:
    slot:
        interface: iface
`
const SambaYamlV1NoSlot = `
name: samba
version: 1
developer: acme
apps:
    smbd:
`
const SambaYamlV1WithNmbdNoSlot = `
name: samba
version: 1
developer: acme
apps:
    smbd:
    nmbd:
`
const SambaYamlV2 = `
name: samba
version: 2
developer: acme
apps:
    smbd:
slots:
    slot:
        interface: iface
`
const SambaYamlWithHook = `
name: samba
version: 0
apps:
    smbd:
    nmbd:
hooks:
    configure:
        plugs: [plug]
slots:
    slot:
        interface: iface
plugs:
    plug:
        interface: iface
`
const HookYaml = `
name: foo
version: 1
developer: acme
hooks:
    configure:
plugs:
    plug:
        interface: iface
`
const PlugNoAppsYaml = `
name: foo
version: 1
developer: acme
plugs:
    plug:
        interface: iface
`
const SlotNoAppsYaml = `
name: foo
version: 1
developer: acme
slots:
    slots:
        interface: iface
`

const SomeSnapYamlV1 = `
name: some-snap
version: 1
developer: acme
apps:
    someapp:
`

var SnapWithComponentsYaml = `
name: snap
version: 1
apps:
  app:
components:
  comp:
    type: test
    hooks:
      install:
plugs:
  iface:
`

var ComponentYaml = `
component: snap+comp
type: test
version: 1
`

// Support code for tests

// InstallSnap "installs" a snap from YAML.
func (s *BackendSuite) InstallSnap(c *C, opts interfaces.ConfinementOptions, instanceName, snapYaml string, revision int) *snap.Info {
	snapInfo := snaptest.MockInfo(c, snapYaml, &snap.SideInfo{
		Revision: snap.R(revision),
	})

	appSet, err := interfaces.NewSnapAppSet(snapInfo, nil)
	c.Assert(err, IsNil)

	if instanceName != "" {
		_, instanceKey := snap.SplitInstanceName(instanceName)
		snapInfo.InstanceKey = instanceKey
		c.Assert(snapInfo.InstanceName(), Equals, instanceName)
	}

	err = s.Repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	err = s.Backend.Setup(appSet, opts, s.Repo, s.meas)
	c.Assert(err, IsNil)
	return snapInfo
}

func (s *BackendSuite) InstallSnapWithComponents(c *C, opts interfaces.ConfinementOptions, instanceName, snapYaml string, revision int, componentYamls []string) *snap.Info {
	snapInfo := snaptest.MockInfo(c, snapYaml, &snap.SideInfo{
		Revision: snap.R(revision),
	})

	if instanceName != "" {
		_, instanceKey := snap.SplitInstanceName(instanceName)
		snapInfo.InstanceKey = instanceKey
		c.Assert(snapInfo.InstanceName(), Equals, instanceName)
	}

	componentInfos := make([]*snap.ComponentInfo, 0, len(componentYamls))
	for _, componentYaml := range componentYamls {
		componentInfos = append(componentInfos, snaptest.MockComponent(c, componentYaml, snapInfo, snap.ComponentSideInfo{
			Revision: snap.R(1),
		}))
	}

	appSet, err := interfaces.NewSnapAppSet(snapInfo, componentInfos)
	c.Assert(err, IsNil)

	err = s.Repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	err = s.Backend.Setup(appSet, opts, s.Repo, s.meas)
	c.Assert(err, IsNil)
	return snapInfo
}

func (s *BackendSuite) UpdateSnapWithComponents(c *C, oldSnapInfo *snap.Info, opts interfaces.ConfinementOptions, snapYaml string, revision int, componentYamls []string) *snap.Info {
	snapInfo := snaptest.MockInfo(c, snapYaml, &snap.SideInfo{
		Revision: snap.R(revision),
	})

	snapInfo.InstanceKey = oldSnapInfo.InstanceKey

	componentInfos := make([]*snap.ComponentInfo, 0, len(componentYamls))
	for _, componentYaml := range componentYamls {
		componentInfos = append(componentInfos, snaptest.MockComponent(c, componentYaml, snapInfo, snap.ComponentSideInfo{
			Revision: snap.R(1),
		}))
	}

	appSet, err := interfaces.NewSnapAppSet(snapInfo, componentInfos)
	c.Assert(err, IsNil)

	s.Repo.RemoveSnap(oldSnapInfo.InstanceName())

	err = s.Repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	err = s.Backend.Setup(appSet, opts, s.Repo, s.meas)
	c.Assert(err, IsNil)
	return snapInfo
}

// UpdateSnap "updates" an existing snap from YAML.
func (s *BackendSuite) UpdateSnap(c *C, oldSnapInfo *snap.Info, opts interfaces.ConfinementOptions, snapYaml string, revision int) *snap.Info {
	newSnapInfo, err := s.UpdateSnapMaybeErr(c, oldSnapInfo, opts, snapYaml, revision)
	c.Assert(err, IsNil)
	return newSnapInfo
}

// UpdateSnapMaybeErr "updates" an existing snap from YAML, this might error.
func (s *BackendSuite) UpdateSnapMaybeErr(c *C, oldSnapInfo *snap.Info, opts interfaces.ConfinementOptions, snapYaml string, revision int) (*snap.Info, error) {
	newSnapInfo := snaptest.MockInfo(c, snapYaml, &snap.SideInfo{
		Revision: snap.R(revision),
	})

	newSnapInfo.InstanceKey = oldSnapInfo.InstanceKey

	appSet, err := interfaces.NewSnapAppSet(newSnapInfo, nil)
	c.Assert(err, IsNil)

	c.Assert(newSnapInfo.InstanceName(), Equals, oldSnapInfo.InstanceName())
	s.Repo.RemoveSnap(oldSnapInfo.InstanceName())

	err = s.Repo.AddAppSet(appSet)
	c.Assert(err, IsNil)

	err = s.Backend.Setup(appSet, opts, s.Repo, s.meas)
	return newSnapInfo, err
}

// RemoveSnap "removes" an "installed" snap.
func (s *BackendSuite) RemoveSnap(c *C, snapInfo *snap.Info) {
	err := s.Backend.Remove(snapInfo.InstanceName())
	c.Assert(err, IsNil)
	s.Repo.RemoveSnap(snapInfo.InstanceName())
}
