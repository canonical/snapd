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

// Package snaptest contains helper functions for mocking snaps.
package snaptest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/pack"
	"github.com/snapcore/snapd/snap/snapdir"
	"github.com/snapcore/snapd/snap/squashfs"
)

func mockSnap(c *check.C, instanceName, yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	c.Assert(sideInfo, check.Not(check.IsNil))

	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	defer restoreSanitize()

	// Parse the yaml (we need the Name).
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, check.IsNil)

	// Set SideInfo so that we can use MountDir below
	snapInfo.SideInfo = *sideInfo

	if instanceName != "" {
		// Set the snap instance name
		snapName, instanceKey := snap.SplitInstanceName(instanceName)
		snapInfo.InstanceKey = instanceKey

		// Make sure snap name/instance name checks out
		c.Assert(snapInfo.InstanceName(), check.Equals, instanceName)
		c.Assert(snapInfo.SnapName(), check.Equals, snapName)
	}

	// Put the YAML on disk, in the right spot.
	metaDir := filepath.Join(snapInfo.MountDir(), "meta")
	err = os.MkdirAll(metaDir, 0755)
	c.Assert(err, check.IsNil)
	err = os.WriteFile(filepath.Join(metaDir, "snap.yaml"), []byte(yamlText), 0644)
	c.Assert(err, check.IsNil)

	// Write the .snap to disk
	err = os.MkdirAll(filepath.Dir(snapInfo.MountFile()), 0755)
	c.Assert(err, check.IsNil)
	snapContents := fmt.Sprintf("%s-%s-%s", sideInfo.RealName, sideInfo.SnapID, sideInfo.Revision)
	err = os.WriteFile(snapInfo.MountFile(), []byte(snapContents), 0644)
	c.Assert(err, check.IsNil)
	snapInfo.Size = int64(len(snapContents))

	return snapInfo
}

// MockSnap puts a snap.yaml file on disk so to mock an installed snap, based on the provided arguments.
//
// The caller is responsible for mocking root directory with dirs.SetRootDir()
// and for altering the overlord state if required.
func MockSnap(c *check.C, yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	return mockSnap(c, "", yamlText, sideInfo)
}

// MockComponent puts a component.yaml file on disk so to mock an installed
// component, based on the provided arguments.
//
// The caller is responsible for mocking root directory with dirs.SetRootDir()
// and for altering the overlord state if required.
func MockComponent(c *check.C, yamlText string, info *snap.Info, csi snap.ComponentSideInfo) *snap.ComponentInfo {
	infoForName, err := snap.InfoFromComponentYaml([]byte(yamlText))
	c.Assert(err, check.IsNil)

	// Put the component.yaml on disk, in the right spot.
	mountDir := snap.ComponentMountDir(infoForName.Component.ComponentName, csi.Revision, info.InstanceName())
	metaDir := filepath.Join(mountDir, "meta")
	err = os.MkdirAll(metaDir, 0755)
	c.Assert(err, check.IsNil)

	err = os.WriteFile(filepath.Join(metaDir, "component.yaml"), []byte(yamlText), 0644)
	c.Assert(err, check.IsNil)

	// Write the .snap to disk
	err = os.MkdirAll(filepath.Dir(info.MountFile()), 0755)
	c.Assert(err, check.IsNil)

	// TODO: write something to disk for the component snap file, like in
	// MockSnap

	container := snapdir.New(filepath.Dir(metaDir))
	component, err := snap.ReadComponentInfoFromContainer(container, info, &csi)
	c.Assert(err, check.IsNil)

	return component
}

// MockSnapInstance puts a snap.yaml file on disk so to mock an installed snap
// instance, based on the provided arguments.
//
// The caller is responsible for mocking root directory with dirs.SetRootDir()
// and for altering the overlord state if required.
func MockSnapInstance(c *check.C, instanceName, yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	return mockSnap(c, instanceName, yamlText, sideInfo)
}

// MockSnapCurrent does the same as MockSnap but additionally creates the
// 'current' symlink.
//
// The caller is responsible for mocking root directory with dirs.SetRootDir()
// and for altering the overlord state if required.
func MockSnapCurrent(c *check.C, yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	si := MockSnap(c, yamlText, sideInfo)
	err := os.Symlink(filepath.Base(si.MountDir()), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)
	return si
}

func MockComponentCurrent(c *check.C, yamlText string, info *snap.Info, csi snap.ComponentSideInfo) *snap.ComponentInfo {
	ci := MockComponent(c, yamlText, info, csi)

	mountDir := snap.ComponentMountDir(ci.Component.ComponentName, ci.Revision, info.InstanceName())
	link := filepath.Join(snap.ComponentsBaseDir(info.InstanceName()), info.Revision.String(), ci.Component.ComponentName)
	err := os.MkdirAll(filepath.Dir(link), 0755)
	c.Assert(err, check.IsNil)

	linkDest, err := filepath.Rel(filepath.Dir(link), mountDir)
	c.Assert(err, check.IsNil)

	err = osutil.AtomicSymlink(linkDest, link)
	c.Assert(err, check.IsNil)

	return ci
}

