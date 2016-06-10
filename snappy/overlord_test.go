// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
)

var helloAppYaml = `name: hello-snap
version: 1.0
`

func (s *SnapTestSuite) TestInstalled(c *C) {
	_, err := makeInstalledMockSnap(helloAppYaml, 11)
	c.Assert(err, IsNil)

	installed, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(installed, HasLen, 1)
	c.Assert(installed[0].Name(), Equals, "hello-snap")
}

func listDir(c *C, p string) []string {
	dir, err := os.Open(p)
	if os.IsNotExist(err) {
		return nil
	}
	c.Assert(err, IsNil)
	names, err := dir.Readdirnames(-1)
	sort.Strings(names)
	return names
}

func (s *SnapTestSuite) TestLocalSnapInstall(c *C) string {
	snapPath := makeTestSnapPackage(c, "")
	// XXX Broken test: revision will be unset
	snap, err := (&Overlord{}).Install(snapPath, 0, nil)
	c.Assert(err, IsNil)
	c.Check(snap.Name(), Equals, "foo")

	baseDir := filepath.Join(dirs.SnapSnapsDir, fooComposedName, "unset")
	c.Assert(osutil.FileExists(baseDir), Equals, true)

	snapEntries := listDir(c, filepath.Join(dirs.SnapSnapsDir, fooComposedName))
	c.Check(snapEntries, DeepEquals, []string{"current", "unset"})

	snapDataEntries := listDir(c, filepath.Join(dirs.SnapDataDir, fooComposedName))
	c.Check(snapDataEntries, DeepEquals, []string{"common", "current", "unset"})

	return snapPath
}

func (s *SnapTestSuite) TestLocalSnapInstallWithBlessedMetadata(c *C) {
	snapPath := makeTestSnapPackage(c, "")

	si := &snap.SideInfo{
		OfficialName: "foo",
		Revision:     snap.R(40),
	}

	sn, err := (&Overlord{}).InstallWithSideInfo(snapPath, si, 0, nil)
	c.Assert(err, IsNil)
	c.Check(sn.Name(), Equals, "foo")
	c.Check(sn.Revision, Equals, snap.R(40))

	baseDir := filepath.Join(dirs.SnapSnapsDir, fooComposedName, "40")
	c.Assert(osutil.FileExists(baseDir), Equals, true)

	snapEntries := listDir(c, filepath.Join(dirs.SnapSnapsDir, fooComposedName))
	c.Check(snapEntries, DeepEquals, []string{"40", "current"})

	snapDataEntries := listDir(c, filepath.Join(dirs.SnapDataDir, fooComposedName))
	c.Check(snapDataEntries, DeepEquals, []string{"40", "common", "current"})
}

func (s *SnapTestSuite) TestLocalSnapInstallWithBlessedMetadataOverridingName(c *C) {
	snapPath := makeTestSnapPackage(c, "")

	si := &snap.SideInfo{
		OfficialName: "bar",
		Revision:     snap.R(55),
	}

	sn, err := (&Overlord{}).InstallWithSideInfo(snapPath, si, 0, nil)
	c.Assert(err, IsNil)
	c.Check(sn.Name(), Equals, "bar")
	c.Check(sn.Revision, Equals, snap.R(55))

	baseDir := filepath.Join(dirs.SnapSnapsDir, "bar", "55")
	c.Assert(osutil.FileExists(baseDir), Equals, true)
}

func (s *SnapTestSuite) TestLocalSnapInstallMissingAssumes(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
assumes: [f1, f2]`)
	_, err := (&Overlord{}).Install(pkg, 0, &MockProgressMeter{})
	c.Check(err, ErrorMatches, `snap "foo" assumes unsupported features: f1, f2.*`)
}

func (s *SnapTestSuite) TestLocalSnapInstallProvidedAssumes(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
assumes: [common-data-dir]`)
	_, err := (&Overlord{}).Install(pkg, 0, &MockProgressMeter{})
	c.Check(err, IsNil)
}

