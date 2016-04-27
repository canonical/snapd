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

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/systemd"
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
	// revision will be 0
	snap, err := (&Overlord{}).Install(snapPath, 0, nil)
	c.Assert(err, IsNil)
	c.Check(snap.Name(), Equals, "foo")

	baseDir := filepath.Join(dirs.SnapSnapsDir, fooComposedName, "0")
	c.Assert(osutil.FileExists(baseDir), Equals, true)

	snapEntries := listDir(c, filepath.Join(dirs.SnapSnapsDir, fooComposedName))
	c.Check(snapEntries, DeepEquals, []string{"0", "current"})

	snapDataEntries := listDir(c, filepath.Join(dirs.SnapDataDir, fooComposedName))
	c.Check(snapDataEntries, DeepEquals, []string{"0", "current"})

	return snapPath
}

func (s *SnapTestSuite) TestLocalSnapInstallWithBlessedMetadata(c *C) {
	snapPath := makeTestSnapPackage(c, "")

	si := &snap.SideInfo{
		OfficialName: "foo",
		Revision:     40,
	}

	snap, err := (&Overlord{}).InstallWithSideInfo(snapPath, si, 0, nil)
	c.Assert(err, IsNil)
	c.Check(snap.Name(), Equals, "foo")
	c.Check(snap.Revision, Equals, 40)

	baseDir := filepath.Join(dirs.SnapSnapsDir, fooComposedName, "40")
	c.Assert(osutil.FileExists(baseDir), Equals, true)

	snapEntries := listDir(c, filepath.Join(dirs.SnapSnapsDir, fooComposedName))
	c.Check(snapEntries, DeepEquals, []string{"40", "current"})

	snapDataEntries := listDir(c, filepath.Join(dirs.SnapDataDir, fooComposedName))
	c.Check(snapDataEntries, DeepEquals, []string{"40", "current"})
}

func (s *SnapTestSuite) TestLocalSnapInstallWithBlessedMetadataOverridingName(c *C) {
	snapPath := makeTestSnapPackage(c, "")

	si := &snap.SideInfo{
		OfficialName: "bar",
		Revision:     55,
	}

	snap, err := (&Overlord{}).InstallWithSideInfo(snapPath, si, 0, nil)
	c.Assert(err, IsNil)
	c.Check(snap.Name(), Equals, "bar")
	c.Check(snap.Revision, Equals, 55)

	baseDir := filepath.Join(dirs.SnapSnapsDir, "bar", "55")
	c.Assert(osutil.FileExists(baseDir), Equals, true)
}

// if the snap asks for accepting a license, and an agreer isn't provided,
// install fails
func (s *SnapTestSuite) TestLocalSnapInstallMissingAccepterFails(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
license-agreement: explicit`)
	_, err := (&Overlord{}).Install(pkg, 0, nil)
	c.Check(err, Equals, ErrLicenseNotAccepted)
	c.Check(IsLicenseNotAccepted(err), Equals, true)
}

// if the snap asks for accepting a license, and an agreer is provided, and
// Agreed returns false, install fails
func (s *SnapTestSuite) TestLocalSnapInstallNegAccepterFails(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
license-agreement: explicit`)
	_, err := (&Overlord{}).Install(pkg, 0, &MockProgressMeter{y: false})
	c.Check(err, Equals, ErrLicenseNotAccepted)
	c.Check(IsLicenseNotAccepted(err), Equals, true)
}

func (s *SnapTestSuite) TestLocalSnapInstallMissingAssumes(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
assumes: [f1, f2]`)
	_, err := (&Overlord{}).Install(pkg, 0, &MockProgressMeter{y: false})
	c.Check(err, ErrorMatches, `snap "foo" assumes unsupported features: f1, f2.*`)
}

// if the snap asks for accepting a license, and an agreer is provided, but
// the click has no license, install fails
func (s *SnapTestSuite) TestLocalSnapInstallNoLicenseFails(c *C) {
	licenseChecker = func(string) error { return nil }
	defer func() { licenseChecker = checkLicenseExists }()

	pkg := makeTestSnapPackageFull(c, `
name: foo
version: 1.0
license-agreement: explicit`, false)
	_, err := (&Overlord{}).Install(pkg, 0, &MockProgressMeter{y: true})
	c.Check(err, Equals, ErrLicenseNotProvided)
	c.Check(IsLicenseNotAccepted(err), Equals, false)
}

// if the snap asks for accepting a license, and an agreer is provided, and
// Agreed returns true, install succeeds
func (s *SnapTestSuite) TestLocalSnapInstallPosAccepterWorks(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
license-agreement: explicit`)
	_, err := (&Overlord{}).Install(pkg, 0, &MockProgressMeter{y: true})
	c.Check(err, Equals, nil)
	c.Check(IsLicenseNotAccepted(err), Equals, false)
}