// MockSnapInstanceCurrent does the same as MockSnapInstance but additionally
// creates the 'current' symlink.
//
// The caller is responsible for mocking root directory with dirs.SetRootDir()
// and for altering the overlord state if required.
func MockSnapInstanceCurrent(c *check.C, instanceName, yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	si := MockSnapInstance(c, instanceName, yamlText, sideInfo)
	err := os.Symlink(si.MountDir(), filepath.Join(si.MountDir(), "../current"))
	c.Assert(err, check.IsNil)
	return si
}

// MockInfo parses the given snap.yaml text and returns a validated snap.Info object including the optional SideInfo.
//
// The result is just kept in memory, there is nothing kept on disk. If that is
// desired please use MockSnap instead.
func MockInfo(c *check.C, yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	if sideInfo == nil {
		sideInfo = &snap.SideInfo{}
	}

	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	defer restoreSanitize()
	snapInfo, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, check.IsNil)
	if snapInfo.InstanceName() == "core" && snapInfo.Type() != snap.TypeOS {
		panic("core snap must use type: os")
	}
	if snapInfo.InstanceName() == "snapd" && snapInfo.Type() != snap.TypeSnapd {
		panic("snapd snap must use type: snapd")
	}

	snapInfo.SideInfo = *sideInfo
	err = snap.Validate(snapInfo)
	c.Assert(err, check.IsNil)
	return snapInfo
}

// MockInvalidInfo parses the given snap.yaml text and returns the snap.Info object including the optional SideInfo.
//
// The result is just kept in memory, there is nothing kept on disk. If that is
// desired please use MockSnap instead.
func MockInvalidInfo(c *check.C, yamlText string, sideInfo *snap.SideInfo) *snap.Info {
	if sideInfo == nil {
		sideInfo = &snap.SideInfo{}
	}

	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	defer restoreSanitize()

	snapInfo, err := snap.InfoFromSnapYaml([]byte(yamlText))
	c.Assert(err, check.IsNil)
	snapInfo.SideInfo = *sideInfo
	err = snap.Validate(snapInfo)
	c.Assert(err, check.NotNil)
	return snapInfo
}

// MockSnapWithFiles does the same as MockSnap, but also populates the snap
// directory with given content
//
// The caller is responsible for mocking root directory with dirs.SetRootDir()
// and for altering the overlord state if required.
func MockSnapWithFiles(c *check.C, yamlText string, si *snap.SideInfo, files [][]string) *snap.Info {
	info := MockSnap(c, yamlText, si)

	PopulateDir(info.MountDir(), files)
	return info
}

// PopulateDir populates the directory with files specified as pairs of relative file path and its content. Useful to add extra files to a snap.
func PopulateDir(dir string, files [][]string) {
	for _, filenameAndContent := range files {
		filename := filenameAndContent[0]
		content := filenameAndContent[1]
		fpath := filepath.Join(dir, filename)
		err := os.MkdirAll(filepath.Dir(fpath), 0755)
		if err != nil {
			panic(err)
		}
		err = os.WriteFile(fpath, []byte(content), 0755)
		if err != nil {
			panic(err)
		}
	}
}

func AssertedSnapID(snapName string) string {
	cleanedName := strings.Replace(snapName, "-", "", -1)
	return cleanedName + strings.Repeat("id", 16)[len(cleanedName):]
}

// MakeTestSnapWithFiles makes a squashfs snap file with the given
// snap.yaml content and optional extras files specified as pairs of
// relative file path and its content.
func MakeTestSnapWithFiles(c *check.C, snapYamlContent string, files [][]string) (snapFilePath string) {
	path, _ := MakeTestSnapInfoWithFiles(c, snapYamlContent, files, nil)
	return path
}

// MakeTestComponentWithFiles builds a snap component container with a
// given name and content for component.yaml, populating additionally
// the squashfs with files if required.
func MakeTestComponentWithFiles(c *check.C, componentName, componentYaml string, files [][]string) (snapFilePath string) {
	compSource := populateContainer(c, "component.yaml", componentYaml, files)
	err := osutil.ChDir(compSource, func() error {
		d := squashfs.New(componentName)
		err := d.Build(compSource, nil)
		return err
	})
	c.Assert(err, check.IsNil)
	return filepath.Join(compSource, componentName)
}

func MakeTestComponent(c *check.C, compYaml string) string {
	compInfo, err := snap.InfoFromComponentYaml([]byte(compYaml))
	c.Assert(err, check.IsNil)
	return MakeTestComponentWithFiles(c, compInfo.FullName()+".comp", compYaml, nil)
}

