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

	"github.com/mvo5/goconfigparser"
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/clickdeb"
	"github.com/ubuntu-core/snappy/policy"
	"github.com/ubuntu-core/snappy/progress"
	"github.com/ubuntu-core/snappy/systemd"
)

func (s *SnapTestSuite) TestReadManifest(c *C) {
	manifestData := []byte(`{
   "description": "This is a simple hello world example.",
    "framework": "ubuntu-core-15.04-dev1",
    "hooks": {
        "echo": {
            "apparmor": "meta/echo.apparmor",
            "bin-path": "bin/echo"
        },
        "env": {
            "apparmor": "meta/env.apparmor",
            "bin-path": "bin/env"
        },
        "evil": {
            "apparmor": "meta/evil.apparmor",
            "bin-path": "bin/evil"
        }
    },
    "icon": "meta/hello.svg",
    "installed-size": "59",
    "maintainer": "Michael Vogt <mvo@ubuntu.com>",
    "name": "hello-world",
    "title": "Hello world example",
    "version": "1.0.5"
}`)
	manifest, err := readClickManifest(manifestData)
	c.Assert(err, IsNil)
	c.Assert(manifest.Name, Equals, "hello-world")
	c.Assert(manifest.Version, Equals, "1.0.5")
	c.Assert(manifest.Hooks["evil"]["bin-path"], Equals, "bin/evil")
	c.Assert(manifest.Hooks["evil"]["apparmor"], Equals, "meta/evil.apparmor")
}

func makeClickHook(c *C, hookContent string) {
	cfg := goconfigparser.New()
	c.Assert(cfg.ReadString("[hook]\n"+hookContent), IsNil)
	hookName, err := cfg.Get("hook", "Hook-Name")
	c.Assert(err, IsNil)

	if _, err := os.Stat(dirs.ClickSystemHooksDir); err != nil {
		os.MkdirAll(dirs.ClickSystemHooksDir, 0755)
	}
	ioutil.WriteFile(filepath.Join(dirs.ClickSystemHooksDir, hookName+".hook"), []byte(hookContent), 0644)
}

func (s *SnapTestSuite) TestReadClickHookFile(c *C) {
	makeClickHook(c, `Hook-Name: systemd
User: root
Exec: /usr/lib/click-systemd/systemd-clickhook
Pattern: /var/lib/systemd/click/${id}`)
	hook, err := readClickHookFile(filepath.Join(dirs.ClickSystemHooksDir, "systemd.hook"))
	c.Assert(err, IsNil)
	c.Assert(hook.name, Equals, "systemd")
	c.Assert(hook.user, Equals, "root")
	c.Assert(hook.exec, Equals, "/usr/lib/click-systemd/systemd-clickhook")
	c.Assert(hook.pattern, Equals, "/var/lib/systemd/click/${id}")

	// click allows non-existing "Hook-Name" and uses the filename then
	makeClickHook(c, `Hook-Name: apparmor
Pattern: /var/lib/apparmor/click/${id}`)
	hook, err = readClickHookFile(filepath.Join(dirs.ClickSystemHooksDir, "apparmor.hook"))
	c.Assert(err, IsNil)
	c.Assert(hook.name, Equals, "apparmor")
}

func (s *SnapTestSuite) TestReadClickHooksDir(c *C) {
	makeClickHook(c, `Hook-Name: systemd
User: root
Exec: /usr/lib/click-systemd/systemd-clickhook
Pattern: /var/lib/systemd/click/${id}`)
	hooks, err := systemClickHooks()
	c.Assert(err, IsNil)
	c.Assert(hooks, HasLen, 1)
	c.Assert(hooks["systemd"].name, Equals, "systemd")
}

func (s *SnapTestSuite) TestHandleClickHooks(c *C) {
	// we can not strip the global rootdir for the hook tests
	stripGlobalRootDir = func(s string) string { return s }

	// two hooks to ensure iterating works correct
	testSymlinkDir := filepath.Join(s.tempdir, "/var/lib/systemd/click/")
	os.MkdirAll(testSymlinkDir, 0755)

	content := `Hook-Name: systemd
Pattern: /var/lib/systemd/click/${id}
`
	makeClickHook(c, content)

	os.MkdirAll(filepath.Join(s.tempdir, "/var/lib/apparmor/click/"), 0755)
	testSymlinkDir2 := filepath.Join(s.tempdir, "/var/lib/apparmor/click/")
	os.MkdirAll(testSymlinkDir2, 0755)
	content = `Hook-Name: apparmor
Pattern: /var/lib/apparmor/click/${id}
`
	makeClickHook(c, content)

	instDir := filepath.Join(s.tempdir, "apps", "foo", "1.0")
	os.MkdirAll(instDir, 0755)
	ioutil.WriteFile(filepath.Join(instDir, "path-to-systemd-file"), []byte(""), 0644)
	ioutil.WriteFile(filepath.Join(instDir, "path-to-apparmor-file"), []byte(""), 0644)
	m := &packageYaml{
		Name:    "foo",
		Version: "1.0",
		Integration: map[string]clickAppHook{
			"app": clickAppHook{
				"systemd":  "path-to-systemd-file",
				"apparmor": "path-to-apparmor-file",
			},
		},
	}
	err := installClickHooks(instDir, m, testOrigin, false)
	c.Assert(err, IsNil)
	p := fmt.Sprintf("%s/%s.%s_%s_%s", testSymlinkDir, m.Name, testOrigin, "app", m.Version)
	_, err = os.Stat(p)
	c.Assert(err, IsNil)
	symlinkTarget, err := filepath.EvalSymlinks(p)
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, filepath.Join(instDir, "path-to-systemd-file"))

	p = fmt.Sprintf("%s/%s.%s_%s_%s", testSymlinkDir2, m.Name, testOrigin, "app", m.Version)
	_, err = os.Stat(p)
	c.Assert(err, IsNil)
	symlinkTarget, err = filepath.EvalSymlinks(p)
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, filepath.Join(instDir, "path-to-apparmor-file"))

	// now ensure we can remove
	err = removeClickHooks(m, testOrigin, false)
	c.Assert(err, IsNil)
	_, err = os.Stat(fmt.Sprintf("%s/%s.%s_%s_%s", testSymlinkDir, m.Name, testOrigin, "app", m.Version))
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) testLocalSnapInstall(c *C) string {
	snapFile := makeTestSnapPackage(c, "")
	name, err := installClick(snapFile, 0, nil, testOrigin)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	baseDir := filepath.Join(dirs.SnapAppsDir, fooComposedName, "1.0")
	contentFile := filepath.Join(baseDir, "bin", "foo")
	content, err := ioutil.ReadFile(contentFile)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "#!/bin/sh\necho \"hello\"")

	// ensure we have the manifest too
	_, err = os.Stat(filepath.Join(baseDir, ".click", "info", fooComposedName+".manifest"))
	c.Assert(err, IsNil)

	// ensure we have the data dir
	_, err = os.Stat(filepath.Join(s.tempdir, "var", "lib", "apps", "foo."+testOrigin, "1.0"))
	c.Assert(err, IsNil)

	// ensure we have the hashes
	snap, err := NewInstalledSnapPart(filepath.Join(baseDir, "meta", "package.yaml"), testOrigin)
	c.Assert(err, IsNil)
	c.Assert(snap.Hash(), Not(Equals), "")

	return snapFile
}

