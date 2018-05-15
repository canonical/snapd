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

package mount_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/interfaces/mount"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type backendSuite struct {
	ifacetest.BackendSuite

	iface2 *ifacetest.TestInterface
}

var _ = Suite(&backendSuite{})

func (s *backendSuite) SetUpTest(c *C) {
	s.Backend = &mount.Backend{}
	s.BackendSuite.SetUpTest(c)

	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)

	err := os.MkdirAll(dirs.SnapMountPolicyDir, 0700)
	c.Assert(err, IsNil)

	// add second iface so that we actually test combining snippets
	s.iface2 = &ifacetest.TestInterface{InterfaceName: "iface2"}
	err = s.Repo.AddInterface(s.iface2)
	c.Assert(err, IsNil)
}

func (s *backendSuite) TearDownTest(c *C) {
	s.BackendSuite.TearDownTest(c)
}

func (s *backendSuite) TestName(c *C) {
	c.Check(s.Backend.Name(), Equals, interfaces.SecurityMount)
}

func (s *backendSuite) TestRemove(c *C) {
	appCanaryToGo := filepath.Join(dirs.SnapMountPolicyDir, "snap.hello-world.hello-world.fstab")
	err := ioutil.WriteFile(appCanaryToGo, []byte("ni! ni! ni!"), 0644)
	c.Assert(err, IsNil)

	hookCanaryToGo := filepath.Join(dirs.SnapMountPolicyDir, "snap.hello-world.hook.configure.fstab")
	err = ioutil.WriteFile(hookCanaryToGo, []byte("ni! ni! ni!"), 0644)
	c.Assert(err, IsNil)

	snapCanaryToGo := filepath.Join(dirs.SnapMountPolicyDir, "snap.hello-world.fstab")
	err = ioutil.WriteFile(snapCanaryToGo, []byte("ni! ni! ni!"), 0644)
	c.Assert(err, IsNil)

	appCanaryToStay := filepath.Join(dirs.SnapMountPolicyDir, "snap.i-stay.really.fstab")
	err = ioutil.WriteFile(appCanaryToStay, []byte("stay!"), 0644)
	c.Assert(err, IsNil)

	snapCanaryToStay := filepath.Join(dirs.SnapMountPolicyDir, "snap.i-stay.fstab")
	err = ioutil.WriteFile(snapCanaryToStay, []byte("stay!"), 0644)
	c.Assert(err, IsNil)

	err = s.Backend.Remove("hello-world")
	c.Assert(err, IsNil)

	c.Assert(osutil.FileExists(snapCanaryToGo), Equals, false)
	c.Assert(osutil.FileExists(appCanaryToGo), Equals, false)
	c.Assert(osutil.FileExists(hookCanaryToGo), Equals, false)
	c.Assert(appCanaryToStay, testutil.FileEquals, "stay!")
	c.Assert(snapCanaryToStay, testutil.FileEquals, "stay!")
}

var mockSnapYaml = `name: snap-name
version: 1
apps:
    app1:
    app2:
hooks:
    configure:
        plugs: [iface-plug]
plugs:
    iface-plug:
        interface: iface
slots:
    iface-slot:
        interface: iface2
`

func (s *backendSuite) TestSetupSetsupSimple(c *C) {
	fsEntry1 := osutil.MountEntry{Name: "/src-1", Dir: "/dst-1", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry2 := osutil.MountEntry{Name: "/src-2", Dir: "/dst-2", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}
	fsEntry3 := osutil.MountEntry{Name: "/src-3", Dir: "/dst-3", Type: "none", Options: []string{"bind", "ro"}, DumpFrequency: 0, CheckPassNumber: 0}

	// Give the plug a permanent effect
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		if err := spec.AddMountEntry(fsEntry1); err != nil {
			return err
		}
		return spec.AddUserMountEntry(fsEntry3)
	}
	// Give the slot a permanent effect
	s.iface2.MountPermanentSlotCallback = func(spec *mount.Specification, slot *snap.SlotInfo) error {
		return spec.AddMountEntry(fsEntry2)
	}

	// confinement options are irrelevant to this security backend
	s.InstallSnap(c, interfaces.ConfinementOptions{}, mockSnapYaml, 0)

	// ensure both security effects from iface/iface2 are combined
	// (because mount profiles are global in the whole snap)
	expected := strings.Split(fmt.Sprintf("%s\n%s\n", fsEntry1, fsEntry2), "\n")
	sort.Strings(expected)
	// and that we have the modern fstab file (global for snap)
	fn := filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.fstab")
	content, err := ioutil.ReadFile(fn)
	c.Assert(err, IsNil, Commentf("Expected mount profile for the whole snap"))
	got := strings.Split(string(content), "\n")
	sort.Strings(got)
	c.Check(got, DeepEquals, expected)

	// Check that the user-fstab file was written with the user mount
	fn = filepath.Join(dirs.SnapMountPolicyDir, "snap.snap-name.user-fstab")
	content, err = ioutil.ReadFile(fn)
	c.Assert(err, IsNil, Commentf("Expected user mount profile for the whole snap"))
	c.Check(string(content), Equals, fsEntry3.String()+"\n")
}

func (s *backendSuite) TestSetupSetsupWithoutDir(c *C) {
	s.Iface.MountPermanentPlugCallback = func(spec *mount.Specification, plug *snap.PlugInfo) error {
		return spec.AddMountEntry(osutil.MountEntry{})
	}

	// Ensure that backend.Setup() creates the required dir on demand
	os.Remove(dirs.SnapMountPolicyDir)
	s.InstallSnap(c, interfaces.ConfinementOptions{}, mockSnapYaml, 0)
}

func (s *backendSuite) TestSandboxFeatures(c *C) {
	c.Assert(s.Backend.SandboxFeatures(), DeepEquals, []string{
		"freezer-cgroup-v1",
		"layouts-beta",
		"mount-namespace",
		"per-snap-persistency",
		"per-snap-profiles",
		"per-snap-updates",
		"per-snap-user-profiles",
		"stale-base-invalidation",
	})
}
