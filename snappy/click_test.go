// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/arch"
	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/policy"
	"github.com/ubuntu-core/snappy/snap"
	"github.com/ubuntu-core/snappy/snap/squashfs"
	"github.com/ubuntu-core/snappy/systemd"
	"github.com/ubuntu-core/snappy/timeout"
)

func (s *SnapTestSuite) testLocalSnapInstall(c *C) string {
	snapFile := makeTestSnapPackage(c, "")
	name, err := installClick(snapFile, 0, nil, testOrigin)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	baseDir := filepath.Join(dirs.SnapSnapsDir, fooComposedName, "1.0")
	c.Assert(helpers.FileExists(baseDir), Equals, true)
	_, err = os.Stat(filepath.Join(s.tempdir, "var", "lib", "snaps", "foo."+testOrigin, "1.0"))
	c.Assert(err, IsNil)

	return snapFile
}

func (s *SnapTestSuite) TestLocalSnapInstall(c *C) {
	s.testLocalSnapInstall(c)
}

func (s *SnapTestSuite) TestLocalSnapInstallFailsAlreadyInstalled(c *C) {
	snapFile := s.testLocalSnapInstall(c)

	_, err := installClick(snapFile, 0, nil, "originother")
	c.Assert(err, ErrorMatches, ".*is already installed with origin.*")
}

