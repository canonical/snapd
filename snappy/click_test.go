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
	"path"
	"path/filepath"
	"strings"

	"github.com/mvo5/goconfigparser"
	. "launchpad.net/gocheck"

	"launchpad.net/snappy/clickdeb"
	"launchpad.net/snappy/helpers"
	"launchpad.net/snappy/policy"
	"launchpad.net/snappy/progress"
	"launchpad.net/snappy/systemd"
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

	if _, err := os.Stat(clickSystemHooksDir); err != nil {
		os.MkdirAll(clickSystemHooksDir, 0755)
	}
	ioutil.WriteFile(path.Join(clickSystemHooksDir, hookName+".hook"), []byte(hookContent), 0644)
}

func (s *SnapTestSuite) TestReadClickHookFile(c *C) {
	makeClickHook(c, `Hook-Name: systemd
User: root
Exec: /usr/lib/click-systemd/systemd-clickhook
Pattern: /var/lib/systemd/click/${id}`)
	hook, err := readClickHookFile(path.Join(clickSystemHooksDir, "systemd.hook"))
	c.Assert(err, IsNil)
	c.Assert(hook.name, Equals, "systemd")
	c.Assert(hook.user, Equals, "root")
	c.Assert(hook.exec, Equals, "/usr/lib/click-systemd/systemd-clickhook")
	c.Assert(hook.pattern, Equals, "/var/lib/systemd/click/${id}")

	// click allows non-existing "Hook-Name" and uses the filename then
	makeClickHook(c, `Hook-Name: apparmor
Pattern: /var/lib/apparmor/click/${id}`)
	hook, err = readClickHookFile(path.Join(clickSystemHooksDir, "apparmor.hook"))
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
	testSymlinkDir := path.Join(s.tempdir, "/var/lib/systemd/click/")
	os.MkdirAll(testSymlinkDir, 0755)

	content := `Hook-Name: systemd
Pattern: /var/lib/systemd/click/${id}
`
	makeClickHook(c, content)

	os.MkdirAll(path.Join(s.tempdir, "/var/lib/apparmor/click/"), 0755)
	testSymlinkDir2 := path.Join(s.tempdir, "/var/lib/apparmor/click/")
	os.MkdirAll(testSymlinkDir2, 0755)
	content = `Hook-Name: apparmor
Pattern: /var/lib/apparmor/click/${id}
`
	makeClickHook(c, content)

	instDir := path.Join(s.tempdir, "apps", "foo", "1.0")
	os.MkdirAll(instDir, 0755)
	ioutil.WriteFile(path.Join(instDir, "path-to-systemd-file"), []byte(""), 0644)
	ioutil.WriteFile(path.Join(instDir, "path-to-apparmor-file"), []byte(""), 0644)
	manifest := clickManifest{
		Name:    "foo",
		Version: "1.0",
		Hooks: map[string]clickAppHook{
			"app": clickAppHook{
				"systemd":  "path-to-systemd-file",
				"apparmor": "path-to-apparmor-file",
			},
		},
	}
	err := installClickHooks(instDir, manifest, false)
	c.Assert(err, IsNil)
	p := fmt.Sprintf("%s/%s_%s_%s", testSymlinkDir, manifest.Name, "app", manifest.Version)
	_, err = os.Stat(p)
	c.Assert(err, IsNil)
	symlinkTarget, err := filepath.EvalSymlinks(p)
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, path.Join(instDir, "path-to-systemd-file"))

	p = fmt.Sprintf("%s/%s_%s_%s", testSymlinkDir2, manifest.Name, "app", manifest.Version)
	_, err = os.Stat(p)
	c.Assert(err, IsNil)
	symlinkTarget, err = filepath.EvalSymlinks(p)
	c.Assert(err, IsNil)
	c.Assert(symlinkTarget, Equals, path.Join(instDir, "path-to-apparmor-file"))

	// now ensure we can remove
	err = removeClickHooks(manifest, false)
	c.Assert(err, IsNil)
	_, err = os.Stat(fmt.Sprintf("%s/%s_%s_%s", testSymlinkDir, manifest.Name, "app", manifest.Version))
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestLocalSnapInstall(c *C) {
	snapFile := makeTestSnapPackage(c, "")
	name, err := installClick(snapFile, 0, nil, testNamespace)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	baseDir := filepath.Join(snapAppsDir, fooComposedName, "1.0")
	contentFile := filepath.Join(baseDir, "bin", "foo")
	content, err := ioutil.ReadFile(contentFile)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, "#!/bin/sh\necho \"hello\"")

	// ensure we have the manifest too
	_, err = os.Stat(filepath.Join(baseDir, ".click", "info", fooComposedName+".manifest"))
	c.Assert(err, IsNil)

	// ensure we have the data dir
	_, err = os.Stat(path.Join(s.tempdir, "var", "lib", "apps", "foo."+testNamespace, "1.0"))
	c.Assert(err, IsNil)

	// ensure we have the hashes
	snap, err := NewInstalledSnapPart(filepath.Join(baseDir, "meta", "package.yaml"), testNamespace)
	c.Assert(err, IsNil)
	c.Assert(snap.Hash(), Not(Equals), "")
}

