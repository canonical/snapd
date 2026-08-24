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

package export_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/export"
	"github.com/snapcore/snapd/interfaces/ifacetest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

// reconcileSuite exercises the on-disk reconciliation performed by
// Backend.Setup() end-to-end: the export tree it leaves behind, garbage
// collection, and its scoping to InterfaceRoot (never touching sibling
// state belonging to other backends).
//
// This does not embed backendSuite (unlike other suites in this package)
// because backendSuite defines its own Test* methods, which would otherwise
// be promoted and re-run, redundantly, as part of this suite too.
type reconcileSuite struct {
	ifacetest.BackendSuite
}

var _ = Suite(&reconcileSuite{})

func (s *reconcileSuite) SetUpTest(c *C) {
	s.Backend = &export.Backend{}
	s.BackendSuite.SetUpTest(c)
	c.Assert(s.Repo.AddBackend(s.Backend), IsNil)
}

func (s *reconcileSuite) mockSlot(c *C, yaml string, slotName string) (*interfaces.SnapAppSet, *snap.SlotInfo) {
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

func (s *reconcileSuite) mockPlug(c *C, yaml string, plugName string) (*interfaces.SnapAppSet, *snap.PlugInfo) {
	info := snaptest.MockInfo(c, yaml, nil)

	set, err := interfaces.NewSnapAppSet(info, nil)
	c.Assert(err, IsNil)
	err = s.Repo.AddAppSet(set)
	c.Assert(err, IsNil)

	if plugInfo, ok := info.Plugs[plugName]; ok {
		return set, plugInfo
	}
	panic(fmt.Sprintf("cannot find plug %q in snap %q", plugName, info.InstanceName()))
}

func (s *reconcileSuite) setup(c *C, appSet *interfaces.SnapAppSet) {
	c.Assert(s.Backend.Setup(appSet, interfaces.ConfinementOptions{},
		interfaces.SetupContext{Reason: interfaces.SnapSetupReasonOther}, s.Repo, nil), IsNil)
}

func (s *reconcileSuite) TestFreshCreate(c *C) {
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddExportedFile("some-driver-libs", "provider_some-driver-libs_0",
			"egl_vendor.d/a.json", &osutil.MemoryFileState{Content: []byte("hello"), Mode: 0644})
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")
	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	s.setup(c, appSet)

	unitDir := export.UnitDir("some-driver-libs", "provider_some-driver-libs_0")
	content, err := os.ReadFile(filepath.Join(unitDir, "egl_vendor.d/a.json"))
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "hello")

	manifest, err := os.ReadFile(export.ManifestPath("some-driver-libs"))
	c.Assert(err, IsNil)
	c.Check(string(manifest), Equals, "provider_some-driver-libs_0/egl_vendor.d/a.json\n")

	// No leftover temporary directory.
	c.Check(osutil.FileExists(export.UnitTmpDir("some-driver-libs", "provider_some-driver-libs_0")), Equals, false)
}

func (s *reconcileSuite) TestIdempotentReRun(c *C) {
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddExportedFile("some-driver-libs", "provider_some-driver-libs_0",
			"egl_vendor.d/a.json", &osutil.MemoryFileState{Content: []byte("hello"), Mode: 0644})
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")
	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	s.setup(c, appSet)

	unitDir := export.UnitDir("some-driver-libs", "provider_some-driver-libs_0")
	before, err := os.Stat(unitDir)
	c.Assert(err, IsNil)

	s.setup(c, appSet)

	after, err := os.Stat(unitDir)
	c.Assert(err, IsNil)
	// The unit directory was not touched at all on the second run (same
	// modification time), since its name already fully determines its
	// content (see UnitName).
	c.Check(after.ModTime().Equal(before.ModTime()), Equals, true)

	content, err := os.ReadFile(filepath.Join(unitDir, "egl_vendor.d/a.json"))
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "hello")
}

func (s *reconcileSuite) TestUnitReplacedOnRevisionBump(c *C) {
	unit := "provider_some-driver-libs_0"
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddExportedFile("some-driver-libs", unit,
			"egl_vendor.d/a.json", &osutil.MemoryFileState{Content: []byte("hello"), Mode: 0644})
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")
	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	s.setup(c, appSet)

	// A new revision produces a new unit name (see UnitName); simulate it
	// here directly since the naming derivation itself is tested
	// elsewhere (interfaces/builtin/helpers_test.go).
	newUnit := "provider_some-driver-libs_1"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddExportedFile("some-driver-libs", newUnit,
			"egl_vendor.d/a.json", &osutil.MemoryFileState{Content: []byte("goodbye"), Mode: 0644})
	}

	s.setup(c, appSet)

	// The old unit is gone...
	c.Check(osutil.FileExists(export.UnitDir("some-driver-libs", unit)), Equals, false)
	// ...and the new one has the new content.
	content, err := os.ReadFile(filepath.Join(export.UnitDir("some-driver-libs", newUnit), "egl_vendor.d/a.json"))
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "goodbye")

	manifest, err := os.ReadFile(export.ManifestPath("some-driver-libs"))
	c.Assert(err, IsNil)
	c.Check(string(manifest), Equals, newUnit+"/egl_vendor.d/a.json\n")
}