func (s *SnapTestSuite) TestSnapRemove(c *C) {
	c.Skip("needs porting to new squashfs based snap activation!")

	allSystemctl := []string{}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		allSystemctl = append(allSystemctl, cmd[0])
		return nil, nil
	}

	targetDir := dirs.SnapSnapsDir
	_, err := (&Overlord{}).Install(makeTestSnapPackage(c, ""), 0, nil)
	c.Assert(err, IsNil)

	instDir := filepath.Join(targetDir, fooComposedName, "1.0")
	_, err = os.Stat(instDir)
	c.Assert(err, IsNil)

	yamlPath := filepath.Join(instDir, "meta", "snap.yaml")
	snap, err := NewInstalledSnap(yamlPath)
	c.Assert(err, IsNil)
	err = (&Overlord{}).Uninstall(snap, &MockProgressMeter{})
	c.Assert(err, IsNil)

	_, err = os.Stat(instDir)
	c.Assert(err, NotNil)

	// we don't run unneeded systemctl reloads
	c.Assert(allSystemctl, HasLen, 0)
}

func (s *SnapTestSuite) TestLocalGadgetSnapInstall(c *C) {
	snapPath := makeTestSnapPackage(c, `name: foo
version: 1.0
type: gadget
`)
	// XXX Broken test: revision will be unset
	_, err := (&Overlord{}).Install(snapPath, LegacyAllowGadget, nil)
	c.Assert(err, IsNil)

	contentFile := filepath.Join(dirs.SnapSnapsDir, "foo", "unset", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, IsNil)
}

// sideinfos
var (
	fooSI10 = &snap.SideInfo{
		OfficialName: "foo",
		Revision:     snap.R(10),
	}

	fooSI20 = &snap.SideInfo{
		OfficialName: "foo",
		Revision:     snap.R(20),
	}
)

func (s *SnapTestSuite) TestClickSetActive(c *C) {
	snapYamlContent := `name: foo
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, fooSI10, LegacyAllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = (&Overlord{}).InstallWithSideInfo(snapPath, fooSI20, LegacyAllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	// ensure v2 is active
	snaps, err := (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(snaps, HasLen, 2)
	c.Assert(snaps[0].Version(), Equals, "1.0")
	c.Assert(snaps[0].IsActive(), Equals, false)
	c.Assert(snaps[1].Version(), Equals, "2.0")
	c.Assert(snaps[1].IsActive(), Equals, true)

	// deactivate v2
	err = unlinkSnap(snaps[1].Info(), nil)
	// set v1 active
	err = ActivateSnap(snaps[0], nil)
	snaps, err = (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(snaps[0].Version(), Equals, "1.0")
	c.Assert(snaps[0].IsActive(), Equals, true)
	c.Assert(snaps[1].Version(), Equals, "2.0")
	c.Assert(snaps[1].IsActive(), Equals, false)

}

func (s *SnapTestSuite) TestSnappyHandleBinariesOnUpgrade(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, fooSI10, LegacyAllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file got generated with the right
	// path
	oldSnapBin := filepath.Join(dirs.SnapSnapsDir[len(dirs.GlobalRootDir):], "foo", "10", "bin", "bar")
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	content, err := ioutil.ReadFile(binaryWrapper)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), oldSnapBin), Equals, true)

	// and that it gets updated on upgrade
	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = (&Overlord{}).InstallWithSideInfo(snapPath, fooSI20, LegacyAllowUnauthenticated, nil)
	c.Assert(err, IsNil)
	newSnapBin := filepath.Join(dirs.SnapSnapsDir[len(dirs.GlobalRootDir):], "foo", "20", "bin", "bar")
	content, err = ioutil.ReadFile(binaryWrapper)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), newSnapBin), Equals, true)
}

func (s *SnapTestSuite) TestSnappyHandleServicesOnInstall(c *C) {
	snapYamlContent := `name: foo
apps:
 service:
   command: bin/hello
   daemon: forking
`
	si := &snap.SideInfo{
		OfficialName: "foo",
		Revision:     snap.R(32),
	}

	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	// XXX Broken test: revision will be unset
	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, si, LegacyAllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	servicesFile := filepath.Join(dirs.SnapServicesDir, "snap.foo.service.service")
	c.Assert(osutil.FileExists(servicesFile), Equals, true)
	st, err := os.Stat(servicesFile)
	c.Assert(err, IsNil)
	// should _not_ be executable
	c.Assert(st.Mode().String(), Equals, "-rw-r--r--")

	// and that it gets removed on remove
	snapDir := filepath.Join(dirs.SnapSnapsDir, "foo", "32")
	yamlPath := filepath.Join(snapDir, "meta", "snap.yaml")
	snap, err := NewInstalledSnap(yamlPath)
	c.Assert(err, IsNil)
	err = (&Overlord{}).Uninstall(snap, &MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(servicesFile), Equals, false)
	c.Assert(osutil.FileExists(snapDir), Equals, false)
}

func (s *SnapTestSuite) TestSnappyHandleServicesOnInstallInhibit(c *C) {
	c.Skip("needs porting to new squashfs based snap activation!")

	allSystemctl := [][]string{}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		allSystemctl = append(allSystemctl, cmd)
		return []byte("ActiveState=inactive\n"), nil
	}

	snapYamlContent := `name: foo
apps:
 service:
   command: bin/hello
   daemon: forking
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := (&Overlord{}).Install(snapPath, LegacyInhibitHooks, nil)
	c.Assert(err, IsNil)

	c.Assert(allSystemctl, HasLen, 0)

}