func (s *SnapTestSuite) TestLocalSnapInstallDebsigVerifyFails(c *C) {
	runDebsigVerify = func(snapFile string, allowUnauth bool) (err error) {
		return errors.New("something went wrong")
	}

	snapFile := makeTestSnapPackage(c, "")
	_, err := installClick(snapFile, 0, nil, testNamespace)
	c.Assert(err, NotNil)

	contentFile := path.Join(s.tempdir, "apps", fooComposedName, "1.0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, NotNil)
}

// ensure that the right parameters are passed to runDebsigVerify()
func (s *SnapTestSuite) TestLocalSnapInstallDebsigVerifyPassesUnauth(c *C) {
	var expectedUnauth bool
	runDebsigVerify = func(snapFile string, allowUnauth bool) (err error) {
		c.Assert(allowUnauth, Equals, expectedUnauth)
		return nil
	}

	expectedUnauth = true
	snapFile := makeTestSnapPackage(c, "")
	name, err := installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)
	c.Check(name, Equals, "foo")

	expectedUnauth = false
	_, err = installClick(snapFile, 0, nil, testNamespace)
	c.Assert(err, IsNil)
}

type agreerator struct {
	y       bool
	intro   string
	license string
}

func (a *agreerator) Agreed(intro, license string) bool {
	a.intro = intro
	a.license = license
	return a.y
}
func (a *agreerator) Notify(string) {}

// if the snap asks for accepting a license, and an agreer isn't provided,
// install fails
func (s *SnapTestSuite) TestLocalSnapInstallMissingAccepterFails(c *C) {
	pkg := makeTestSnapPackage(c, "explicit-license-agreement: Y")
	_, err := installClick(pkg, 0, nil, testNamespace)
	c.Check(err, Equals, ErrLicenseNotAccepted)
}

// if the snap asks for accepting a license, and an agreer is provided, and
// Agreed returns false, install fails
func (s *SnapTestSuite) TestLocalSnapInstallNegAccepterFails(c *C) {
	pkg := makeTestSnapPackage(c, "explicit-license-agreement: Y")
	_, err := installClick(pkg, 0, &agreerator{y: false}, testNamespace)
	c.Check(err, Equals, ErrLicenseNotAccepted)
}

// if the snap asks for accepting a license, and an agreer is provided, but
// the click has no license, install fails
func (s *SnapTestSuite) TestLocalSnapInstallNoLicenseFails(c *C) {
	licenseChecker = func(string) error { return nil }
	defer func() { licenseChecker = checkLicenseExists }()

	pkg := makeTestSnapPackageFull(c, "explicit-license-agreement: Y", false)
	_, err := installClick(pkg, 0, &agreerator{y: true}, testNamespace)
	c.Check(err, Equals, ErrLicenseNotProvided)
}

