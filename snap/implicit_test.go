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

package snap_test

import (
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"

	. "gopkg.in/check.v1"
)

type SpecialSuite struct{}

var _ = Suite(&SpecialSuite{})

func (s *InfoSnapYamlTestSuite) TestAddImplicitSlotsOutsideClassic(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	osYaml := []byte("name: ubuntu-core\ntype: os\n")
	info, err := snap.InfoFromSnapYaml(osYaml)
	c.Assert(err, IsNil)
	snap.AddImplicitSlots(info)
	c.Assert(info.Slots["network"].Interface, Equals, "network")
	c.Assert(info.Slots["network"].Name, Equals, "network")
	c.Assert(info.Slots["network"].Snap, Equals, info)
	c.Assert(info.Slots, HasLen, 13)
}

func (s *InfoSnapYamlTestSuite) TestAddImplicitSlotsOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	osYaml := []byte("name: ubuntu-core\ntype: os\n")
	info, err := snap.InfoFromSnapYaml(osYaml)
	c.Assert(err, IsNil)
	snap.AddImplicitSlots(info)
	c.Assert(info.Slots["unity7"].Interface, Equals, "unity7")
	c.Assert(info.Slots["unity7"].Name, Equals, "unity7")
	c.Assert(info.Slots["unity7"].Snap, Equals, info)
	c.Assert(info.Slots, HasLen, 20)
}

func (s *InfoSnapYamlTestSuite) TestImplicitSlotsAreRealInterfaces(c *C) {
	known := make(map[string]bool)
	for _, iface := range builtin.Interfaces() {
		known[iface.Name()] = true
	}
	for _, ifaceName := range snap.ImplicitSlotsForTests {
		c.Check(known[ifaceName], Equals, true)
	}
	for _, ifaceName := range snap.ImplicitClassicSlotsForTests {
		c.Check(known[ifaceName], Equals, true)
	}
}

func (s *InfoSnapYamlTestSuite) TestAddImplicitHooksNoHooks(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	yaml := `name: foo
version: 1.0`
	info := snaptest.MockSnap(c, yaml, &snap.SideInfo{Revision: snap.R(42)})

	// Now load implicit hooks
	snap.AddImplicitHooks(info)

	// Verify that no hooks were loaded for this snap
	c.Check(info.Hooks, HasLen, 0)
}

func (s *InfoSnapYamlTestSuite) TestAddImplicitHooksFromContainerNoHooks(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	yaml := `name: foo
version: 1.0`
	path := makeTestSnap(c, yaml)

	container, err := snap.Open(path)
	c.Assert(err, IsNil)

	// Now load implicit hooks
	info := &snap.Info{Hooks: make(map[string]*snap.HookInfo)}
	snap.AddImplicitHooksFromContainer(info, container)

	// Verify that no hooks were loaded for this snap
	c.Check(info.Hooks, HasLen, 0)
}

func (s *InfoSnapYamlTestSuite) TestAddImplicitHooksSingle(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	yaml := `name: foo
version: 1.0`
	info := snaptest.MockSnapWithHooks(c, yaml, &snap.SideInfo{Revision: snap.R(42)}, []string{"test-hook"})

	// Verify that no hooks have been loaded for this snap
	c.Check(info.Hooks, HasLen, 0)

	// Now load implicit hooks
	snap.AddImplicitHooks(info)

	// Verify that the `test-hook` hook has now been loaded, and that it has no
	// associated plugs.
	c.Check(info.Hooks, HasLen, 1)
	verifyImplicitHook(c, info, "test-hook")
}

func (s *InfoSnapYamlTestSuite) TestAddImplicitHooksFromContainerSingle(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	yaml := `name: foo
version: 1.0`
	path := makeTestSnapWithHooks(c, yaml, []string{"test-hook"})

	container, err := snap.Open(path)
	c.Assert(err, IsNil)

	// Now load implicit hooks
	info := &snap.Info{Hooks: make(map[string]*snap.HookInfo)}
	snap.AddImplicitHooksFromContainer(info, container)

	// Verify that the `test-hook` hook has now been loaded, and that it has no
	// associated plugs.
	c.Check(info.Hooks, HasLen, 1)
	verifyImplicitHook(c, info, "test-hook")
}