func (s *reconcileSuite) TestGCOnDisconnect(c *C) {
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddExportedFile("some-driver-libs", "provider_some-driver-libs_0",
			"egl_vendor.d/a.json", &osutil.MemoryFileState{Content: []byte("hello"), Mode: 0644})
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")
	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	s.setup(c, appSet)
	c.Check(osutil.FileExists(export.InterfaceRoot("some-driver-libs")), Equals, true)

	c.Assert(s.Repo.Disconnect(plugInfo.Snap.InstanceName(), plugInfo.Name,
		slotInfo.Snap.InstanceName(), slotInfo.Name), IsNil)
	s.setup(c, appSet)

	// Disconnecting the only connection leaves nothing declared for this
	// interface; the whole tree - unit, manifest, and the interface
	// directory itself - is garbage collected, exactly like the symlinks
	// backend removing every managed symlink from a directory whose spec
	// came back empty.
	c.Check(osutil.FileExists(export.InterfaceRoot("some-driver-libs")), Equals, false)
}

func (s *reconcileSuite) TestGCScopedToInterfaceNeverTouchesSiblingState(c *C) {
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddExportedFile("some-driver-libs", "provider_some-driver-libs_0",
			"egl_vendor.d/a.json", &osutil.MemoryFileState{Content: []byte("hello"), Mode: 0644})
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	// A file written directly under /var/lib/snapd/export/ by the
	// configfiles backend (see systemLibrarySourcePath in
	// interfaces/builtin/helpers.go), sitting next to (not inside) this
	// interface's own tree.
	exportRoot := filepath.Dir(filepath.Dir(export.InterfaceRoot("some-driver-libs")))
	siblingFile := filepath.Join(exportRoot, "system_provider_some-driver-libs_some-driver-libs.library-source")
	c.Assert(os.MkdirAll(exportRoot, 0755), IsNil)
	c.Assert(os.WriteFile(siblingFile, []byte("/snap/provider/1/lib1\n"), 0644), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")
	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	s.setup(c, appSet)

	// Disconnect, triggering full GC of this interface's tree.
	c.Assert(s.Repo.Disconnect(plugInfo.Snap.InstanceName(), plugInfo.Name,
		slotInfo.Snap.InstanceName(), slotInfo.Name), IsNil)
	s.setup(c, appSet)

	c.Check(osutil.FileExists(export.InterfaceRoot("some-driver-libs")), Equals, false)
	// The sibling file, one level up, must survive untouched.
	content, err := os.ReadFile(siblingFile)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "/snap/provider/1/lib1\n")
}

func (s *reconcileSuite) TestLeftoverTmpDirIsReaped(c *C) {
	unit := "provider_some-driver-libs_0"
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		return spec.AddExportedFile("some-driver-libs", unit,
			"egl_vendor.d/a.json", &osutil.MemoryFileState{Content: []byte("hello"), Mode: 0644})
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")
	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	// Simulate a crash mid-materialisation: a stale "<unit>.tmp" left
	// behind by an interrupted previous run, before the real unit
	// directory ever existed.
	tmpDir := export.UnitTmpDir("some-driver-libs", unit)
	c.Assert(os.MkdirAll(tmpDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(tmpDir, "garbage"), []byte("stale"), 0644), IsNil)

	s.setup(c, appSet)

	// The stale directory was replaced by a freshly materialised one
	// containing only the currently declared file.
	entries, err := os.ReadDir(export.UnitDir("some-driver-libs", unit))
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 1)
	c.Check(entries[0].Name(), Equals, "egl_vendor.d")

	content, err := os.ReadFile(filepath.Join(export.UnitDir("some-driver-libs", unit), "egl_vendor.d/a.json"))
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, "hello")
}

func (s *reconcileSuite) TestMultipleUnitsPooledUnderSameInterface(c *C) {
	s.Iface.InterfaceName = "some-driver-libs"
	s.Iface.ExportConnectedPlugCallback = func(spec *export.Specification,
		plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
		if err := spec.AddExportedFile("some-driver-libs", "provider_some-driver-libs_0",
			"egl_vendor.d/from-snap.json", &osutil.MemoryFileState{Content: []byte("snap"), Mode: 0644}); err != nil {
			return err
		}
		return spec.AddExportedFile("some-driver-libs", "provider_some-driver-libs_0+comp1_1",
			"egl_vendor.d/from-comp1.json", &osutil.MemoryFileState{Content: []byte("comp1"), Mode: 0644})
	}
	c.Assert(s.Repo.AddInterface(s.Iface), IsNil)

	appSet, plugInfo := s.mockPlug(c, exportConsumer, "some-driver-libs")
	_, slotInfo := s.mockSlot(c, exportProvider, "some-driver-libs")
	connRef := interfaces.NewConnRef(plugInfo, slotInfo)
	_, err := s.Repo.Connect(connRef, nil, nil, nil, nil, nil)
	c.Assert(err, IsNil)

	s.setup(c, appSet)

	root := export.InterfaceRoot("some-driver-libs")
	entries, err := os.ReadDir(root)
	c.Assert(err, IsNil)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	c.Check(names, DeepEquals, []string{
		"export.sources", "provider_some-driver-libs_0", "provider_some-driver-libs_0+comp1_1",
	})

	manifest, err := os.ReadFile(export.ManifestPath("some-driver-libs"))
	c.Assert(err, IsNil)
	c.Check(string(manifest), Equals,
		"provider_some-driver-libs_0+comp1_1/egl_vendor.d/from-comp1.json\n"+
			"provider_some-driver-libs_0/egl_vendor.d/from-snap.json\n")
}