// Agreed is given reasonable values for intro and license
func (s *SnapTestSuite) TestLocalSnapInstallAccepterReasonable(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foobar
version: 1.0
license-agreement: explicit`)
	ag := &MockProgressMeter{y: true}
	_, err := (&Overlord{}).Install(pkg, 0, ag)
	c.Assert(err, Equals, nil)
	c.Check(IsLicenseNotAccepted(err), Equals, false)
	c.Check(ag.intro, Matches, ".*foobar.*requires.*license.*")
	c.Check(ag.license, Equals, "WTFPL")
}

// If a previous version is installed with the same license version, the agreer
// isn't called
func (s *SnapTestSuite) TestPreviouslyAcceptedLicense(c *C) {
	ag := &MockProgressMeter{y: true}
	yaml := `name: foox
license-agreement: explicit
license-version: 2
`
	yamlFile, err := makeInstalledMockSnap(yaml+"version: 1", 11)
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testDeveloper+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	installedSnap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)
	c.Assert(ActivateSnap(installedSnap, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"version: 2")
	_, err = (&Overlord{}).InstallWithSideInfo(pkg, &snap.SideInfo{OfficialName: "foox"}, 0, ag)
	c.Assert(err, Equals, nil)
	c.Check(IsLicenseNotAccepted(err), Equals, false)
	c.Check(ag.intro, Equals, "")
	c.Check(ag.license, Equals, "")
}

// If a previous version is installed with the same license version, but without
// explicit license agreement set, the agreer *is* called
func (s *SnapTestSuite) TestSameLicenseVersionButNotRequired(c *C) {
	ag := &MockProgressMeter{y: true}
	yaml := `name: foox
license-version: 2
version: 1.0
`
	yamlFile, err := makeInstalledMockSnap(yaml+"version: 1", 11)
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testDeveloper+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)
	c.Assert(ActivateSnap(snap, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"version: 2\nlicense-agreement: explicit\n")
	_, err = (&Overlord{}).Install(pkg, 0, ag)
	c.Check(IsLicenseNotAccepted(err), Equals, false)
	c.Assert(err, Equals, nil)
	c.Check(ag.license, Equals, "WTFPL")
}

// If a previous version is installed with a different license version, the
// agreer *is* called
func (s *SnapTestSuite) TestDifferentLicenseVersion(c *C) {
	ag := &MockProgressMeter{y: true}
	yaml := `name: foox
license-agreement: explicit
`
	yamlFile, err := makeInstalledMockSnap(yaml+"license-version: 2\nversion: 1", 11)
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testDeveloper+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)
	c.Assert(ActivateSnap(snap, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"license-version: 3\nversion: 2")
	_, err = (&Overlord{}).Install(pkg, 0, ag)
	c.Assert(err, Equals, nil)
	c.Check(IsLicenseNotAccepted(err), Equals, false)
	c.Check(ag.license, Equals, "WTFPL")
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
	// revision will be 0
	_, err := (&Overlord{}).Install(snapPath, AllowGadget, nil)
	c.Assert(err, IsNil)

	contentFile := filepath.Join(dirs.SnapSnapsDir, "foo", "0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestLocalGadgetSnapInstallVariants(c *C) {
	snapPath := makeTestSnapPackage(c, `name: foo
version: 1.0
type: gadget
`)

	foo10 := &snap.SideInfo{
		OfficialName: "foo",
		Developer:    testDeveloper,
		Revision:     100,
		Channel:      "remote-channel",
	}
	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, foo10, AllowGadget, nil)
	c.Assert(err, IsNil)

	contentFile := filepath.Join(dirs.SnapSnapsDir, "foo", "100", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, IsNil)

	// a package update
	snapPath = makeTestSnapPackage(c, `name: foo
version: 2.0
type: gadget
`)
	foo20 := &snap.SideInfo{
		OfficialName: "foo",
		Developer:    testDeveloper,
		Revision:     200,
		Channel:      "remote-channel",
	}
	_, err = (&Overlord{}).InstallWithSideInfo(snapPath, foo20, 0, nil)
	c.Check(err, IsNil)

	// a package name fork, IOW, a different Gadget package.
	snapPath = makeTestSnapPackage(c, `name: foo-fork
version: 2.0
type: gadget
`)
	_, err = (&Overlord{}).Install(snapPath, 0, nil)
	c.Check(err, Equals, ErrGadgetPackageInstall)

	// this will cause chaos, but let's test if it works
	_, err = (&Overlord{}).Install(snapPath, AllowGadget, nil)
	c.Check(err, IsNil)
}

// sideinfos
var (
	fooSI10 = &snap.SideInfo{
		OfficialName: "foo",
		Revision:     10,
	}

	fooSI20 = &snap.SideInfo{
		OfficialName: "foo",
		Revision:     20,
	}
)

func (s *SnapTestSuite) TestClickSetActive(c *C) {
	snapYamlContent := `name: foo
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, fooSI10, AllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = (&Overlord{}).InstallWithSideInfo(snapPath, fooSI20, AllowUnauthenticated, nil)
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
	err = UnlinkSnap(snaps[1].Info(), nil)
	// set v1 active
	err = ActivateSnap(snaps[0], nil)
	snaps, err = (&Overlord{}).Installed()
	c.Assert(err, IsNil)
	c.Assert(snaps[0].Version(), Equals, "1.0")
	c.Assert(snaps[0].IsActive(), Equals, true)
	c.Assert(snaps[1].Version(), Equals, "2.0")
	c.Assert(snaps[1].IsActive(), Equals, false)

}