func (s *InfoSnapYamlTestSuite) TestAddImplicitHooksMultiple(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	yaml := `name: foo
version: 1.0`
	info := snaptest.MockSnapWithHooks(c, yaml, &snap.SideInfo{Revision: snap.R(42)}, []string{"hook1", "hook2"})

	// Verify that no hooks have been loaded for this snap
	c.Check(info.Hooks, HasLen, 0)

	// Now load implicit hooks
	snap.AddImplicitHooks(info)

	// Verify that both hooks have now been loaded, and that neither have any
	// associated plugs.
	c.Check(info.Hooks, HasLen, 2)
	verifyImplicitHook(c, info, "hook1")
	verifyImplicitHook(c, info, "hook2")
}

func (s *InfoSnapYamlTestSuite) TestAddImplicitHooksFromContainerMultiple(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	yaml := `name: foo
version: 1.0`
	path := makeTestSnapWithHooks(c, yaml, []string{"hook1", "hook2"})

	container, err := snap.Open(path)
	c.Assert(err, IsNil)

	// Now load implicit hooks
	info := &snap.Info{Hooks: make(map[string]*snap.HookInfo)}
	snap.AddImplicitHooksFromContainer(info, container)

	// Verify that both hooks have now been loaded, and that neither have any
	// associated plugs.
	c.Check(info.Hooks, HasLen, 2)
	verifyImplicitHook(c, info, "hook1")
	verifyImplicitHook(c, info, "hook2")
}

func (s *InfoSnapYamlTestSuite) TestAddImplicitHooksWithExplicitHook(c *C) {
	dirs.SetRootDir(c.MkDir())
	defer func() { dirs.SetRootDir("") }()

	yaml := `name: foo
version: 1.0
hooks:
  explicit:
    plugs: [test-plug]`
	info := snaptest.MockSnapWithHooks(c, yaml, &snap.SideInfo{Revision: snap.R(42)}, []string{"explicit", "implicit"})

	// Verify that `explicit` has already been loaded due to its being in the YAML.
	c.Assert(info.Hooks, HasLen, 1)
	verifyExplicitHook(c, info, "explicit", []string{"test-plug"})

	// Now load implicit hooks
	snap.AddImplicitHooks(info)

	// Verify that the `implicit` hook has now been loaded, and that it has no
	// associated plugs. Also verify that the `explicit` hook is still valid.
	c.Check(info.Hooks, HasLen, 2)
	verifyImplicitHook(c, info, "implicit")
	verifyExplicitHook(c, info, "explicit", []string{"test-plug"})
}

func verifyImplicitHook(c *C, info *snap.Info, hookName string) {
	hook := info.Hooks[hookName]
	c.Assert(hook, NotNil, Commentf("Expected hooks to contain %q", hookName))
	c.Check(hook.Name, Equals, hookName)
	c.Check(hook.Plugs, IsNil)
}

func verifyExplicitHook(c *C, info *snap.Info, hookName string, plugNames []string) {
	hook := info.Hooks[hookName]
	c.Assert(hook, NotNil, Commentf("Expected hooks to contain %q", hookName))
	c.Check(hook.Name, Equals, hookName)
	c.Check(hook.Plugs, HasLen, len(plugNames))

	for _, plugName := range plugNames {
		// Verify that the HookInfo and PlugInfo point to each other
		plug := hook.Plugs[plugName]
		c.Assert(plug, NotNil, Commentf("Expected hook plugs to contain %q", plugName))
		c.Check(plug.Name, Equals, plugName)
		c.Check(plug.Hooks, HasLen, 1)
		hook = plug.Hooks[hookName]
		c.Assert(hook, NotNil, Commentf("Expected plug to be associated with hook %q", hookName))
		c.Check(hook.Name, Equals, hookName)

		// Verify also that the hook plug made it into info.Plugs
		c.Check(info.Plugs[plugName], DeepEquals, plug)
	}
}