func (s *SnapTestSuite) TestSnappyHandleBinariesOnInstall(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	// XXX Broken test: revision will be unset
	_, err := (&Overlord{}).Install(snapPath, LegacyAllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file go generated with the right
	// name
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	c.Assert(osutil.FileExists(binaryWrapper), Equals, true)

	// and that it gets removed on remove
	snapDir := filepath.Join(dirs.SnapSnapsDir, "foo", "unset")
	yamlPath := filepath.Join(snapDir, "meta", "snap.yaml")
	snap, err := NewInstalledSnap(yamlPath)
	c.Assert(err, IsNil)
	err = (&Overlord{}).Uninstall(snap, &MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(binaryWrapper), Equals, false)
	c.Assert(osutil.FileExists(snapDir), Equals, false)
}

func (s *SnapTestSuite) TestInstallIncorrectSnapYamlErrors(c *C) {
	c.Skip("no easy path to this kind of late verification failure now!")
	snapPath := makeTestSnapPackage(c, `name: foo
version: 1.0
apps:
 foo:
  plugs: [invalid-chars!!]
`)

	si := &snap.SideInfo{
		OfficialName: "bar",
		Revision:     snap.R(55),
	}

	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, si, 0, &MockProgressMeter{})
	c.Assert(err, NotNil)
}

// Test that openSnapFile has correct snap.SideInfo and snap.Info in leaf objects
// like apps, plugs and slots.
func (s *SnapTestSuite) TestOpenSnapFile(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
plugs:
  plug:
slots:
 slot:
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	// Use InstallWithSideInfo, this is just a cheap way to call openSnapFile
	snapInfo, err := (&Overlord{}).InstallWithSideInfo(snapPath, fooSI10, LegacyAllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	// Ensure that side info is correctly stored
	c.Check(snapInfo.SideInfo, DeepEquals, *fooSI10)
	// Ensure that all leaf objects link back to the same snapInfo with
	// sideInfo and not to some copy.
	c.Check(snapInfo.Apps["bar"].Snap, Equals, snapInfo)
	c.Check(snapInfo.Plugs["plug"].Snap, Equals, snapInfo)
	c.Check(snapInfo.Slots["slot"].Snap, Equals, snapInfo)
}