func (s *SnapTestSuite) TestLocalSnapInstall(c *C) {
	s.testLocalSnapInstall(c)
}

func (s *SnapTestSuite) TestLocalSnapInstallFailsAlreadyInstalled(c *C) {
	snapFile := s.testLocalSnapInstall(c)

	_, err := installClick(snapFile, 0, nil, "originother")
	c.Assert(err, Equals, ErrPackageNameAlreadyInstalled)
}

func (s *SnapTestSuite) TestLocalSnapInstallDebsigVerifyFails(c *C) {
	old := clickdeb.VerifyCmd
	clickdeb.VerifyCmd = "false"
	defer func() { clickdeb.VerifyCmd = old }()

	snapFile := makeTestSnapPackage(c, "")
	_, err := installClick(snapFile, 0, nil, testOrigin)
	c.Assert(err, NotNil)

	contentFile := filepath.Join(s.tempdir, "apps", fooComposedName, "1.0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, NotNil)
}

// ensure that the right parameters are passed to runDebsigVerify()
func (s *SnapTestSuite) TestLocalSnapInstallDebsigVerifyPassesUnauth(c *C) {
	// make a fake debsig that fails with unauth
	f := filepath.Join(c.MkDir(), "fakedebsig")
	c.Assert(ioutil.WriteFile(f, []byte("#!/bin/sh\nexit 10\n"), 0755), IsNil)

	old := clickdeb.VerifyCmd
	clickdeb.VerifyCmd = f
	defer func() { clickdeb.VerifyCmd = old }()

	snapFile := makeTestSnapPackage(c, "")
	name, err := installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	_, err = installClick(snapFile, 0, nil, testOrigin)
	c.Assert(err, NotNil)
}

// if the snap asks for accepting a license, and an agreer isn't provided,
// install fails
func (s *SnapTestSuite) TestLocalSnapInstallMissingAccepterFails(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
vendor: foo
explicit-license-agreement: Y`)
	_, err := installClick(pkg, 0, nil, testOrigin)
	c.Check(err, Equals, ErrLicenseNotAccepted)
}

// if the snap asks for accepting a license, and an agreer is provided, and
// Agreed returns false, install fails
func (s *SnapTestSuite) TestLocalSnapInstallNegAccepterFails(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
vendor: foo
explicit-license-agreement: Y`)
	_, err := installClick(pkg, 0, &MockProgressMeter{y: false}, testOrigin)
	c.Check(err, Equals, ErrLicenseNotAccepted)
}

// if the snap asks for accepting a license, and an agreer is provided, but
// the click has no license, install fails
func (s *SnapTestSuite) TestLocalSnapInstallNoLicenseFails(c *C) {
	licenseChecker = func(string) error { return nil }
	defer func() { licenseChecker = checkLicenseExists }()

	pkg := makeTestSnapPackageFull(c, `
name: foo
version: 1.0
vendor: foo
explicit-license-agreement: Y`, false)
	_, err := installClick(pkg, 0, &MockProgressMeter{y: true}, testOrigin)
	c.Check(err, Equals, ErrLicenseNotProvided)
}

// if the snap asks for accepting a license, and an agreer is provided, and
// Agreed returns true, install succeeds
func (s *SnapTestSuite) TestLocalSnapInstallPosAccepterWorks(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foo
version: 1.0
vendor: foo
explicit-license-agreement: Y`)
	_, err := installClick(pkg, 0, &MockProgressMeter{y: true}, testOrigin)
	c.Check(err, Equals, nil)
}

// Agreed is given reasonable values for intro and license
func (s *SnapTestSuite) TestLocalSnapInstallAccepterReasonable(c *C) {
	pkg := makeTestSnapPackage(c, `
name: foobar
version: 1.0
vendor: foo
explicit-license-agreement: Y`)
	ag := &MockProgressMeter{y: true}
	_, err := installClick(pkg, 0, ag, testOrigin)
	c.Assert(err, Equals, nil)
	c.Check(ag.intro, Matches, ".*foobar.*requires.*license.*")
	c.Check(ag.license, Equals, "WTFPL")
}

// If a previous version is installed with the same license version, the agreer
// isn't called
func (s *SnapTestSuite) TestPreviouslyAcceptedLicense(c *C) {
	ag := &MockProgressMeter{y: true}
	yaml := `name: foox
explicit-license-agreement: Y
license-version: 2
vendor: foo
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
vendor: foo
`
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml+"version: 1")
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testOrigin+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	part, err := NewInstalledSnapPart(yamlFile, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(part.activate(true, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"version: 2\nexplicit-license-agreement: Y\nvendor: foo")
	_, err = installClick(pkg, 0, ag, testOrigin)
	c.Assert(err, Equals, nil)
	c.Check(ag.license, Equals, "WTFPL")
}

// If a previous version is installed with a different license version, the
// agreer *is* called
func (s *SnapTestSuite) TestDifferentLicenseVersion(c *C) {
	ag := &MockProgressMeter{y: true}
	yaml := `name: foox
vendor: foo
explicit-license-agreement: Y
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
	c.Check(ag.license, Equals, "WTFPL")
}