// if the snap asks for accepting a license, and an agreer is provided, and
// Agreed returns true, install succeeds
func (s *SnapTestSuite) TestLocalSnapInstallPosAccepterWorks(c *C) {
	pkg := makeTestSnapPackage(c, "explicit-license-agreement: Y")
	_, err := installClick(pkg, 0, &agreerator{y: true}, testNamespace)
	c.Check(err, Equals, nil)
}

// Agreed is given reasonable values for intro and license
func (s *SnapTestSuite) TestLocalSnapInstallAccepterReasonable(c *C) {
	pkg := makeTestSnapPackage(c, "name: foobar\nexplicit-license-agreement: Y")
	ag := &agreerator{y: true}
	_, err := installClick(pkg, 0, ag, testNamespace)
	c.Assert(err, Equals, nil)
	c.Check(ag.intro, Matches, ".*foobar.*requires.*license.*")
	c.Check(ag.license, Equals, "WTFPL")
}

// If a previous version is installed with the same license version, the agreer
// isn't called
func (s *SnapTestSuite) TestPreviouslyAcceptedLicense(c *C) {
	ag := &agreerator{y: true}
	yaml := "name: foox\nexplicit-license-agreement: Y\nlicense-version: 2\n"
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml+"version: 1")
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testNamespace+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	c.Assert(setActiveClick(pkgdir, true, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"version: 2")
	_, err = installClick(pkg, 0, ag, testNamespace)
	c.Assert(err, Equals, nil)
	c.Check(ag.intro, Equals, "")
	c.Check(ag.license, Equals, "")
}

// If a previous version is installed with the same license version, but without
// explicit license agreement set, the agreer *is* called
func (s *SnapTestSuite) TestSameLicenseVersionButNotRequired(c *C) {
	ag := &agreerator{y: true}
	yaml := "name: foox\nlicense-version: 2\n"
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml+"version: 1")
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testNamespace+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	c.Assert(setActiveClick(pkgdir, true, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"version: 2\nexplicit-license-agreement: Y")
	_, err = installClick(pkg, 0, ag, testNamespace)
	c.Assert(err, Equals, nil)
	c.Check(ag.license, Equals, "WTFPL")
}

// If a previous version is installed with a different license version, the
// agreer *is* called
func (s *SnapTestSuite) TestDifferentLicenseVersion(c *C) {
	ag := &agreerator{y: true}
	yaml := "name: foox\nexplicit-license-agreement: Y\n"
	yamlFile, err := makeInstalledMockSnap(s.tempdir, yaml+"license-version: 2\nversion: 1")
	pkgdir := filepath.Dir(filepath.Dir(yamlFile))
	c.Assert(os.MkdirAll(filepath.Join(pkgdir, ".click", "info"), 0755), IsNil)
	c.Assert(ioutil.WriteFile(filepath.Join(pkgdir, ".click", "info", "foox."+testNamespace+".manifest"), []byte(`{"name": "foox"}`), 0644), IsNil)
	c.Assert(setActiveClick(pkgdir, true, ag), IsNil)

	pkg := makeTestSnapPackage(c, yaml+"license-version: 3\nversion: 2")
	_, err = installClick(pkg, 0, ag, testNamespace)
	c.Assert(err, Equals, nil)
	c.Check(ag.license, Equals, "WTFPL")
}

func (s *SnapTestSuite) TestSnapRemove(c *C) {
	allSystemctl := []string{}
	systemd.SystemctlCmd = func(cmd ...string) ([]byte, error) {
		allSystemctl = append(allSystemctl, cmd[0])
		return nil, nil
	}

	targetDir := path.Join(s.tempdir, "apps")
	_, err := installClick(makeTestSnapPackage(c, ""), 0, nil, testNamespace)
	c.Assert(err, IsNil)

	instDir := path.Join(targetDir, fooComposedName, "1.0")
	_, err = os.Stat(instDir)
	c.Assert(err, IsNil)

	err = removeClick(instDir, nil)
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

	yamlFile := path.Join(tmpdir, "meta", "package.yaml")
	c.Assert(ioutil.WriteFile(yamlFile, yaml, 0644), IsNil)
	readmeMd := path.Join(tmpdir, "meta", "readme.md")
	c.Assert(ioutil.WriteFile(readmeMd, []byte("blah\nx"), 0644), IsNil)
	m, err := parsePackageYamlData(yaml)
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

	_, err = installClick(snapName, 0, nil, testNamespace)
	c.Assert(err, IsNil)

	return snapName
}

