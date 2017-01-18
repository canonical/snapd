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

	canaryToStay := filepath.Join(dirs.SnapMountPolicyDir, "snap.i-stay.really.fstab")
	err = ioutil.WriteFile(canaryToStay, []byte("stay!"), 0644)
	c.Assert(err, IsNil)

	err = s.Backend.Remove("hello-world")
	c.Assert(err, IsNil)

	c.Assert(osutil.FileExists(appCanaryToGo), Equals, false)
	c.Assert(osutil.FileExists(hookCanaryToGo), Equals, false)
	content, err := ioutil.ReadFile(canaryToStay)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "stay!")
}

var mockSnapYaml = `name: snap-name
version: 1
apps:
    app1:
    app2:
hooks:
    configure:
        plugs: [iface-plug, iface2-plug]
plugs:
    iface-plug:
        interface: iface
    iface2-plug:
        interface: iface2
slots:
    iface-slot:
        interface: iface
    iface2-slot:
        interface: iface2
`

func (s *backendSuite) TestSetupSetsupSimple(c *C) {
	fsEntryIF1 := "/src-1 /dst-1 none bind,ro 0 0"
	fsEntryIF2 := "/src-2 /dst-2 none bind,ro 0 0"

	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(fsEntryIF1), nil
	}
	s.Iface.PermanentPlugSnippetCallback = func(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(fsEntryIF1), nil
	}
	s.iface2.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(fsEntryIF2), nil
	}
	s.iface2.PermanentPlugSnippetCallback = func(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte(fsEntryIF2), nil
	}

	// confinement options are irrelevant to this security backend
	s.InstallSnap(c, interfaces.ConfinementOptions{}, mockSnapYaml, 0)

	// ensure both security snippets for iface/iface2 are combined
	expected := strings.Split(fmt.Sprintf("%s\n%s\n", fsEntryIF1, fsEntryIF2), "\n")
	sort.Strings(expected)
	// and we have them both for both apps and the hook
	for _, binary := range []string{"app1", "app2", "hook.configure"} {
		fn1 := filepath.Join(dirs.SnapMountPolicyDir, fmt.Sprintf("snap.snap-name.%s.fstab", binary))
		content, err := ioutil.ReadFile(fn1)
		c.Assert(err, IsNil, Commentf("Expected mount file for %q", binary))
		got := strings.Split(string(content), "\n")
		sort.Strings(got)
		c.Check(got, DeepEquals, expected)
	}
}

func (s *backendSuite) TestSetupSetsupWithoutDir(c *C) {
	s.Iface.PermanentSlotSnippetCallback = func(slot *interfaces.Slot, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("xxx"), nil
	}
	s.Iface.PermanentPlugSnippetCallback = func(plug *interfaces.Plug, securitySystem interfaces.SecuritySystem) ([]byte, error) {
		return []byte("xxx"), nil
	}

	// Ensure that backend.Setup() creates the required dir on demand
	os.Remove(dirs.SnapMountPolicyDir)
	s.InstallSnap(c, interfaces.ConfinementOptions{}, mockSnapYaml, 0)

	for _, binary := range []string{"app1", "app2", "hook.configure"} {
		fn := filepath.Join(dirs.SnapMountPolicyDir, fmt.Sprintf("snap.snap-name.%s.fstab", binary))
		c.Assert(osutil.FileExists(fn), Equals, true, Commentf("Expected mount file for %q", binary))
	}
}