func (s *SnapTestSuite) TestSnapRemove(c *C) {
	allSystemctl := []string{}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		allSystemctl = append(allSystemctl, cmd[0])
		return nil, nil
	}

	targetDir := filepath.Join(s.tempdir, "apps")
	_, err := installClick(makeTestSnapPackage(c, ""), 0, nil, testOrigin)
	c.Assert(err, IsNil)

	instDir := filepath.Join(targetDir, fooComposedName, "1.0")
	_, err = os.Stat(instDir)
	c.Assert(err, IsNil)

	yamlPath := filepath.Join(instDir, "meta", "package.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = part.remove(nil)
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
vendor: Foo <foo@example.com>
type: framework
`)

	yamlFile := filepath.Join(tmpdir, "meta", "package.yaml")
	c.Assert(ioutil.WriteFile(yamlFile, yaml, 0644), IsNil)
	readmeMd := filepath.Join(tmpdir, "meta", "readme.md")
	c.Assert(ioutil.WriteFile(readmeMd, []byte("blah\nx"), 0644), IsNil)
	m, err := parsePackageYamlData(yaml, false)
	c.Assert(err, IsNil)
	c.Assert(writeDebianControl(tmpdir, m), IsNil)
	c.Assert(writeClickManifest(tmpdir, m), IsNil)
	snapName := fmt.Sprintf("%s_%s_all.snap", m.Name, m.Version)
	d, err := clickdeb.Create(snapName)
	c.Assert(err, IsNil)
	defer d.Close()
	c.Assert(d.Build(tmpdir, func(dataTar string) error {
		return writeHashes(tmpdir, dataTar)
	}), IsNil)

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
	// appdir := filepath.Join(s.tempdir, "apps", "hello.testspacethename", "1.0.1")
	// c.Assert(removeClick(appdir, nil), IsNil)
}

func (s *SnapTestSuite) TestSnapRemovePackagePolicy(c *C) {
	secbase := policy.SecBase
	defer func() { policy.SecBase = secbase }()
	policy.SecBase = c.MkDir()

	s.buildFramework(c)
	appdir := filepath.Join(s.tempdir, "apps", "hello", "1.0.1")
	yamlPath := filepath.Join(appdir, "meta", "package.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = part.remove(nil)
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestSnapRemovePackagePolicyWeirdClickManifest(c *C) {
	secbase := policy.SecBase
	defer func() { policy.SecBase = secbase }()
	policy.SecBase = c.MkDir()

	s.buildFramework(c)
	appdir := filepath.Join(s.tempdir, "apps", "hello", "1.0.1")
	// c.Assert(removeClick(appdir, nil), IsNil)

	manifestFile := filepath.Join(appdir, ".click", "info", "hello.manifest")
	c.Assert(ioutil.WriteFile(manifestFile, []byte(`{"name": "xyzzy","type":"framework"}`), 0644), IsNil)

	yamlPath := filepath.Join(appdir, "meta", "package.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = part.remove(nil)
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestLocalOemSnapInstall(c *C) {
	snapFile := makeTestSnapPackage(c, `name: foo
version: 1.0
type: oem
icon: foo.svg
vendor: Foo Bar <foo@example.com>`)
	_, err := installClick(snapFile, AllowOEM, nil, testOrigin)
	c.Assert(err, IsNil)

	contentFile := filepath.Join(s.tempdir, "oem", "foo", "1.0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(s.tempdir, "oem", "foo", "1.0", ".click", "info", "foo.manifest"))
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestLocalOemSnapInstallVariants(c *C) {
	snapFile := makeTestSnapPackage(c, `name: foo
version: 1.0
type: oem
icon: foo.svg
vendor: Foo Bar <foo@example.com>`)
	_, err := installClick(snapFile, AllowOEM, nil, testOrigin)
	c.Assert(err, IsNil)
	c.Assert(storeMinimalRemoteManifest("foo", "foo", testOrigin, "1.0", "", "remote-channel"), IsNil)

	contentFile := filepath.Join(s.tempdir, "oem", "foo", "1.0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(s.tempdir, "oem", "foo", "1.0", ".click", "info", "foo.manifest"))
	c.Assert(err, IsNil)

	// a package update
	snapFile = makeTestSnapPackage(c, `name: foo
version: 2.0
type: oem
icon: foo.svg
vendor: Foo Bar <foo@example.com>`)
	_, err = installClick(snapFile, 0, nil, testOrigin)
	c.Check(err, IsNil)
	c.Assert(storeMinimalRemoteManifest("foo", "foo", testOrigin, "2.0", "", "remote-channel"), IsNil)

	// XXX: I think this next test now tests something we actually don't
	// want. At least for fwks and apps, sideloading something installed
	// is a no-no. Are OEMs different in this regard?
	//
	// // different origin, this shows we have no origin support at this
	// // level, but sideloading also works.
	// _, err = installClick(snapFile, 0, nil, SideloadedOrigin)
	// c.Check(err, IsNil)
	// c.Assert(storeMinimalRemoteManifest("foo", "foo", SideloadedOrigin, "1.0", ""), IsNil)

	// a package name fork, IOW, a different OEM package.
	snapFile = makeTestSnapPackage(c, `name: foo-fork
version: 2.0
type: oem
icon: foo.svg
vendor: Foo Bar <foo@example.com>`)
	_, err = installClick(snapFile, 0, nil, testOrigin)
	c.Check(err, Equals, ErrOEMPackageInstall)

	// this will cause chaos, but let's test if it works
	_, err = installClick(snapFile, AllowOEM, nil, testOrigin)
	c.Check(err, IsNil)
}

func (s *SnapTestSuite) TestClickSetActive(c *C) {
	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)

	// ensure v2 is active
	repo := NewLocalSnapRepository(filepath.Join(s.tempdir, "apps"))
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
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "home", "*", "apps")
	homeDir := filepath.Join(s.tempdir, "home", "user1", "apps")
	appDir := "foo." + testOrigin
	homeData := filepath.Join(homeDir, appDir, "1.0")
	err := os.MkdirAll(homeData, 0755)
	c.Assert(err, IsNil)

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	canaryData := []byte("ni ni ni")

	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, canaryData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(homeData, "canary.home"), canaryData, 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
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
	dirs.SnapDataHomeGlob = filepath.Join(s.tempdir, "no-such-home", "*", "apps")

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	appDir := "foo." + testOrigin
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(dirs.SnapDataDir, appDir, "2.0", "canary.txt"))
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestClickCopyRemovesHooksFirst(c *C) {
	// we can not strip the global rootdir for the hook tests
	stripGlobalRootDir = func(s string) string { return s }

	// this hook will create a hook.trace file with the *.hook
	// files generated, this is then later used to verify that
	// the hook files got generated/removed in the right order
	hookContent := fmt.Sprintf(`Hook-Name: tracehook
User: root
Exec: (cd %s && printf "now: $(find . -name "*.tracehook")\n") >> %s/hook.trace
Pattern: /${id}.tracehook`, s.tempdir, s.tempdir)
	makeClickHook(c, hookContent)

	packageYaml := `name: bar
icon: foo.svg
vendor: Foo Bar <foo@example.com>
integration:
 app:
  tracehook: meta/package.yaml
`
	appDir := "bar." + testOrigin
	// install 1.0 and then upgrade to 2.0
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(dirs.SnapDataDir, appDir, "2.0", "canary.txt"))
	c.Assert(err, IsNil)

	// read the hook trace file, this shows that 1.0 was active, then
	// it go de-activated and finally 2.0 got activated
	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "hook.trace"))
	c.Assert(err, IsNil)
	// Forcefully in one line to avoid issues with hidden spaces,
	// it is visually obvious in this form.
	hookRun := fmt.Sprintf("now: ./bar.%s_app_1.0.tracehook\nnow: \nnow: ./bar.%s_app_2.0.tracehook\n", testOrigin, testOrigin)
	c.Assert(string(content), Equals, hookRun)
}

func (s *SnapTestSuite) TestClickCopyDataHookFails(c *C) {
	// we can not strip the global rootdir for the hook tests
	stripGlobalRootDir = func(s string) string { return s }

	// this is a special hook that fails on a 2.0 upgrade, this way
	// we can ensure that upgrades can work
	hookContent := fmt.Sprintf(`Hook-Name: hooky
User: root
Exec: if test -e %s/bar.%s_app_2.0.hooky; then echo "this log message is harmless and can be ignored"; false; fi
Pattern: /${id}.hooky`, s.tempdir, testOrigin)
	makeClickHook(c, hookContent)

	packageYaml := `name: bar
icon: foo.svg
vendor: Foo Bar <foo@example.com>
integration:
 app:
  hooky: meta/package.yaml
`

	appDir := "bar." + testOrigin
	// install 1.0 and then upgrade to 2.0
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(dirs.SnapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testOrigin)
	c.Assert(err, NotNil)

	// installing 2.0 will fail in the hooks,
	//   so ensure we fall back to v1.0
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppsDir, appDir, "current", "meta", "package.yaml"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "version: 1.0"), Equals, true)

	// no leftovers from the failed install
	_, err = os.Stat(filepath.Join(dirs.SnapAppsDir, fooComposedName, "2.0"))
	c.Assert(err, NotNil)
}

const expectedWrapper = `#!/bin/sh
set -e

# app info (deprecated)
export SNAPP_APP_PATH="/apps/pastebinit.mvo/1.4.0.0.1/"
export SNAPP_APP_DATA_PATH="/var/lib/apps/pastebinit.mvo/1.4.0.0.1/"
export SNAPP_APP_TMPDIR="/tmp/snaps/pastebinit.mvo/1.4.0.0.1/tmp"
export SNAPPY_APP_ARCH="%[1]s"
export SNAPP_APP_USER_DATA_PATH="$HOME/apps/pastebinit.mvo/1.4.0.0.1/"
export SNAPP_OLD_PWD="$(pwd)"

# app info
export TMPDIR="/tmp/snaps/pastebinit.mvo/1.4.0.0.1/tmp"
export TEMPDIR="/tmp/snaps/pastebinit.mvo/1.4.0.0.1/tmp"
export SNAP_APP_PATH="/apps/pastebinit.mvo/1.4.0.0.1/"
export SNAP_APP_DATA_PATH="/var/lib/apps/pastebinit.mvo/1.4.0.0.1/"
export SNAP_APP_TMPDIR="/tmp/snaps/pastebinit.mvo/1.4.0.0.1/tmp"
export SNAP_NAME="pastebinit"
export SNAP_VERSION="1.4.0.0.1"
export SNAP_ORIGIN="mvo"
export SNAP_FULLNAME="pastebinit.mvo"
export SNAP_ARCH="%[1]s"
export SNAP_APP_USER_DATA_PATH="$HOME/apps/pastebinit.mvo/1.4.0.0.1/"

if [ ! -d "$SNAP_APP_USER_DATA_PATH" ]; then
   mkdir -p "$SNAP_APP_USER_DATA_PATH"
fi
export HOME="$SNAP_APP_USER_DATA_PATH"

# export old pwd
export SNAP_OLD_PWD="$(pwd)"
cd /apps/pastebinit.mvo/1.4.0.0.1/
ubuntu-core-launcher pastebinit.mvo pastebinit.mvo_pastebinit_1.4.0.0.1 /apps/pastebinit.mvo/1.4.0.0.1/bin/pastebinit "$@"
`

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapper(c *C) {
	binary := Binary{Name: "pastebinit", Exec: "bin/pastebinit"}
	pkgPath := "/apps/pastebinit.mvo/1.4.0.0.1/"
	aaProfile := "pastebinit.mvo_pastebinit_1.4.0.0.1"
	m := packageYaml{Name: "pastebinit",
		Version: "1.4.0.0.1"}

	expected := fmt.Sprintf(expectedWrapper, helpers.UbuntuArchitecture())

	generatedWrapper, err := generateSnapBinaryWrapper(binary, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expected)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapperFmk(c *C) {
	binary := Binary{Name: "echo", Exec: "bin/echo"}
	pkgPath := "/apps/fmk/1.4.0.0.1/"
	aaProfile := "fmk_echo_1.4.0.0.1"
	m := packageYaml{Name: "fmk",
		Version: "1.4.0.0.1",
		Type:    "framework"}

	expected := strings.Replace(expectedWrapper, "pastebinit.mvo", "fmk", -1)
	expected = strings.Replace(expected, `NAME="pastebinit"`, `NAME="fmk"`, 1)
	expected = strings.Replace(expected, "mvo", "", -1)
	expected = strings.Replace(expected, "pastebinit", "echo", -1)
	expected = fmt.Sprintf(expected, helpers.UbuntuArchitecture())

	generatedWrapper, err := generateSnapBinaryWrapper(binary, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expected)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapperIllegalChars(c *C) {
	binary := Binary{Name: "bin/pastebinit\nSomething nasty"}
	pkgPath := "/apps/pastebinit.mvo/1.4.0.0.1/"
	aaProfile := "pastebinit.mvo_pastebinit_1.4.0.0.1"
	m := packageYaml{Name: "pastebinit",
		Version: "1.4.0.0.1"}

	_, err := generateSnapBinaryWrapper(binary, pkgPath, aaProfile, &m)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnappyBinPathForBinaryNoExec(c *C) {
	binary := Binary{Name: "pastebinit", Exec: "bin/pastebinit"}
	pkgPath := "/apps/pastebinit.mvo/1.0/"
	c.Assert(binPathForBinary(pkgPath, binary), Equals, "/apps/pastebinit.mvo/1.0/bin/pastebinit")
}

func (s *SnapTestSuite) TestSnappyBinPathForBinaryWithExec(c *C) {
	binary := Binary{
		Name: "pastebinit",
		Exec: "bin/random-pastebin",
	}
	pkgPath := "/apps/pastebinit.mvo/1.1/"
	c.Assert(binPathForBinary(pkgPath, binary), Equals, "/apps/pastebinit.mvo/1.1/bin/random-pastebin")
}

func (s *SnapTestSuite) TestSnappyHandleBinariesOnInstall(c *C) {
	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
binaries:
 - name: bin/bar
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file go generated with the right
	// name
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	c.Assert(helpers.FileExists(binaryWrapper), Equals, true)

	// and that it gets removed on remove
	snapDir := filepath.Join(dirs.SnapAppsDir, "foo.mvo", "1.0")
	yamlPath := filepath.Join(snapDir, "meta", "package.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = part.remove(nil)
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(binaryWrapper), Equals, false)
	c.Assert(helpers.FileExists(snapDir), Equals, false)
}

func (s *SnapTestSuite) TestSnappyHandleBinariesOnUpgrade(c *C) {
	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
binaries:
 - name: bin/bar
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)

	// ensure that the binary wrapper file go generated with the right
	// path
	oldSnapBin := filepath.Join(dirs.SnapAppsDir[len(dirs.GlobalRootDir):], "foo.mvo", "1.0", "bin", "bar")
	binaryWrapper := filepath.Join(dirs.SnapBinariesDir, "foo.bar")
	content, err := ioutil.ReadFile(binaryWrapper)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), oldSnapBin), Equals, true)

	// and that it gets updated on upgrade
	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)
	newSnapBin := filepath.Join(dirs.SnapAppsDir[len(dirs.GlobalRootDir):], "foo.mvo", "2.0", "bin", "bar")
	content, err = ioutil.ReadFile(binaryWrapper)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), newSnapBin), Equals, true)
}

func (s *SnapTestSuite) TestSnappyHandleServicesOnInstall(c *C) {
	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
services:
 - name: service
   start: bin/hello
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)

	servicesFile := filepath.Join(dirs.SnapServicesDir, "foo_service_1.0.service")
	c.Assert(helpers.FileExists(servicesFile), Equals, true)
	st, err := os.Stat(servicesFile)
	c.Assert(err, IsNil)
	// should _not_ be executable
	c.Assert(st.Mode().String(), Equals, "-rw-r--r--")

	// and that it gets removed on remove
	snapDir := filepath.Join(dirs.SnapAppsDir, "foo.mvo", "1.0")
	yamlPath := filepath.Join(snapDir, "meta", "package.yaml")
	part, err := NewInstalledSnapPart(yamlPath, testOrigin)
	c.Assert(err, IsNil)
	err = part.remove(&progress.NullProgress{})
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(servicesFile), Equals, false)
	c.Assert(helpers.FileExists(snapDir), Equals, false)
}

func (s *SnapTestSuite) setupSnappyDependentServices(c *C) (string, *MockProgressMeter) {
	inter := &MockProgressMeter{}
	fmkYaml := `name: fmk
version: 1.0
vendor: foo
type: framework
version: `
	fmkFile := makeTestSnapPackage(c, fmkYaml+"1")
	_, err := installClick(fmkFile, AllowUnauthenticated, inter, "")
	c.Assert(err, IsNil)

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
frameworks:
 - fmk
services:
 - name: svc1
   start: bin/hello
 - name: svc2
   start: bin/bye
version: `
	snapFile := makeTestSnapPackage(c, packageYaml+"1.0")
	_, err = installClick(snapFile, AllowUnauthenticated, inter, testOrigin)
	c.Assert(err, IsNil)

	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapServicesDir, "foo_svc1_1.0.service")), Equals, true)
	c.Assert(helpers.FileExists(filepath.Join(dirs.SnapServicesDir, "foo_svc2_1.0.service")), Equals, true)

	return fmkYaml, inter
}