func (s *SnapTestSuite) TestSnapInstallPackagePolicyDelta(c *C) {
	secbase := policy.SecBase
	defer func() { policy.SecBase = secbase }()
	policy.SecBase = c.MkDir()

	snapName := s.buildFramework(c)
	// rename the policy
	//poldir := filepath.Join(tmpdir, "meta", "framework-policy", "apparmor", "policygroups")

	_, err := installClick(snapName, 0, nil, testNamespace)
	c.Assert(err, IsNil)
	// appdir := filepath.Join(s.tempdir, "apps", "hello.testspacethename", "1.0.1")
	// c.Assert(removeClick(appdir, nil), IsNil)
}

func (s *SnapTestSuite) TestSnapRemovePackagePolicy(c *C) {
	secbase := policy.SecBase
	defer func() { policy.SecBase = secbase }()
	policy.SecBase = c.MkDir()

	s.buildFramework(c)
	appdir := filepath.Join(s.tempdir, "apps", "hello.testspacethename", "1.0.1")
	c.Assert(removeClick(appdir, nil), IsNil)
}

func (s *SnapTestSuite) TestSnapRemovePackagePolicyWeirdClickManifest(c *C) {
	secbase := policy.SecBase
	defer func() { policy.SecBase = secbase }()
	policy.SecBase = c.MkDir()

	s.buildFramework(c)
	appdir := filepath.Join(s.tempdir, "apps", "hello.testspacethename", "1.0.1")
	// c.Assert(removeClick(appdir, nil), IsNil)

	manifestFile := path.Join(appdir, ".click", "info", "hello.testspacethename.manifest")
	c.Assert(ioutil.WriteFile(manifestFile, []byte(`{"name": "xyzzy","type":"framework"}`), 0644), IsNil)

	c.Assert(removeClick(appdir, nil), IsNil)
}

func (s *SnapTestSuite) TestLocalOemSnapInstall(c *C) {
	snapFile := makeTestSnapPackage(c, `name: foo
version: 1.0
type: oem
icon: foo.svg
vendor: Foo Bar <foo@example.com>`)
	_, err := installClick(snapFile, 0, nil, testNamespace)
	c.Assert(err, IsNil)

	contentFile := path.Join(s.tempdir, "oem", fooComposedName, "1.0", "bin", "foo")
	_, err = os.Stat(contentFile)
	c.Assert(err, IsNil)
	_, err = os.Stat(path.Join(s.tempdir, "oem", fooComposedName, "1.0", ".click", "info", fooComposedName+".manifest"))
	c.Assert(err, IsNil)
}

func (s *SnapTestSuite) TestClickSetActive(c *C) {
	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
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
	err = setActiveClick(parts[0].(*SnapPart).basedir, false, nil)
	parts, err = repo.Installed()
	c.Assert(err, IsNil)
	c.Assert(parts[0].Version(), Equals, "1.0")
	c.Assert(parts[0].IsActive(), Equals, true)
	c.Assert(parts[1].Version(), Equals, "2.0")
	c.Assert(parts[1].IsActive(), Equals, false)

}