func populateContainer(c *check.C, yamlFile, yamlContent string, files [][]string) string {
	tmpdir := c.MkDir()
	snapSource := filepath.Join(tmpdir, "snapsrc")
	err := os.MkdirAll(filepath.Join(snapSource, "meta"), 0755)
	c.Assert(err, check.IsNil)
	snapYamlFn := filepath.Join(snapSource, "meta", yamlFile)
	err = os.WriteFile(snapYamlFn, []byte(yamlContent), 0644)
	c.Assert(err, check.IsNil)
	PopulateDir(snapSource, files)
	return snapSource
}

// MakeTestSnapInfoWithFiles makes a squashfs snap file with the given snap.yaml
// content and optional extra files specified as pairs of relative file path and
// it's contents, and returns the path to the snap file and a suitable snap.Info
// for the snap
func MakeTestSnapInfoWithFiles(c *check.C, snapYamlContent string, files [][]string, si *snap.SideInfo) (snapFilePath string, info *snap.Info) {
	snapSource := populateContainer(c, "snap.yaml", snapYamlContent, files)

	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	defer restoreSanitize()

	// Parse the yaml (we need the Name).
	snapInfo, err := snap.InfoFromSnapYaml([]byte(snapYamlContent))
	c.Assert(err, check.IsNil)
	if si != nil {
		snapInfo.SideInfo = *si
	}
	err = osutil.ChDir(snapSource, func() error {
		var err error
		snapFilePath, err = pack.Pack(snapSource, nil)
		return err
	})
	c.Assert(err, check.IsNil)
	return filepath.Join(snapSource, snapFilePath), snapInfo
}

// MakeSnapFileAndDir makes a squashfs snap file and a directory under
// /snaps/<snap>/<rev> with the given contents. It's a combined effect of
// MakeTestSnapInfoWithFiles and MockSnapWithFiles.
func MakeSnapFileAndDir(c *check.C, snapYamlContent string, files [][]string, si *snap.SideInfo) *snap.Info {
	info := MockSnapWithFiles(c, snapYamlContent, si, files)

	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	defer restoreSanitize()

	err := osutil.ChDir(info.MountDir(), func() error {
		snapName, err := pack.Pack(info.MountDir(), &pack.Options{
			SnapName: info.MountFile(),
		})
		c.Check(snapName, check.Equals, info.MountFile())
		return err
	})
	c.Assert(err, check.IsNil)
	return info
}

// MustParseChannel parses a string representing a store channel and
// includes the given architecture, if architecture is "" the system
// architecture is included. It panics on error.
func MustParseChannel(s string, architecture string) channel.Channel {
	c, err := channel.Parse(s, architecture)
	if err != nil {
		panic(err)
	}
	return c
}

// RenameSlot renames gives an existing slot a new name.
//
// The new slot name cannot clash with an existing plug or slot and must
// be a valid slot name.
func RenameSlot(snapInfo *snap.Info, oldName, newName string) error {
	if snapInfo.Slots[oldName] == nil {
		return fmt.Errorf("cannot rename slot %q to %q: no such slot", oldName, newName)
	}
	if err := snap.ValidateSlotName(newName); err != nil {
		return fmt.Errorf("cannot rename slot %q to %q: %s", oldName, newName, err)
	}
	if oldName == newName {
		return nil
	}
	if snapInfo.Slots[newName] != nil {
		return fmt.Errorf("cannot rename slot %q to %q: existing slot with that name", oldName, newName)
	}
	if snapInfo.Plugs[newName] != nil {
		return fmt.Errorf("cannot rename slot %q to %q: existing plug with that name", oldName, newName)
	}

	// Rename the slot.
	slotInfo := snapInfo.Slots[oldName]
	snapInfo.Slots[newName] = slotInfo
	delete(snapInfo.Slots, oldName)
	slotInfo.Name = newName

	// Update references to the slot in all applications and hooks.
	for _, appInfo := range snapInfo.Apps {
		if _, ok := appInfo.Slots[oldName]; ok {
			delete(appInfo.Slots, oldName)
			appInfo.Slots[newName] = slotInfo
		}
	}
	for _, hookInfo := range snapInfo.Hooks {
		if _, ok := hookInfo.Slots[oldName]; ok {
			delete(hookInfo.Slots, oldName)
			hookInfo.Slots[newName] = slotInfo
		}
	}

	return nil
}

// MockContainer returns a mock snap.Container with the given content.
// If files is empty it still produces a minimal container that passes
// ValidateContainer: / and /meta exist and are 0755, and
// /meta/snap.yaml is a regular world-readable file.
func MockContainer(c *check.C, files [][]string) snap.Container {
	d := c.MkDir()
	c.Assert(os.Chmod(d, 0755), check.IsNil)
	files = append([][]string{{"meta/snap.yaml", ""}}, files...)
	PopulateDir(d, files)
	return snapdir.New(d)
}