func (s *SnapTestSuite) TestSnappyHandleDependentServicesOnInstall(c *C) {
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
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppsDir, "fmk", "current", "meta", "package.yaml"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "version: 2"), Equals, true)

	// just in case (cf. the following tests)
	_, err = os.Stat(filepath.Join(dirs.SnapAppsDir, "fmk", "2"))
	c.Assert(err, IsNil)

}

func (s *SnapTestSuite) TestSnappyHandleDependentServicesOnInstallFailingToStop(c *C) {
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
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppsDir, "fmk", "current", "meta", "package.yaml"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "version: 1"), Equals, true)

	// no leftovers from the failed install
	_, err = os.Stat(filepath.Join(dirs.SnapAppsDir, "fmk", "2"))
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestSnappyHandleDependentServicesOnInstallFailingToStart(c *C) {
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
	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppsDir, "fmk", "current", "meta", "package.yaml"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "version: 1"), Equals, true)

	// no leftovers from the failed install
	_, err = os.Stat(filepath.Join(dirs.SnapAppsDir, "fmk", "2"))
	c.Assert(err, NotNil)

}

func (s *SnapTestSuite) TestSnappyHandleServicesOnInstallInhibit(c *C) {
	allSystemctl := [][]string{}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		allSystemctl = append(allSystemctl, cmd)
		return []byte("ActiveState=inactive\n"), nil
	}

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
services:
 - name: service
   start: bin/hello
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, InhibitHooks, nil, testOrigin)
	c.Assert(err, IsNil)

	c.Assert(allSystemctl, HasLen, 0)

}