func (s *SnapTestSuite) TestClickCopyData(c *C) {
	snapDataHomeGlob = filepath.Join(s.tempdir, "home", "*", "apps")
	homeDir := filepath.Join(s.tempdir, "home", "user1", "apps")
	appDir := "foo." + testNamespace
	homeData := filepath.Join(homeDir, appDir, "1.0")
	err := helpers.EnsureDir(homeData, 0755)
	c.Assert(err, IsNil)

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	canaryData := []byte("ni ni ni")

	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(snapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, canaryData, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(homeData, "canary.home"), canaryData, 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)
	newCanaryDataFile := filepath.Join(snapDataDir, appDir, "2.0", "canary.txt")
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
	snapDataHomeGlob = filepath.Join(s.tempdir, "no-such-home", "*", "apps")

	packageYaml := `name: foo
icon: foo.svg
vendor: Foo Bar <foo@example.com>
`
	appDir := "foo." + testNamespace
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(snapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(snapDataDir, appDir, "2.0", "canary.txt"))
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
	appDir := "bar." + testNamespace
	// install 1.0 and then upgrade to 2.0
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(snapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)
	_, err = os.Stat(filepath.Join(snapDataDir, appDir, "2.0", "canary.txt"))
	c.Assert(err, IsNil)

	// read the hook trace file, this shows that 1.0 was active, then
	// it go de-activated and finally 2.0 got activated
	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "hook.trace"))
	c.Assert(err, IsNil)
	// Forcefully in one line to avoid issues with hidden spaces,
	// it is visually obvious in this form.
	hookRun := fmt.Sprintf("now: ./bar.%s_app_1.0.tracehook\nnow: \nnow: ./bar.%s_app_2.0.tracehook\n", testNamespace, testNamespace)
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
Pattern: /${id}.hooky`, s.tempdir, testNamespace)
	makeClickHook(c, hookContent)

	packageYaml := `name: bar
icon: foo.svg
vendor: Foo Bar <foo@example.com>
integration:
 app:
  hooky: meta/package.yaml
`

	appDir := "bar." + testNamespace
	// install 1.0 and then upgrade to 2.0
	snapFile := makeTestSnapPackage(c, packageYaml+"version: 1.0")
	_, err := installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, IsNil)
	canaryDataFile := filepath.Join(snapDataDir, appDir, "1.0", "canary.txt")
	err = ioutil.WriteFile(canaryDataFile, []byte(""), 0644)
	c.Assert(err, IsNil)

	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, testNamespace)
	c.Assert(err, NotNil)

	// installing 2.0 will fail in the hooks,
	//   so ensure we fall back to v1.0
	content, err := ioutil.ReadFile(filepath.Join(snapAppsDir, appDir, "current", "meta", "package.yaml"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "version: 1.0"), Equals, true)

	// no leftovers from the failed install
	_, err = os.Stat(filepath.Join(snapAppsDir, fooComposedName, "2.0"))
	c.Assert(err, NotNil)
}

const expectedWrapper = `#!/bin/sh
# !!!never remove this line!!!
##TARGET=/apps/pastebinit.mvo/1.4.0.0.1/bin/pastebinit

set -e

TMPDIR="/tmp/snaps/pastebinit.mvo/1.4.0.0.1/tmp"
if [ ! -d "$TMPDIR" ]; then
    mkdir -p -m1777 "$TMPDIR"
fi
export TMPDIR
export TEMPDIR="$TMPDIR"

# app paths (deprecated)
export SNAPP_APP_PATH="/apps/pastebinit.mvo/1.4.0.0.1/"
export SNAPP_APP_DATA_PATH="/var/lib//apps/pastebinit.mvo/1.4.0.0.1/"
export SNAPP_APP_USER_DATA_PATH="$HOME//apps/pastebinit.mvo/1.4.0.0.1/"
export SNAPP_APP_TMPDIR="$TMPDIR"
export SNAPP_OLD_PWD="$(pwd)"

# app paths
export SNAP_APP_PATH="/apps/pastebinit.mvo/1.4.0.0.1/"
export SNAP_APP_DATA_PATH="/var/lib//apps/pastebinit.mvo/1.4.0.0.1/"
export SNAP_APP_USER_DATA_PATH="$HOME//apps/pastebinit.mvo/1.4.0.0.1/"
export SNAP_APP_TMPDIR="$TMPDIR"

# FIXME: this will need to become snappy arch or something
export SNAPPY_APP_ARCH="$(dpkg --print-architecture)"

if [ ! -d "$SNAP_APP_USER_DATA_PATH" ]; then
   mkdir -p "$SNAP_APP_USER_DATA_PATH"
fi
export HOME="$SNAP_APP_USER_DATA_PATH"