// if the snap asks for accepting a license, and an agreer isn't provided,
// install fails
func (s *SnapTestSuite) TestLocalSnapInstallMissingAccepterFails(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
license-agreement: explicit`)
	_, err := installClick(pkg, 0, nil, testOrigin)
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
	_, err := installClick(pkg, 0, &MockProgressMeter{y: false}, testOrigin)
	c.Check(err, Equals, ErrLicenseNotAccepted)
	c.Check(IsLicenseNotAccepted(err), Equals, true)
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
	_, err := installClick(pkg, 0, &MockProgressMeter{y: true}, testOrigin)
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
	_, err := installClick(pkg, 0, &MockProgressMeter{y: true}, testOrigin)
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
	_, err := installClick(pkg, 0, ag, testOrigin)
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
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml+"version: 1")
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testOrigin+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	part, err := NewInstalledSnapPart(yamlFile, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(part.activate(true, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"version: 2")
	_, err = installClick(pkg, 0, ag, testOrigin)
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
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml+"version: 1")
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testOrigin+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	part, err := NewInstalledSnapPart(yamlFile, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(part.activate(true, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"version: 2\nlicense-agreement: explicit\n")
	_, err = installClick(pkg, 0, ag, testOrigin)
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
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml+"license-version: 2\nversion: 1")
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testOrigin+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	part, err := NewInstalledSnapPart(yamlFile, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(part.activate(true, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"license-version: 3\nversion: 2")
	_, err = installClick(pkg, 0, ag, testOrigin)
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

	targetDir := filepath.Join(s.tempdir, "snaps")
	_, err := installClick(makeTestSnapPackage(c, ""), 0, nil, testOrigin)
	c.Assert(err, IsNil)

	instDir := filepath.Join(targetDir, fooComposedName, "1.0")
	_, err = os.Stat(instDir)
	c.Assert(err, IsNil)

	yamlPath := filepath.Join(instDir, "meta", "snap.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = (&Overlord{}).Uninstall(part, &MockProgressMeter{})
	c.Assert(err, IsNil)

	_, err = os.Stat(instDir)
	c.Assert(err, NotNil)

	// we don't run unneeded systemctl reloads
	c.Assert(allSystemctl, HasLen, 0)
}

func (s *SnapTestSuite) buildFramework(c *C) string {
	allSystemctl := []string{}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		allSystemctl = append(allSystemctl, cmd[0])
		return nil, nil
	}

	tmpdir := c.MkDir()
	appg := filepath.Join(tmpdir, "meta", "framework-policy", "apparmor", "policygroups")
	c.Assert(os.MkdirAll(appg, 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(appg, "one"), []byte("hello"), 0644), IsNil)

	yaml := []byte(`name: hello
version: 1.0.1
type: framework
`)

	yamlFile := filepath.Join(tmpdir, "meta", "snap.yaml")
	c.Assert(ioutil.WriteFile(yamlFile, yaml, 0644), IsNil)
	m, err := parseSnapYamlData(yaml, false)
	c.Assert(err, IsNil)
	snapName := fmt.Sprintf("%s_%s_all.snap", m.Name, m.Version)
	d := squashfs.New(snapName)
	c.Assert(d.Build(tmpdir), IsNil)
	defer os.Remove(snapName)

	_, err = installClick(snapName, 0, nil, testOrigin)
	c.Assert(err, IsNil)

	return snapName
}

func (s *SnapTestSuite) TestSnapInstallPackagePolicyDelta(c *C) {
	secbase := policy.SecBase
	defer func() { policy.SecBase = secbase }()
	policy.SecBase = c.MkDir()

	s.buildFramework(c)

	// ...?

	// rename the policy
	//poldir := filepath.Join(tmpdir, "meta", "framework-policy", "apparmor", "policygroups")

	// _, err := installClick(snapName, 0, nil, testOrigin)
	// c.Assert(err, IsNil)
	// appdir := filepath.Join(s.tempdir, "snaps", "hello.testspacethename", "1.0.1")
	// c.Assert(removeClick(appdir, nil), IsNil)
}

func (s *SnapTestSuite) TestSnapRemovePackagePolicy(c *C) {
	c.Skip("need porting to the new squashfs based tests")

	secbase := policy.SecBase
	defer func() { policy.SecBase = secbase }()
	policy.SecBase = c.MkDir()

	s.buildFramework(c)
	appdir := filepath.Join(s.tempdir, "snaps", "hello", "1.0.1")
	yamlPath := filepath.Join(appdir, "meta", "snap.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = (&Overlord{}).Uninstall(part, &MockProgressMeter{})
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestLocalGadgetSnapInstall(c *C) {
	snapFile := makeTestSnapPackage(c, `name: foo
version: 1.0
type: gadget
`)
	_, err := installClick(snapFile, AllowGadget, nil, testOrigin)
	c.Assert(err, IsNil)

	contentFile := filepath.Join(s.tempdir, "snaps", "foo", "1.0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestLocalGadgetSnapInstallVariants(c *C) {
	snapFile := makeTestSnapPackage(c, `name: foo
version: 1.0
type: gadget
`)
	_, err := installClick(snapFile, AllowGadget, nil, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(storeMinimalRemoteManifest("foo", "foo", testOrigin, "1.0", "", "remote-channel"), IsNil)

	contentFile := filepath.Join(s.tempdir, "snaps", "foo", "1.0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, IsNil)

	// a package update
	snapFile = makeTestSnapPackage(c, `name: foo
version: 2.0
type: gadget
`)
	_, err = installClick(snapFile, 0, nil, testOrigin)
	c.Check(err, IsNil)
	c.Assert(storeMinimalRemoteManifest("foo", "foo", testOrigin, "2.0", "", "remote-channel"), IsNil)

	// XXX: I think this next test now tests something we actually don't
	// want. At least for fwks and apps, sideloading something installed
	// is a no-no. Are Gadgets different in this regard?
	//
	// // different origin, this shows we have no origin support at this
	// // level, but sideloading also works.
	// _, err = installClick(snapFile, 0, nil, SideloadedOrigin)
	// c.Check(err, IsNil)
	// c.Assert(storeMinimalRemoteManifest("foo", "foo", SideloadedOrigin, "1.0", ""), IsNil)

	// a package name fork, IOW, a different Gadget package.
	snapFile = makeTestSnapPackage(c, `name: foo-fork
version: 2.0
type: gadget
`)
	_, err = installClick(snapFile, 0, nil, testOrigin)
	c.Check(err, Equals, ErrGadgetPackageInstall)

	// this will cause chaos, but let's test if it works
	_, err = installClick(snapFile, AllowGadget, nil, testOrigin)
	c.Check(err, IsNil)
}

func (s *SnapTestSuite) TestClickSetActive(c *C) {
	snapYamlContent := `name: foo
`
	snapFile := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)

	// ensure v2 is active
	repo := NewLocalSnapRepository()
	parts, err := repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(parts, HasLen, 2)
	c.Assert(parts[0].Version(), Equals, "1.0")
	c.Assert(parts[0].IsActive(), Equals, false)
	c.Assert(parts[1].Version(), Equals, "2.0")
	c.Assert(parts[1].IsActive(), Equals, true)

	// set v1 active
	err = parts[0].(*SnapPart).activate(false, nil)
	parts, err = repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(parts[0].Version(), Equals, "1.0")
	c.Assert(parts[0].IsActive(), Equals, true)
	c.Assert(parts[1].Version(), Equals, "2.0")
	c.Assert(parts[1].IsActive(), Equals, false)

}

func (s *SnapTestSuite) TestClickCopyData(c *C) {
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "home", "*", "snaps")
	homeDir := filepath.Join(s.tempdir, "home", "user1", "snaps")
	appDir := "foo." + testOrigin
	homeData := filepath.Join(homeDir, appDir, "1.0")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)

	snapYamlContent := `name: foo
`
	canaryData := []byte("ni ni ni")

	snapFile := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, canaryData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(homeData, "canary.home"), canaryData, 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	newCanaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "2.0", "canary.txt")
	content, err := ioutil.ReadFile(newCanaryDataFile)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canaryData)

	newHomeDataCanaryFile := filepath.Join(homeDir, appDir, "2.0", "canary.home")
	content, err = ioutil.ReadFile(newHomeDataCanaryFile)
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, canaryData)
}

// ensure that even with no home dir there is no error and the
// system data gets copied
func (s *SnapTestSuite) TestClickCopyDataNoUserHomes(c *C) {
	// this home dir path does not exist
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "no-such-home", "*", "snaps")

	snapYamlContent := `name: foo
`
	appDir := "foo." + testOrigin
	snapFile := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(dirs.SnapDataDir, appDir, "2.0", "canary.txt"))
	c.Assert(err, IsNil)
}

const expectedWrapper = `#!/bin/sh
set -e

# app info (deprecated)
export SNAP_APP_PATH="/snaps/pastebinit.mvo/1.4.0.0.1/"
export SNAP_APP_DATA_PATH="/var/lib/snaps/pastebinit.mvo/1.4.0.0.1/"
export SNAP_APP_TMPDIR="/tmp/snaps/pastebinit.mvo/1.4.0.0.1/tmp"
export SNAP_APP_USER_DATA_PATH="$HOME/snaps/pastebinit.mvo/1.4.0.0.1/"

# app info
export SNAP="/snaps/pastebinit.mvo/1.4.0.0.1/"
export SNAP_DATA="/var/lib/snaps/pastebinit.mvo/1.4.0.0.1/"
export TMPDIR="/tmp/snaps/pastebinit.mvo/1.4.0.0.1/tmp"
export TEMPDIR="/tmp/snaps/pastebinit.mvo/1.4.0.0.1/tmp"
export SNAP_NAME="pastebinit"
export SNAP_VERSION="1.4.0.0.1"
export SNAP_ORIGIN="mvo"
export SNAP_FULLNAME="pastebinit.mvo"
export SNAP_ARCH="%[1]s"
export SNAP_USER_DATA="$HOME/snaps/pastebinit.mvo/1.4.0.0.1/"

if [ ! -d "$SNAP_USER_DATA" ]; then
   mkdir -p "$SNAP_USER_DATA"
fi
export HOME="$SNAP_USER_DATA"

# export old pwd
export SNAP_OLD_PWD="$(pwd)"
cd /snaps/pastebinit.mvo/1.4.0.0.1/
ubuntu-core-launcher pastebinit.mvo pastebinit.mvo_pastebinit_1.4.0.0.1 /snaps/pastebinit.mvo/1.4.0.0.1/bin/pastebinit "$@"
`

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapper(c *C) {
	binary := &AppYaml{Name: "pastebinit", Command: "bin/pastebinit"}
	pkgPath := "/snaps/pastebinit.mvo/1.4.0.0.1/"
	aaProfile := "pastebinit.mvo_pastebinit_1.4.0.0.1"
	m := snapYaml{Name: "pastebinit",
		Version: "1.4.0.0.1"}

	expected := fmt.Sprintf(expectedWrapper, arch.UbuntuArchitecture())

	generatedWrapper, err := generateSnapBinaryWrapper(binary, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expected)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapperFmk(c *C) {
	binary := &AppYaml{Name: "echo", Command: "bin/echo"}
	pkgPath := "/snaps/fmk/1.4.0.0.1/"
	aaProfile := "fmk_echo_1.4.0.0.1"
	m := snapYaml{Name: "fmk",
		Version: "1.4.0.0.1",
		Type:    "framework"}

	expected := strings.Replace(expectedWrapper, "pastebinit.mvo", "fmk", -1)
	expected = strings.Replace(expected, `NAME="pastebinit"`, `NAME="fmk"`, 1)
	expected = strings.Replace(expected, "mvo", "", -1)
	expected = strings.Replace(expected, "pastebinit", "echo", -1)
	expected = fmt.Sprintf(expected, arch.UbuntuArchitecture())

	generatedWrapper, err := generateSnapBinaryWrapper(binary, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expected)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapperIllegalChars(c *C) {
	binary := &AppYaml{Name: "bin/pastebinit\nSomething nasty"}
	pkgPath := "/snaps/pastebinit.mvo/1.4.0.0.1/"
	aaProfile := "pastebinit.mvo_pastebinit_1.4.0.0.1"
	m := snapYaml{Name: "pastebinit",
		Version: "1.4.0.0.1"}

	_, err := generateSnapBinaryWrapper(binary, pkgPath, aaProfile, &m)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnappyBinPathForBinaryNoExec(c *C) {
	binary := &AppYaml{Name: "pastebinit", Command: "bin/pastebinit"}
	pkgPath := "/snaps/pastebinit.mvo/1.0/"
	c.Assert(binPathForBinary(pkgPath, binary), Equals, "/snaps/pastebinit.mvo/1.0/bin/pastebinit")
}

func (s *SnapTestSuite) TestSnappyBinPathForBinaryWithExec(c *C) {
	binary := &AppYaml{
		Name:    "pastebinit",
		Command: "bin/random-pastebin",
	}
	pkgPath := "/snaps/pastebinit.mvo/1.1/"
	c.Assert(binPathForBinary(pkgPath, binary), Equals, "/snaps/pastebinit.mvo/1.1/bin/random-pastebin")
}

func (s *SnapTestSuite) TestSnappyHandleBinariesOnUpgrade(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapFile := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file go generated with the right
	// path
	oldSnapBin := filepath.Join(dirs.SnapSnapsDir[len(dirs.GlobalRootDir):], "foo.mvo", "1.0", "bin", "bar")
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	content, err := ioutil.ReadFile(binaryWrapper)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), oldSnapBin), Equals, true)

	// and that it gets updated on upgrade
	snapFile = makeTestSnapPackage(c, snapYamlContent+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)
	newSnapBin := filepath.Join(dirs.SnapSnapsDir[len(dirs.GlobalRootDir):], "foo.mvo", "2.0", "bin", "bar")
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
	snapFile := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)

	servicesFile := filepath.Join(dirs.SnapServicesDir, "foo_service_1.0.service")
	c.Assert(helpers.FileExists(servicesFile), Equals, true)
	st, err := os.Stat(servicesFile)
	c.Assert(err, IsNil)
	// should _not_ be executable
	c.Assert(st.Mode().String(), Equals, "-rw-r--r--")

	// and that it gets removed on remove
	snapDir := filepath.Join(dirs.SnapSnapsDir, "foo.mvo", "1.0")
	yamlPath := filepath.Join(snapDir, "meta", "snap.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = (&Overlord{}).Uninstall(part, &MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(servicesFile), Equals, false)
	c.Assert(helpers.FileExists(snapDir), Equals, false)
}

func (s *SnapTestSuite) setupSnappyDependentServices(c *C) (string, *MockProgressMeter) {
	inter := &MockProgressMeter{}
	fmkYaml := `name: fmk
version: 1.0
type: framework
version: `
	fmkFile := makeTestSnapPackage(c, fmkYaml+"1")
	_, err := installClick(fmkFile, AllowUnauthenticated, inter, "")
	c.Assert(err, IsNil)

	snapYamlContent := `name: foo
frameworks:
 - fmk
apps:
 svc1:
   command: bin/hello
   daemon: forking
 svc2:
   command: bin/bye
   daemon: forking
version: `
	snapFile := makeTestSnapPackage(c, snapYamlContent+"1.0")
	_, err = installClick(snapFile, AllowUnauthenticated, inter, testOrigin)
	c.Assert(err, IsNil)

	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapServicesDir, "foo_svc1_1.0.service")), Equals, true)
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapServicesDir, "foo_svc2_1.0.service")), Equals, true)

	return fmkYaml, inter
}

func (s *SnapTestSuite) TestSnappyHandleDependentServicesOnInstall(c *C) {
	c.Skip("needs porting to new squashfs based snap activation!")

	fmkYaml, inter := s.setupSnappyDependentServices(c)

	var cmdlog []string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		cmdlog = append(cmdlog, cmd[0])
		return []byte("ActiveState=inactive\n"), nil
	}

	upFile := makeTestSnapPackage(c, fmkYaml+"2")
	_, err := installClick(upFile, AllowUnauthenticated, inter, "")
	c.Assert(err, IsNil)
	c.Check(cmdlog, DeepEquals, []string{"stop", "show", "stop", "show", "start", "start"})

	// check it got set active
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapSnapsDir, "fmk", "current", "meta", "snap.yaml"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "version: 2"), Equals, true)

	// just in case (cf. the following tests)
	_, err = os.Stat(filepath.Join(dirs.SnapSnapsDir, "fmk", "2"))
	c.Assert(err, IsNil)

}

func (s *SnapTestSuite) TestSnappyHandleDependentServicesOnInstallFailingToStop(c *C) {
	c.Skip("needs porting to new squashfs based snap activation!")

	fmkYaml, inter := s.setupSnappyDependentServices(c)

	anError := errors.New("failure")
	var cmdlog []string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		cmdlog = append(cmdlog, cmd[0])
		if len(cmdlog) == 3 && cmd[0] == "stop" {
			return nil, anError
		}
		return []byte("ActiveState=inactive\n"), nil
	}

	upFile := makeTestSnapPackage(c, fmkYaml+"2")
	_, err := installClick(upFile, AllowUnauthenticated, inter, "")
	c.Check(err, Equals, anError)
	c.Check(cmdlog, DeepEquals, []string{"stop", "show", "stop", "start"})

	// check it got rolled back
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapSnapsDir, "fmk", "current", "meta", "snap.yaml"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "version: 1"), Equals, true)

	// no leftovers from the failed install
	_, err = os.Stat(filepath.Join(dirs.SnapSnapsDir, "fmk", "2"))
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnappyHandleDependentServicesOnInstallFailingToStart(c *C) {
	c.Skip("needs porting to new squashfs based snap activation!")

	fmkYaml, inter := s.setupSnappyDependentServices(c)

	anError := errors.New("failure")
	var cmdlog []string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		cmdlog = append(cmdlog, cmd[0])
		if len(cmdlog) == 6 && cmd[0] == "start" {
			return nil, anError
		}
		return []byte("ActiveState=inactive\n"), nil
	}

	upFile := makeTestSnapPackage(c, fmkYaml+"2")
	_, err := installClick(upFile, AllowUnauthenticated, inter, "")
	c.Assert(err, Equals, anError)
	c.Check(cmdlog, DeepEquals, []string{
		"stop", "show", "stop", "show", "start", "start", // <- this one fails
		"stop", "show", "start", "start",
	})

	// check it got rolled back
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapSnapsDir, "fmk", "current", "meta", "snap.yaml"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "version: 1"), Equals, true)

	// no leftovers from the failed install
	_, err = os.Stat(filepath.Join(dirs.SnapSnapsDir, "fmk", "2"))
	c.Assert(err, NotNil)

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
	snapFile := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := installClick(snapFile, InhibitHooks, nil, testOrigin)
	c.Assert(err, IsNil)

	c.Assert(allSystemctl, HasLen, 0)

}

func (s *SnapTestSuite) TestAddPackageServicesStripsGlobalRootdir(c *C) {
	// ensure that even with a global rootdir the paths in the generated
	// .services file are setup correctly (i.e. that the global root
	// is stripped)
	c.Assert(dirs.GlobalRootDir, Not(Equals), "/")

	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	m, err := parseSnapYamlFile(yamlFile)
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = addPackageServices(m, baseDir, false, nil)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/hello-app_svc1_1.10.service"))
	c.Assert(err, IsNil)

	baseDirWithoutRootPrefix := "/snaps/" + helloAppComposedName + "/1.10"
	verbs := []string{"Start", "Stop", "StopPost"}
	bins := []string{"hello", "goodbye", "missya"}
	for i := range verbs {
		expected := fmt.Sprintf("Exec%s=/usr/bin/ubuntu-core-launcher hello-app.%s %s_svc1_1.10 %s/bin/%s", verbs[i], testOrigin, helloAppComposedName, baseDirWithoutRootPrefix, bins[i])
		c.Check(string(content), Matches, "(?ms).*^"+regexp.QuoteMeta(expected)) // check.v1 adds ^ and $ around the regexp provided
	}
}

func (s *SnapTestSuite) TestAddPackageServicesBusPolicyFramework(c *C) {
	yaml := `name: foo
version: 1
type: framework
apps:
  bar:
    bus-name: foo.bar.baz
    daemon: forking
`
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml)
	c.Assert(err, IsNil)
	m, err := parseSnapYamlFile(yamlFile)
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = addPackageServices(m, baseDir, false, nil)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/dbus-1/system.d/foo_bar_1.conf"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "<allow own=\"foo.bar.baz\"/>\n"), Equals, true)
}

func (s *SnapTestSuite) TestAddPackageServicesBusPolicyNoFramework(c *C) {
	yaml := `name: foo
version: 1
type: app
apps:
  bar:
    bus-name: foo.bar.baz
    daemon: forking
`
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml)
	c.Assert(err, IsNil)
	m, err := parseSnapYamlFile(yamlFile)
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = addPackageServices(m, baseDir, false, nil)
	c.Assert(err, IsNil)

	_, err = ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/dbus-1/system.d/foo_bar_1.conf"))
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestAddPackageBinariesStripsGlobalRootdir(c *C) {
	// ensure that even with a global rootdir the paths in the generated
	// .services file are setup correctly (i.e. that the global root
	// is stripped)
	c.Assert(dirs.GlobalRootDir, Not(Equals), "/")

	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	m, err := parseSnapYamlFile(yamlFile)
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = addPackageBinaries(m, baseDir)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/snaps/bin/hello-app.hello"))
	c.Assert(err, IsNil)

	needle := fmt.Sprintf(`
cd /snaps/hello-app.testspacethename/1.10
ubuntu-core-launcher hello-app.%s hello-app.testspacethename_hello_1.10 /snaps/hello-app.testspacethename/1.10/bin/hello "$@"
`, testOrigin)
	c.Assert(string(content), Matches, "(?ms).*"+regexp.QuoteMeta(needle)+".*")
}

var (
	expectedServiceWrapperFmt = `[Unit]
Description=A fun webserver
%s
X-Snappy=yes

[Service]
ExecStart=/usr/bin/ubuntu-core-launcher xkcd-webserver%s xkcd-webserver%[2]s_xkcd-webserver_0.3.4 /snaps/xkcd-webserver%[2]s/0.3.4/bin/foo start
Restart=on-failure
WorkingDirectory=/snaps/xkcd-webserver%[2]s/0.3.4/
Environment="SNAP_APP=xkcd-webserver_xkcd-webserver_0.3.4" "SNAP=/snaps/xkcd-webserver%[2]s/0.3.4/" "SNAP_DATA=/var/lib/snaps/xkcd-webserver%[2]s/0.3.4/" "TMPDIR=/tmp/snaps/xkcd-webserver%[2]s/0.3.4/tmp" "TEMPDIR=/tmp/snaps/xkcd-webserver%[2]s/0.3.4/tmp" "SNAP_NAME=xkcd-webserver" "SNAP_VERSION=0.3.4" "SNAP_ORIGIN=%[3]s" "SNAP_FULLNAME=xkcd-webserver%[2]s" "SNAP_ARCH=%[5]s" "SNAP_USER_DATA=/root/snaps/xkcd-webserver%[2]s/0.3.4/" "SNAP_APP_PATH=/snaps/xkcd-webserver%[2]s/0.3.4/" "SNAP_APP_DATA_PATH=/var/lib/snaps/xkcd-webserver%[2]s/0.3.4/" "SNAP_APP_TMPDIR=/tmp/snaps/xkcd-webserver%[2]s/0.3.4/tmp" "SNAP_APP_USER_DATA_PATH=/root/snaps/xkcd-webserver%[2]s/0.3.4/"
ExecStop=/usr/bin/ubuntu-core-launcher xkcd-webserver%[2]s xkcd-webserver%[2]s_xkcd-webserver_0.3.4 /snaps/xkcd-webserver%[2]s/0.3.4/bin/foo stop
ExecStopPost=/usr/bin/ubuntu-core-launcher xkcd-webserver%[2]s xkcd-webserver%[2]s_xkcd-webserver_0.3.4 /snaps/xkcd-webserver%[2]s/0.3.4/bin/foo post-stop
TimeoutStopSec=30
%[4]s

[Install]
WantedBy=multi-user.target
`
	expectedServiceAppWrapper     = fmt.Sprintf(expectedServiceWrapperFmt, "After=ubuntu-snappy.frameworks.target\nRequires=ubuntu-snappy.frameworks.target", ".canonical", "canonical", "Type=simple\n", arch.UbuntuArchitecture())
	expectedNetAppWrapper         = fmt.Sprintf(expectedServiceWrapperFmt, "After=ubuntu-snappy.frameworks.target\nRequires=ubuntu-snappy.frameworks.target\nAfter=snappy-wait4network.service\nRequires=snappy-wait4network.service", ".canonical", "canonical", "Type=simple\n", arch.UbuntuArchitecture())
	expectedServiceFmkWrapper     = fmt.Sprintf(expectedServiceWrapperFmt, "Before=ubuntu-snappy.frameworks.target\nAfter=ubuntu-snappy.frameworks-pre.target\nRequires=ubuntu-snappy.frameworks-pre.target", "", "", "Type=dbus\nBusName=foo.bar.baz", arch.UbuntuArchitecture())
	expectedSocketUsingWrapper    = fmt.Sprintf(expectedServiceWrapperFmt, "After=ubuntu-snappy.frameworks.target xkcd-webserver_xkcd-webserver_0.3.4.socket\nRequires=ubuntu-snappy.frameworks.target xkcd-webserver_xkcd-webserver_0.3.4.socket", ".canonical", "canonical", "Type=simple\n", arch.UbuntuArchitecture())
	expectedTypeForkingFmkWrapper = fmt.Sprintf(expectedServiceWrapperFmt, "After=ubuntu-snappy.frameworks.target\nRequires=ubuntu-snappy.frameworks.target", ".canonical", "canonical", "Type=forking\n", arch.UbuntuArchitecture())
)

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceTypeForking(c *C) {
	service := &AppYaml{
		Name:        "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Description: "A fun webserver",
		Daemon:      "forking",
	}
	pkgPath := "/snaps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := snapYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedTypeForkingFmkWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceAppWrapper(c *C) {
	service := &AppYaml{
		Name:        "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Description: "A fun webserver",
		Daemon:      "simple",
	}
	pkgPath := "/snaps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := snapYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedServiceAppWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceAppWrapperWithExternalPort(c *C) {
	service := &AppYaml{
		Name:        "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Description: "A fun webserver",
		Ports:       &Ports{External: map[string]Port{"foo": Port{}}},
		Daemon:      "simple",
	}
	pkgPath := "/snaps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := snapYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedNetAppWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceFmkWrapper(c *C) {
	service := &AppYaml{
		Name:        "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Description: "A fun webserver",
		BusName:     "foo.bar.baz",
		Daemon:      "dbus",
	}
	pkgPath := "/snaps/xkcd-webserver/0.3.4/"
	aaProfile := "xkcd-webserver_xkcd-webserver_0.3.4"
	m := snapYaml{
		Name:    "xkcd-webserver",
		Version: "0.3.4",
		Type:    snap.TypeFramework,
	}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedServiceFmkWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceRestart(c *C) {
	service := &AppYaml{
		Name:        "xkcd-webserver",
		RestartCond: systemd.RestartOnAbort,
		Daemon:      "simple",
	}
	pkgPath := "/snaps/xkcd-webserver/0.3.4/"
	aaProfile := "xkcd-webserver_xkcd-webserver_0.3.4"
	m := snapYaml{
		Name:    "xkcd-webserver",
		Version: "0.3.4",
	}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Matches, `(?ms).*^Restart=on-abort$.*`)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceWrapperWhitelist(c *C) {
	service := &AppYaml{Name: "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Description: "A fun webserver\nExec=foo",
		Daemon:      "simple",
	}
	pkgPath := "/snaps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := snapYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	_, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestServiceWhitelistSimple(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Name: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Description: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Command: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Stop: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{PostStop: "foo"}), IsNil)
}

func (s *SnapTestSuite) TestServiceWhitelistIllegal(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Name: "x\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Description: "foo\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Command: "foo\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Stop: "foo\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{PostStop: "foo\n"}), NotNil)
}

func (s *SnapTestSuite) TestVerifyAppDaemonValue(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Daemon: "oneshot"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Daemon: "nono"}), ErrorMatches, `"daemon" field contains invalid value "nono"`)
}

func (s *SnapTestSuite) TestServiceWhitelistError(c *C) {
	err := verifyAppYaml(&AppYaml{Name: "x\n"})
	c.Assert(err.Error(), Equals, `app description field 'Name' contains illegal "x\n" (legal: '^[A-Za-z0-9/. _#:-]*$')`)
}

func (s *SnapTestSuite) TestBinariesWhitelistSimple(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Name: "foo"}), IsNil)
	c.Assert(verifyAppYaml(&AppYaml{Command: "foo"}), IsNil)
}

func (s *SnapTestSuite) TestUsesWhitelistSimple(c *C) {
	c.Check(verifyUsesYaml(&usesYaml{
		Type: "migration-skill",
		SecurityDefinitions: SecurityDefinitions{
			SecurityTemplate: "foo"},
	}), IsNil)
	c.Check(verifyUsesYaml(&usesYaml{
		Type: "migration-skill",
		SecurityDefinitions: SecurityDefinitions{
			SecurityPolicy: &SecurityPolicyDefinition{
				AppArmor: "foo"},
		},
	}), IsNil)
}

func (s *SnapTestSuite) TestBinariesWhitelistIllegal(c *C) {
	c.Assert(verifyAppYaml(&AppYaml{Name: "test!me"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Name: "x\n"}), NotNil)
	c.Assert(verifyAppYaml(&AppYaml{Command: "x\n"}), NotNil)
}

func (s *SnapTestSuite) TestWrongType(c *C) {
	c.Check(verifyUsesYaml(&usesYaml{
		Type: "some-skill",
	}), ErrorMatches, ".*can not use skill.* only migration-skill supported")
}

func (s *SnapTestSuite) TestUsesWhitelistIllegal(c *C) {
	c.Check(verifyUsesYaml(&usesYaml{
		Type: "migration-skill",
		SecurityDefinitions: SecurityDefinitions{
			SecurityTemplate: "x\n"},
	}), ErrorMatches, ".*contains illegal.*")
	c.Check(verifyUsesYaml(&usesYaml{
		Type: "migration-skill",
		SecurityDefinitions: SecurityDefinitions{
			SecurityPolicy: &SecurityPolicyDefinition{
				AppArmor: "x\n"},
		},
	}), ErrorMatches, ".*contains illegal.*")
}

func (s *SnapTestSuite) TestInstallChecksFrameworks(c *C) {
	snapYamlContent := `name: foo
version: 0.1
frameworks:
  - missing
`
	snapFile := makeTestSnapPackage(c, snapYamlContent)
	_, err := installClick(snapFile, 0, nil, testOrigin)
	c.Assert(err, ErrorMatches, `.*missing framework.*`)
}

func (s *SnapTestSuite) TestRemovePackageServiceKills(c *C) {
	c.Skip("needs porting to new squashfs based snap activation!")

	// make Stop not work
	var sysdLog [][]string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=active\n"), nil
	}
	yamlFile, err := makeInstalledMockSnap(s.tempdir, `name: wat
version: 42
apps:
 wat:
   command: wat
   stop-timeout: 25
   daemon: forking
`)
	c.Assert(err, IsNil)
	m, err := parseSnapYamlFile(yamlFile)
	c.Assert(err, IsNil)
	inter := &MockProgressMeter{}
	c.Check(removePackageServices(m, filepath.Dir(filepath.Dir(yamlFile)), inter), IsNil)
	c.Assert(len(inter.notified) > 0, Equals, true)
	c.Check(inter.notified[len(inter.notified)-1], Equals, "wat_wat_42.service refused to stop, killing.")
	c.Assert(len(sysdLog) >= 3, Equals, true)
	sd1 := sysdLog[len(sysdLog)-3]
	sd2 := sysdLog[len(sysdLog)-2]
	c.Check(sd1, DeepEquals, []string{"kill", "wat_wat_42.service", "-s", "TERM"})
	c.Check(sd2, DeepEquals, []string{"kill", "wat_wat_42.service", "-s", "KILL"})
}

func (s *SnapTestSuite) TestCopySnapDataDirectoryError(c *C) {
	oldPath := c.MkDir()
	newPath := "/nonono-i-can-not-write-here"
	err := copySnapDataDirectory(oldPath, newPath)
	c.Assert(err, DeepEquals, &ErrDataCopyFailed{
		OldPath:  oldPath,
		NewPath:  newPath,
		ExitCode: 1,
	})
}

func (s *SnapTestSuite) TestSnappyGenerateSnapSocket(c *C) {
	service := &AppYaml{Name: "xkcd-webserver",
		Command:      "bin/foo start",
		Description:  "meep",
		Socket:       true,
		ListenStream: "/var/run/docker.sock",
		SocketMode:   "0660",
		SocketUser:   "root",
		SocketGroup:  "adm",
		Daemon:       "simple",
	}
	pkgPath := "/snaps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := snapYaml{
		Name:    "xkcd-webserver",
		Version: "0.3.4"}

	content, err := generateSnapSocketFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(content, Equals, `[Unit]
Description= Socket Unit File
PartOf=xkcd-webserver_xkcd-webserver_0.3.4.service
X-Snappy=yes

[Socket]
ListenStream=/var/run/docker.sock
SocketMode=0660
SocketUser=root
SocketGroup=adm

[Install]
WantedBy=sockets.target
`)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceWithSockte(c *C) {
	service := &AppYaml{
		Name:        "xkcd-webserver",
		Command:     "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: timeout.DefaultTimeout,
		Description: "A fun webserver",
		Socket:      true,
		Daemon:      "simple",
	}
	pkgPath := "/snaps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := snapYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedSocketUsingWrapper)
}

func (s *SnapTestSuite) TestGenerateSnapSocketFile(c *C) {
	srv := &AppYaml{}
	baseDir := "/base/dir"
	aaProfile := "pkg_app_1.0"
	m := &snapYaml{}

	// no socket mode means 0660
	content, err := generateSnapSocketFile(srv, baseDir, aaProfile, m)
	c.Assert(err, IsNil)
	c.Assert(content, Matches, "(?ms).*SocketMode=0660")

	// SocketMode itself is honored
	srv.SocketMode = "0600"
	content, err = generateSnapSocketFile(srv, baseDir, aaProfile, m)
	c.Assert(err, IsNil)
	c.Assert(content, Matches, "(?ms).*SocketMode=0600")

}

func (s *SnapTestSuite) TestSnappyHandleBinariesOnInstall(c *C) {
	snapYamlContent := `name: foo
apps:
 bar:
  command: bin/bar
`
	snapFile := makeTestSnapPackage(c, snapYamlContent+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file go generated with the right
	// name
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	c.Assert(helpers.FileExists(binaryWrapper), Equals, true)

	// and that it gets removed on remove
	snapDir := filepath.Join(dirs.SnapSnapsDir, "foo.mvo", "1.0")
	yamlPath := filepath.Join(snapDir, "meta", "snap.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = (&Overlord{}).Uninstall(part, &MockProgressMeter{})
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(binaryWrapper), Equals, false)
	c.Assert(helpers.FileExists(snapDir), Equals, false)
}