func (s *SnapTestSuite) TestLocalSnapInstallRunHooks(c *C) {
	// we can not strip the global rootdir for the hook tests
	stripGlobalRootDir = func(s string) string { return s }

	hookSymlinkDir := filepath.Join(s.tempdir, "/var/lib/click/hooks/systemd")
	c.Assert(os.MkdirAll(hookSymlinkDir, 0755), IsNil)

	hookContent := fmt.Sprintf(`Hook-Name: systemd
User: root
Exec: touch %s/i-ran
Pattern: /var/lib/click/hooks/systemd/${id}`, s.tempdir)
	makeClickHook(c, hookContent)

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
integration:
 app:
  systemd: meta/package.yaml
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")

	// install it
	_, err := installClick(snapFile, 0, nil, testOrigin)
	c.Assert(err, IsNil)

	// verify we have the symlink
	c.Assert(helpers.FileExists(filepath.Join(hookSymlinkDir, fmt.Sprintf("foo.%s_app_1.0", testOrigin))), Equals, true)
	// and the hook exec was called
	c.Assert(helpers.FileExists(filepath.Join(s.tempdir, "i-ran")), Equals, true)
}

func (s *SnapTestSuite) TestLocalSnapInstallInhibitHooks(c *C) {
	// we can not strip the global rootdir for the hook tests
	stripGlobalRootDir = func(s string) string { return s }

	hookSymlinkDir := filepath.Join(s.tempdir, "/var/lib/click/hooks/systemd")
	c.Assert(os.MkdirAll(hookSymlinkDir, 0755), IsNil)

	hookContent := fmt.Sprintf(`Hook-Name: systemd
User: root
Exec: touch %s/i-ran
Pattern: /var/lib/click/hooks/systemd/${id}`, s.tempdir)
	makeClickHook(c, hookContent)

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
integration:
 app:
  systemd: meta/package.yaml
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")

	// install it
	_, err := installClick(snapFile, InhibitHooks, nil, testOrigin)
	c.Assert(err, IsNil)

	// verify we have the symlink
	c.Assert(helpers.FileExists(filepath.Join(hookSymlinkDir, fmt.Sprintf("foo.%s_app_1.0", testOrigin))), Equals, true)
	// but the hook exec was not called
	c.Assert(helpers.FileExists(filepath.Join(s.tempdir, "i-ran")), Equals, false)
}