# export old pwd
export SNAP_OLD_PWD="$(pwd)"
cd /apps/pastebinit.mvo/1.4.0.0.1/
aa-exec -p pastebinit.mvo_pastebinit_1.4.0.0.1 -- /apps/pastebinit.mvo/1.4.0.0.1/bin/pastebinit "$@"
`

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapper(c *C) {
	binary := Binary{Name: "pastebinit", Exec: "bin/pastebinit"}
	pkgPath := "/apps/pastebinit.mvo/1.4.0.0.1/"
	aaProfile := "pastebinit.mvo_pastebinit_1.4.0.0.1"
	m := packageYaml{Name: "pastebinit.mvo",
		Version: "1.4.0.0.1"}

	generatedWrapper, err := generateSnapBinaryWrapper(binary, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapBinaryWrapperIllegalChars(c *C) {
	binary := Binary{Name: "bin/pastebinit\nSomething nasty"}
	pkgPath := "/apps/pastebinit.mvo/1.4.0.0.1/"
	aaProfile := "pastebinit.mvo_pastebinit_1.4.0.0.1"
	m := packageYaml{Name: "pastebinit.mvo",
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
	binaryWrapper := filepath.Join(snapBinariesDir, "foo.bar")
	c.Assert(helpers.FileExists(binaryWrapper), Equals, true)

	// and that it gets removed on remove
	snapDir := filepath.Join(snapAppsDir, "foo.mvo", "1.0")
	err = removeClick(snapDir, nil)
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
	oldSnapBin := filepath.Join(snapAppsDir[len(globalRootDir):], "foo.mvo", "1.0", "bin", "bar")
	binaryWrapper := filepath.Join(snapBinariesDir, "foo.bar")
	content, err := ioutil.ReadFile(binaryWrapper)
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), oldSnapBin), Equals, true)

	// and that it gets updated on upgrade
	snapFile = makeTestSnapPackage(c, packageYaml+"version: 2.0")
	_, err = installClick(snapFile, AllowUnauthenticated, nil, "mvo")
	c.Assert(err, IsNil)
	newSnapBin := filepath.Join(snapAppsDir[len(globalRootDir):], "foo.mvo", "2.0", "bin", "bar")
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

	servicesFile := filepath.Join(snapServicesDir, "foo_service_1.0.service")
	c.Assert(helpers.FileExists(servicesFile), Equals, true)
	st, err := os.Stat(servicesFile)
	c.Assert(err, IsNil)
	// should _not_ be executable
	c.Assert(st.Mode().String(), Equals, "-rw-r--r--")

	// and that it gets removed on remove
	snapDir := filepath.Join(snapAppsDir, "foo.mvo", "1.0")
	err = removeClick(snapDir, new(progress.NullProgress))
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(servicesFile), Equals, false)
	c.Assert(helpers.FileExists(snapDir), Equals, false)
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
	_, err := installClick(snapFile, InhibitHooks, nil, testNamespace)
	c.Assert(err, IsNil)

	c.Assert(allSystemctl, HasLen, 0)

}

func (s *SnapTestSuite) TestFindBinaryInPath(c *C) {
	fakeBinDir := c.MkDir()
	runMePath := filepath.Join(fakeBinDir, "runme")
	err := ioutil.WriteFile(runMePath, []byte(""), 0755)
	c.Assert(err, IsNil)

	p := filepath.Join(fakeBinDir, "not-executable")
	err = ioutil.WriteFile(p, []byte(""), 0644)
	c.Assert(err, IsNil)

	fakePATH := fmt.Sprintf("/some/dir:%s", fakeBinDir)
	c.Assert(findBinaryInPath("runme", fakePATH), Equals, runMePath)
	c.Assert(findBinaryInPath("no-such-binary-nowhere", fakePATH), Equals, "")
	c.Assert(findBinaryInPath("not-executable", fakePATH), Equals, "")
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
	_, err := installClick(snapFile, 0, nil, testNamespace)
	c.Assert(err, IsNil)

	// verify we have the symlink
	c.Assert(helpers.FileExists(filepath.Join(hookSymlinkDir, fmt.Sprintf("foo.%s_app_1.0", testNamespace))), Equals, true)
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
	_, err := installClick(snapFile, InhibitHooks, nil, testNamespace)
	c.Assert(err, IsNil)

	// verify we have the symlink
	c.Assert(helpers.FileExists(filepath.Join(hookSymlinkDir, fmt.Sprintf("foo.%s_app_1.0", testNamespace))), Equals, true)
	// but the hook exec was not called
	c.Assert(helpers.FileExists(filepath.Join(s.tempdir, "i-ran")), Equals, false)
}

func (s *SnapTestSuite) TestAddPackageServicesStripsGlobalRootdir(c *C) {
	// ensure that even with a global rootdir the paths in the generated
	// .services file are setup correctly (i.e. that the global root
	// is stripped)
	c.Assert(globalRootDir, Not(Equals), "/")

	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = addPackageServices(baseDir, false, nil)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/etc/systemd/system/hello-app_svc1_1.10.service"))
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(content), "\nExecStart=/apps/"+helloAppComposedName+"/1.10/bin/hello\n"), Equals, true)
}

func (s *SnapTestSuite) TestAddPackageBinariesStripsGlobalRootdir(c *C) {
	// ensure that even with a global rootdir the paths in the generated
	// .services file are setup correctly (i.e. that the global root
	// is stripped)
	c.Assert(globalRootDir, Not(Equals), "/")

	yamlFile, err := makeInstalledMockSnap(s.tempdir, "")
	c.Assert(err, IsNil)
	baseDir := filepath.Dir(filepath.Dir(yamlFile))
	err = addPackageBinaries(baseDir)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "/apps/bin/hello-app.hello"))
	c.Assert(err, IsNil)

	needle := `