func (s *SnapTestSuite) TestCopyData(c *C) {
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "home", "*", "snap")
	homeDir := filepath.Join(s.tempdir, "home", "user1", "snap")
	appDir := "foo"
	homeData := filepath.Join(homeDir, appDir, "10")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)

	snapYamlContent := `name: foo
`
	canaryData := []byte("ni ni ni")

	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err = (&Overlord{}).InstallWithSideInfo(snapPath, fooSI10, AllowUnauthenticated, nil)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "10", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, canaryData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(homeData, "canary.home"), canaryData, 0644)
	c.Assert(err, IsNil)

	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = (&Overlord{}).InstallWithSideInfo(snapPath, fooSI20, AllowUnauthenticated, nil)
	c.Assert(err, IsNil)
	newCanaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "20", "canary.txt")
	content, err := ioutil.ReadFile(newCanaryDataFile)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canaryData)

	newHomeDataCanaryFile := filepath.Join(homeDir, appDir, "20", "canary.home")
	content, err = ioutil.ReadFile(newHomeDataCanaryFile)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canaryData)
}

// ensure that even with no home dir there is no error and the
// system data gets copied
func (s *SnapTestSuite) TestCopyDataNoUserHomes(c *C) {
	// this home dir path does not exist
	oldSnapDataHomeGlob := dirs.SnapDataHomeGlob
	defer func() { dirs.SnapDataHomeGlob = oldSnapDataHomeGlob }()
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "no-such-home", "*", "snap")

	snapYamlContent := `name: foo
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	snap, err := (&Overlord{}).InstallWithSideInfo(snapPath, fooSI10, AllowUnauthenticated, nil)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(snap.DataDir(), "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	snap2, err := (&Overlord{}).InstallWithSideInfo(snapPath, fooSI20, AllowUnauthenticated, nil)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(snap2.DataDir(), "canary.txt"))
	c.Assert(err, IsNil)

	// sanity atm
	c.Check(snap.DataDir(), Not(Equals), snap2.DataDir())
}

func (s *SnapTestSuite) TestSnappyHandleBinariesOnUpgrade(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, fooSI10, AllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file go generated with the right
	// path
	oldSnapBin := filepath.Join(dirs.SnapSnapsDir[len(dirs.GlobalRootDir):], "foo", "10", "bin", "bar")
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	content, err := ioutil.ReadFile(binaryWrapper)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), oldSnapBin), Equals, true)

	// and that it gets updated on upgrade
	snapPath = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = (&Overlord{}).InstallWithSideInfo(snapPath, fooSI20, AllowUnauthenticated, nil)
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
		Revision:     32,
	}

	snapPath := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	// revision will be 0
	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, si, AllowUnauthenticated, nil)
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
	_, err := (&Overlord{}).Install(snapPath, InhibitHooks, nil)
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
	// revision will be 0
	_, err := (&Overlord{}).Install(snapPath, AllowUnauthenticated, nil)
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file go generated with the right
	// name
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	c.Assert(osutil.FileExists(binaryWrapper), Equals, true)

	// and that it gets removed on remove
	snapDir := filepath.Join(dirs.SnapSnapsDir, "foo", "0")
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
		Revision:     55,
	}

	_, err := (&Overlord{}).InstallWithSideInfo(snapPath, si, 0, &MockProgressMeter{})
	c.Assert(err, NotNil)
}