func (s *SnapTestSuite) TestAddPackageServicesStripsGlobalRootdir(c *C) {
	// ensure that even with a global rootdir the paths in the generated
	// .services file are setup correctly (i.e. that the global root
	// is stripped)
	c.Assert(dirs.GlobalRootDir, Not(Equals), "/")

	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	m, err := parsePackageYamlFile(yamlFile)
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = m.addPackageServices(baseDir, false, nil)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/hello-app_svc1_1.10.service"))
	c.Assert(err, IsNil)

	baseDirWithoutRootPrefix := "/apps/" + helloAppComposedName + "/1.10"
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
vendor: foo
services:
  - name: bar
    bus-name: foo.bar.baz
`
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml)
	c.Assert(err, IsNil)
	m, err := parsePackageYamlFile(yamlFile)
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = m.addPackageServices(baseDir, false, nil)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/dbus-1/system.d/foo_bar_1.conf"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "<allow own=\"foo.bar.baz\"/>\n"), Equals, true)
}

func (s *SnapTestSuite) TestAddPackageServicesBusPolicyNoFramework(c *C) {
	yaml := `name: foo
version: 1
vendor: foo
type: app
services:
  - name: bar
    bus-name: foo.bar.baz
`
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml)
	c.Assert(err, IsNil)
	m, err := parsePackageYamlFile(yamlFile)
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = m.addPackageServices(baseDir, false, nil)
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
	m, err := parsePackageYamlFile(yamlFile)
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = m.addPackageBinaries(baseDir)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/apps/bin/hello-app.hello"))
	c.Assert(err, IsNil)

	needle := fmt.Sprintf(`
cd /apps/hello-app.testspacethename/1.10
ubuntu-core-launcher hello-app.%s hello-app.testspacethename_hello_1.10 /apps/hello-app.testspacethename/1.10/bin/hello "$@"
`, testOrigin)
	c.Assert(string(content), Matches, "(?ms).*"+regexp.QuoteMeta(needle)+".*")
}

var (
	expectedServiceWrapperFmt = `[Unit]
Description=A fun webserver
%s
X-Snappy=yes

[Service]
ExecStart=/usr/bin/ubuntu-core-launcher xkcd-webserver%s xkcd-webserver%[2]s_xkcd-webserver_0.3.4 /apps/xkcd-webserver%[2]s/0.3.4/bin/foo start
Restart=on-failure
WorkingDirectory=/apps/xkcd-webserver%[2]s/0.3.4/
Environment="SNAP_APP=xkcd-webserver_xkcd-webserver_0.3.4" "TMPDIR=/tmp/snaps/xkcd-webserver%[2]s/0.3.4/tmp" "TEMPDIR=/tmp/snaps/xkcd-webserver%[2]s/0.3.4/tmp" "SNAP_APP_PATH=/apps/xkcd-webserver%[2]s/0.3.4/" "SNAP_APP_DATA_PATH=/var/lib/apps/xkcd-webserver%[2]s/0.3.4/" "SNAP_APP_TMPDIR=/tmp/snaps/xkcd-webserver%[2]s/0.3.4/tmp" "SNAP_NAME=xkcd-webserver" "SNAP_VERSION=0.3.4" "SNAP_ORIGIN=%[3]s" "SNAP_FULLNAME=xkcd-webserver%[2]s" "SNAP_ARCH=%[5]s" "SNAP_APP_USER_DATA_PATH=%%h/apps/xkcd-webserver%[2]s/0.3.4/" "SNAPP_APP_PATH=/apps/xkcd-webserver%[2]s/0.3.4/" "SNAPP_APP_DATA_PATH=/var/lib/apps/xkcd-webserver%[2]s/0.3.4/" "SNAPP_APP_TMPDIR=/tmp/snaps/xkcd-webserver%[2]s/0.3.4/tmp" "SNAPPY_APP_ARCH=%[5]s" "SNAPP_APP_USER_DATA_PATH=%%h/apps/xkcd-webserver%[2]s/0.3.4/"
ExecStop=/usr/bin/ubuntu-core-launcher xkcd-webserver%[2]s xkcd-webserver%[2]s_xkcd-webserver_0.3.4 /apps/xkcd-webserver%[2]s/0.3.4/bin/foo stop
ExecStopPost=/usr/bin/ubuntu-core-launcher xkcd-webserver%[2]s xkcd-webserver%[2]s_xkcd-webserver_0.3.4 /apps/xkcd-webserver%[2]s/0.3.4/bin/foo post-stop
TimeoutStopSec=30
%[4]s

[Install]
WantedBy=multi-user.target
`
	expectedServiceAppWrapper     = fmt.Sprintf(expectedServiceWrapperFmt, "After=ubuntu-snappy.frameworks.target\nRequires=ubuntu-snappy.frameworks.target", ".canonical", "canonical", "\n", helpers.UbuntuArchitecture())
	expectedNetAppWrapper         = fmt.Sprintf(expectedServiceWrapperFmt, "After=ubuntu-snappy.frameworks.target\nRequires=ubuntu-snappy.frameworks.target\nAfter=snappy-wait4network.service\nRequires=snappy-wait4network.service", ".canonical", "canonical", "\n", helpers.UbuntuArchitecture())
	expectedServiceFmkWrapper     = fmt.Sprintf(expectedServiceWrapperFmt, "Before=ubuntu-snappy.frameworks.target\nAfter=ubuntu-snappy.frameworks-pre.target\nRequires=ubuntu-snappy.frameworks-pre.target", "", "", "BusName=foo.bar.baz\nType=dbus", helpers.UbuntuArchitecture())
	expectedSocketUsingWrapper    = fmt.Sprintf(expectedServiceWrapperFmt, "After=ubuntu-snappy.frameworks.target xkcd-webserver_xkcd-webserver_0.3.4.socket\nRequires=ubuntu-snappy.frameworks.target xkcd-webserver_xkcd-webserver_0.3.4.socket", ".canonical", "canonical", "\n", helpers.UbuntuArchitecture())
	expectedTypeForkingFmkWrapper = fmt.Sprintf(expectedServiceWrapperFmt, "After=ubuntu-snappy.frameworks.target\nRequires=ubuntu-snappy.frameworks.target", ".canonical", "canonical", "Type=forking\n", helpers.UbuntuArchitecture())
)

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceTypeForking(c *C) {
	service := ServiceYaml{
		Name:        "xkcd-webserver",
		Start:       "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: DefaultTimeout,
		Description: "A fun webserver",
		Forking:     true,
	}
	pkgPath := "/apps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := packageYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedTypeForkingFmkWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceAppWrapper(c *C) {
	service := ServiceYaml{
		Name:        "xkcd-webserver",
		Start:       "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: DefaultTimeout,
		Description: "A fun webserver",
	}
	pkgPath := "/apps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := packageYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedServiceAppWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceAppWrapperWithExternalPort(c *C) {
	service := ServiceYaml{
		Name:        "xkcd-webserver",
		Start:       "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: DefaultTimeout,
		Description: "A fun webserver",
		Ports:       &Ports{External: map[string]Port{"foo": Port{}}},
	}
	pkgPath := "/apps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := packageYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedNetAppWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceFmkWrapper(c *C) {
	service := ServiceYaml{
		Name:        "xkcd-webserver",
		Start:       "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: DefaultTimeout,
		Description: "A fun webserver",
		BusName:     "foo.bar.baz",
	}
	pkgPath := "/apps/xkcd-webserver/0.3.4/"
	aaProfile := "xkcd-webserver_xkcd-webserver_0.3.4"
	m := packageYaml{
		Name:    "xkcd-webserver",
		Version: "0.3.4",
		Type:    pkg.TypeFramework,
	}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedServiceFmkWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceWrapperWhitelist(c *C) {
	service := ServiceYaml{Name: "xkcd-webserver",
		Start:       "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: DefaultTimeout,
		Description: "A fun webserver\nExec=foo",
	}
	pkgPath := "/apps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := packageYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	_, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestServiceWhitelistSimple(c *C) {
	c.Assert(verifyServiceYaml(ServiceYaml{Name: "foo"}), IsNil)
	c.Assert(verifyServiceYaml(ServiceYaml{Description: "foo"}), IsNil)
	c.Assert(verifyServiceYaml(ServiceYaml{Start: "foo"}), IsNil)
	c.Assert(verifyServiceYaml(ServiceYaml{Stop: "foo"}), IsNil)
	c.Assert(verifyServiceYaml(ServiceYaml{PostStop: "foo"}), IsNil)
}

func (s *SnapTestSuite) TestServiceWhitelistIllegal(c *C) {
	c.Assert(verifyServiceYaml(ServiceYaml{Name: "x\n"}), NotNil)
	c.Assert(verifyServiceYaml(ServiceYaml{Description: "foo\n"}), NotNil)
	c.Assert(verifyServiceYaml(ServiceYaml{Start: "foo\n"}), NotNil)
	c.Assert(verifyServiceYaml(ServiceYaml{Stop: "foo\n"}), NotNil)
	c.Assert(verifyServiceYaml(ServiceYaml{PostStop: "foo\n"}), NotNil)
}

func (s *SnapTestSuite) TestServiceWhitelistError(c *C) {
	err := verifyServiceYaml(ServiceYaml{Name: "x\n"})
	c.Assert(err.Error(), Equals, "services description field 'Name' contains illegal 'x\n' (legal: '^[A-Za-z0-9/. _#:-]*$')")
}

func (s *SnapTestSuite) TestBinariesWhitelistSimple(c *C) {
	c.Assert(verifyBinariesYaml(Binary{Name: "foo"}), IsNil)
	c.Assert(verifyBinariesYaml(Binary{Exec: "foo"}), IsNil)
	c.Assert(verifyBinariesYaml(Binary{
		SecurityDefinitions: SecurityDefinitions{
			SecurityTemplate: "foo"},
	}), IsNil)
	c.Assert(verifyBinariesYaml(Binary{
		SecurityDefinitions: SecurityDefinitions{
			SecurityPolicy: &SecurityPolicyDefinition{
				Apparmor: "foo"},
		},
	}), IsNil)
}

func (s *SnapTestSuite) TestBinariesWhitelistIllegal(c *C) {
	c.Assert(verifyBinariesYaml(Binary{Name: "test!me"}), NotNil)
	c.Assert(verifyBinariesYaml(Binary{Name: "x\n"}), NotNil)
	c.Assert(verifyBinariesYaml(Binary{Exec: "x\n"}), NotNil)
	c.Assert(verifyBinariesYaml(Binary{
		SecurityDefinitions: SecurityDefinitions{
			SecurityTemplate: "x\n"},
	}), NotNil)
	c.Assert(verifyBinariesYaml(Binary{
		SecurityDefinitions: SecurityDefinitions{
			SecurityPolicy: &SecurityPolicyDefinition{
				Apparmor: "x\n"},
		},
	}), NotNil)
}

func (s *SnapTestSuite) TestSnappyRunHooks(c *C) {
	hookWasRunStamp := fmt.Sprintf("%s/systemd-was-run", s.tempdir)
	c.Assert(helpers.FileExists(hookWasRunStamp), Equals, false)

	makeClickHook(c, fmt.Sprintf(`Hook-Name: systemd
User: root
Exec: touch %s
Pattern: /var/lib/systemd/click/${id}`, hookWasRunStamp))

	err := RunHooks()
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(hookWasRunStamp), Equals, true)
}

func (s *SnapTestSuite) TestInstallChecksForClashes(c *C) {
	// creating the thing by hand (as build refuses to)...
	tmpdir := c.MkDir()
	os.MkdirAll(filepath.Join(tmpdir, "meta"), 0755)
	yaml := []byte(`name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
services:
 - name: foo
binaries:
 - name: foo
`)
	yamlFile := filepath.Join(tmpdir, "meta", "package.yaml")
	c.Assert(ioutil.WriteFile(yamlFile, yaml, 0644), IsNil)
	readmeMd := filepath.Join(tmpdir, "meta", "readme.md")
	c.Assert(ioutil.WriteFile(readmeMd, []byte("blah\nx"), 0644), IsNil)
	m, err := parsePackageYamlData(yaml, false)
	c.Assert(err, IsNil)
	c.Assert(writeDebianControl(tmpdir, m), IsNil)
	c.Assert(writeClickManifest(tmpdir, m), IsNil)
	snapName := fmt.Sprintf("%s_%s_all.snap", m.Name, m.Version)
	d, err := clickdeb.Create(snapName)
	c.Assert(err, IsNil)
	defer d.Close()
	c.Assert(d.Build(tmpdir, func(dataTar string) error {
		return writeHashes(tmpdir, dataTar)
	}), IsNil)

	_, err = installClick(snapName, 0, nil, testOrigin)
	c.Assert(err, ErrorMatches, ".*binary and service both called foo.*")
}

func (s *SnapTestSuite) TestInstallChecksFrameworks(c *C) {
	packageYaml := `name: foo
version: 0.1
vendor: Foo Bar <foo@example.com>
frameworks:
  - missing
`
	snapFile := makeTestSnapPackage(c, packageYaml)
	_, err := installClick(snapFile, 0, nil, testOrigin)
	c.Assert(err, ErrorMatches, `.*missing framework.*`)
}

func (s *SnapTestSuite) TestInstallClickHooksCallsStripRootDir(c *C) {
	content := `Hook-Name: systemd
Pattern: /var/lib/systemd/click/${id}
`
	makeClickHook(c, content)
	os.MkdirAll(filepath.Join(s.tempdir, "/var/lib/systemd/click/"), 0755)

	m := &packageYaml{
		Name:    "foo",
		Version: "1.0",
		Integration: map[string]clickAppHook{
			"app": clickAppHook{
				"systemd": "path-to-systemd-file",
			},
		},
	}

	stripGlobalRootDirWasCalled := false
	stripGlobalRootDir = func(s string) string {
		stripGlobalRootDirWasCalled = true
		return s
	}

	err := installClickHooks(c.MkDir(), m, testOrigin, false)
	c.Assert(err, IsNil)
	c.Assert(stripGlobalRootDirWasCalled, Equals, true)
}

func (s *SnapTestSuite) TestPackageYamlAddSecurityPolicy(c *C) {
	m, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
vendor: foo
binaries:
 - name: foo
services:
 - name: bar
   start: baz
`), false)
	c.Assert(err, IsNil)

	dirs.SnapSeccompDir = c.MkDir()
	err = m.addSecurityPolicy("/apps/foo.mvo/1.0/")
	c.Assert(err, IsNil)

	binSeccompContent, err := ioutil.ReadFile(filepath.Join(dirs.SnapSeccompDir, "foo.mvo_foo_1.0"))
	c.Assert(string(binSeccompContent), Equals, scFilterGenFakeResult)

	serviceSeccompContent, err := ioutil.ReadFile(filepath.Join(dirs.SnapSeccompDir, "foo.mvo_bar_1.0"))
	c.Assert(string(serviceSeccompContent), Equals, scFilterGenFakeResult)

}