cd /apps/hello-app.testspacethename/1.10
aa-exec -p hello-app.testspacethename_hello_1.10 -- /apps/hello-app.testspacethename/1.10/bin/hello "$@"
`
	c.Assert(strings.Contains(string(content), needle), Equals, true)
}

var expectedServiceWrapper = `[Unit]
Description=A fun webserver
After=ubuntu-snappy.run-hooks.service
X-Snappy=yes

[Service]
ExecStart=/apps/xkcd-webserver.canonical/0.3.4/bin/foo start
WorkingDirectory=/apps/xkcd-webserver.canonical/0.3.4/
Environment="SNAPP_APP_PATH=/apps/xkcd-webserver.canonical/0.3.4/" "SNAPP_APP_DATA_PATH=/var/lib/apps/xkcd-webserver.canonical/0.3.4/" "SNAPP_APP_USER_DATA_PATH=%h/apps/xkcd-webserver.canonical/0.3.4/" "SNAP_APP_PATH=/apps/xkcd-webserver.canonical/0.3.4/" "SNAP_APP_DATA_PATH=/var/lib/apps/xkcd-webserver.canonical/0.3.4/" "SNAP_APP_USER_DATA_PATH=%h/apps/xkcd-webserver.canonical/0.3.4/" "SNAP_APP=xckd-webserver.canonical_xkcd-webserver_0.3.4" "TMPDIR=/tmp/snaps/xckd-webserver.canonical/0.3.4/tmp" "SNAP_APP_TMPDIR=/tmp/snaps/xckd-webserver.canonical/0.3.4/tmp"
AppArmorProfile=xkcd-webserver.canonical_xkcd-webserver_0.3.4
ExecStop=/apps/xkcd-webserver.canonical/0.3.4/bin/foo stop
ExecStopPost=/apps/xkcd-webserver.canonical/0.3.4/bin/foo post-stop
TimeoutStopSec=30
BusName=foo.bar.baz
Type=dbus