func (s *SnapTestSuite) TestPackageYamlRemoveSecurityPolicy(c *C) {
	m, err := parsePackageYamlData([]byte(`name: foo
version: 1.0
vendor: foo
binaries:
 - name: foo
services:
 - name: bar
   start: baz
`), false)
	c.Assert(err, IsNil)

	dirs.SnapSeccompDir = c.MkDir()
	binSeccomp := filepath.Join(dirs.SnapSeccompDir, "foo.mvo_foo_1.0")
	serviceSeccomp := filepath.Join(dirs.SnapSeccompDir, "foo.mvo_bar_1.0")
	c.Assert(helpers.FileExists(binSeccomp), Equals, false)
	c.Assert(helpers.FileExists(serviceSeccomp), Equals, false)

	// add it now
	err = m.addSecurityPolicy("/apps/foo.mvo/1.0/")
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(binSeccomp), Equals, true)
	c.Assert(helpers.FileExists(serviceSeccomp), Equals, true)

	// ensure that it removes the files on remove
	err = m.removeSecurityPolicy("/apps/foo.mvo/1.0/")
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(binSeccomp), Equals, false)
	c.Assert(helpers.FileExists(serviceSeccomp), Equals, false)
}

func (s *SnapTestSuite) TestRemovePackageServiceKills(c *C) {
	// make Stop not work
	var sysdLog [][]string
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		sysdLog = append(sysdLog, cmd)
		return []byte("ActiveState=active\n"), nil
	}
	yamlFile, err := makeInstalledMockSnap(s.tempdir, `name: wat
version: 42
vendor: WAT <wat@example.com>
icon: meta/wat.ico
services:
 - name: wat
   stop-timeout: 25
`)
	c.Assert(err, IsNil)
	m, err := parsePackageYamlFile(yamlFile)
	c.Assert(err, IsNil)
	inter := &MockProgressMeter{}
	c.Check(m.removePackageServices(filepath.Dir(filepath.Dir(yamlFile)), inter), IsNil)
	c.Assert(len(inter.notified) > 0, Equals, true)
	c.Check(inter.notified[len(inter.notified)-1], Equals, "wat_wat_42.service refused to stop, killing.")
	c.Assert(len(sysdLog) >= 3, Equals, true)
	sd1 := sysdLog[len(sysdLog)-3]
	sd2 := sysdLog[len(sysdLog)-2]
	c.Check(sd1, DeepEquals, []string{"kill", "wat_wat_42.service", "-s", "TERM"})
	c.Check(sd2, DeepEquals, []string{"kill", "wat_wat_42.service", "-s", "KILL"})
}

func (s *SnapTestSuite) TestExecHookCorrectErrType(c *C) {
	err := execHook("false")
	c.Assert(err, DeepEquals, &ErrHookFailed{
		Cmd:      "false",
		ExitCode: 1,
	})
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
	service := ServiceYaml{Name: "xkcd-webserver",
		Start:        "bin/foo start",
		Description:  "meep",
		Socket:       true,
		ListenStream: "/var/run/docker.sock",
		SocketMode:   "0660",
		SocketUser:   "root",
		SocketGroup:  "adm",
	}
	pkgPath := "/apps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := packageYaml{
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
	service := ServiceYaml{
		Name:        "xkcd-webserver",
		Start:       "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: DefaultTimeout,
		Description: "A fun webserver",
		Socket:      true,
	}
	pkgPath := "/apps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := packageYaml{Name: "xkcd-webserver",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedSocketUsingWrapper)
}

func (s *SnapTestSuite) TestWriteCompatManifestJSON(c *C) {
	manifest := []byte(`{
    "name": "hello-world"
}
`)
	manifestJSON := filepath.Join(s.tempdir, "hello-world.some-origin.manifest")

	err := writeCompatManifestJSON(s.tempdir, manifest, "some-origin")
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(manifestJSON), Equals, true)
}

func (s *SnapTestSuite) TestWriteCompatManifestJSONNoFollow(c *C) {
	manifest := []byte(`{
    "name": "hello-world"
}
`)
	manifestJSON := filepath.Join(s.tempdir, "hello-world.some-origin.manifest")
	symlinkTarget := filepath.Join(s.tempdir, "symlink-target")
	os.Symlink(symlinkTarget, manifestJSON)
	c.Assert(helpers.FileExists(symlinkTarget), Equals, false)

	err := writeCompatManifestJSON(s.tempdir, manifest, "some-origin")
	c.Assert(err, IsNil)
	c.Check(helpers.FileExists(manifestJSON), Equals, true)
	c.Check(helpers.FileExists(symlinkTarget), Equals, false)
}