[Install]
WantedBy=multi-user.target
`

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceWrapper(c *C) {
	service := Service{Name: "xkcd-webserver",
		Start:       "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: DefaultTimeout,
		Description: "A fun webserver",
		BusName:     "foo.bar.baz",
	}
	pkgPath := "/apps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := packageYaml{Name: "xckd-webserver.canonical",
		Version: "0.3.4"}

	generatedWrapper, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, IsNil)
	c.Assert(generatedWrapper, Equals, expectedServiceWrapper)
}

func (s *SnapTestSuite) TestSnappyGenerateSnapServiceWrapperWhitelist(c *C) {
	service := Service{Name: "xkcd-webserver",
		Start:       "bin/foo start",
		Stop:        "bin/foo stop",
		PostStop:    "bin/foo post-stop",
		StopTimeout: DefaultTimeout,
		Description: "A fun webserver\nExec=foo",
	}
	pkgPath := "/apps/xkcd-webserver.canonical/0.3.4/"
	aaProfile := "xkcd-webserver.canonical_xkcd-webserver_0.3.4"
	m := packageYaml{Name: "xckd-webserver.canonical",
		Version: "0.3.4"}

	_, err := generateSnapServicesFile(service, pkgPath, aaProfile, &m)
	c.Assert(err, NotNil)
}

func (s *SnapTestSuite) TestServiceWhitelistSimple(c *C) {
	c.Assert(verifyServiceYaml(Service{Name: "foo"}), IsNil)
	c.Assert(verifyServiceYaml(Service{Description: "foo"}), IsNil)
	c.Assert(verifyServiceYaml(Service{Start: "foo"}), IsNil)
	c.Assert(verifyServiceYaml(Service{Stop: "foo"}), IsNil)
	c.Assert(verifyServiceYaml(Service{PostStop: "foo"}), IsNil)
}

func (s *SnapTestSuite) TestServiceWhitelistIllegal(c *C) {
	c.Assert(verifyServiceYaml(Service{Name: "x\n"}), NotNil)
	c.Assert(verifyServiceYaml(Service{Description: "foo\n"}), NotNil)
	c.Assert(verifyServiceYaml(Service{Start: "foo\n"}), NotNil)
	c.Assert(verifyServiceYaml(Service{Stop: "foo\n"}), NotNil)
	c.Assert(verifyServiceYaml(Service{PostStop: "foo\n"}), NotNil)
}

func (s *SnapTestSuite) TestServiceWhitelistError(c *C) {
	err := verifyServiceYaml(Service{Name: "x\n"})
	c.Assert(err.Error(), Equals, `services description field 'Name' contains illegal 'x
' (legal: '^[A-Za-z0-9/. _#:-]*$')`)
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
			SecurityPolicy: &SecurityOverrideDefinition{
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
			SecurityPolicy: &SecurityOverrideDefinition{
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
	os.MkdirAll(path.Join(tmpdir, "meta"), 0755)
	yaml := []byte(`name: hello
version: 1.0.1
vendor: Foo <foo@example.com>
services:
 - name: foo
binaries:
 - name: foo
`)
	yamlFile := path.Join(tmpdir, "meta", "package.yaml")
	c.Assert(ioutil.WriteFile(yamlFile, yaml, 0644), IsNil)
	readmeMd := path.Join(tmpdir, "meta", "readme.md")
	c.Assert(ioutil.WriteFile(readmeMd, []byte("blah\nx"), 0644), IsNil)
	m, err := parsePackageYamlData(yaml)
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

	_, err = installClick(snapName, 0, nil, testNamespace)
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
	_, err := installClick(snapFile, 0, nil, testNamespace)
	c.Assert(err, ErrorMatches, `.*missing framework.*`)
}

func (s *SnapTestSuite) TestInstallClickHooksCallsStripRootDir(c *C) {
	content := `Hook-Name: systemd
Pattern: /var/lib/systemd/click/${id}
`
	makeClickHook(c, content)
	os.MkdirAll(path.Join(s.tempdir, "/var/lib/systemd/click/"), 0755)

	manifest := clickManifest{
		Name:    "foo",
		Version: "1.0",
		Hooks: map[string]clickAppHook{
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

	err := installClickHooks(c.MkDir(), manifest, false)
	c.Assert(err, IsNil)
	c.Assert(stripGlobalRootDirWasCalled, Equals, true)
}
