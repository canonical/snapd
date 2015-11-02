// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/pkg"
)

type SecurityTestSuite struct {
	buildDir              string
	m                     *packageYaml
	scFilterGenCall       []string
	scFilterGenCallReturn []byte

	aaPolicyDir string
	scPolicyDir string
}

var _ = Suite(&SecurityTestSuite{})

func (a *SecurityTestSuite) SetUpTest(c *C) {
	a.buildDir = c.MkDir()
	os.MkdirAll(filepath.Join(a.buildDir, "meta"), 0755)

	a.m = &packageYaml{
		Name:        "foo",
		Version:     "1.0",
		Integration: make(map[string]clickAppHook),
	}

	a.aaPolicyDir = aaPolicyDir
	aaPolicyDir = c.MkDir()
	a.scPolicyDir = scPolicyDir
	scPolicyDir = c.MkDir()
}

func (a *SecurityTestSuite) TearDownTest(c *C) {
	aaPolicyDir = a.aaPolicyDir
	scPolicyDir = a.scPolicyDir
}

func makeMockApparmorTemplate(c *C, templateName string, content []byte) {
	mockTemplate := filepath.Join(aaPolicyDir, "templates", defaultPolicyVendor, defaultPolicyVersion, templateName)
	err := os.MkdirAll(filepath.Dir(mockTemplate), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockTemplate, content, 0644)
	c.Assert(err, IsNil)
}

func makeMockApparmorCap(c *C, capname string, content []byte) {
	mockPG := filepath.Join(aaPolicyDir, "policygroups", defaultPolicyVendor, defaultPolicyVersion, capname)
	err := os.MkdirAll(filepath.Dir(mockPG), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(mockPG, []byte(content), 0644)
	c.Assert(err, IsNil)
}

func makeMockSeccompTemplate(c *C, templateName string, content []byte) {
	mockTemplate := filepath.Join(scPolicyDir, "templates", defaultPolicyVendor, defaultPolicyVersion, templateName)
	err := os.MkdirAll(filepath.Dir(mockTemplate), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockTemplate, content, 0644)
	c.Assert(err, IsNil)
}

func makeMockSeccompCap(c *C, capname string, content []byte) {
	mockPG := filepath.Join(scPolicyDir, "policygroups", defaultPolicyVendor, defaultPolicyVersion, capname)
	err := os.MkdirAll(filepath.Dir(mockPG), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(mockPG, []byte(content), 0644)
	c.Assert(err, IsNil)
}

func (a *SecurityTestSuite) TestSnappyGetSecurityProfile(c *C) {
	m := packageYaml{
		Name:    "foo",
		Version: "1.0",
	}
	b := Binary{Name: "bin/app"}
	ap, err := getSecurityProfile(&m, b.Name, "/apps/foo.mvo/1.0/")
	c.Assert(err, IsNil)
	c.Check(ap, Equals, "foo.mvo_bin-app_1.0")
}

func (a *SecurityTestSuite) TestSnappyGetSecurityProfileInvalid(c *C) {
	m := packageYaml{
		Name:    "foo",
		Version: "1.0",
	}
	b := Binary{Name: "bin/app"}
	_, err := getSecurityProfile(&m, b.Name, "/apps/foo/1.0/")
	c.Assert(err, Equals, ErrInvalidPart)
}

func (a *SecurityTestSuite) TestSnappyGetSecurityProfileFramework(c *C) {
	m := packageYaml{
		Name:    "foo",
		Version: "1.0",
		Type:    pkg.TypeFramework,
	}
	b := Binary{Name: "bin/app"}
	ap, err := getSecurityProfile(&m, b.Name, "/apps/foo.mvo/1.0/")
	c.Assert(err, IsNil)
	c.Check(ap, Equals, "foo_bin-app_1.0")
}

func (a *SecurityTestSuite) TestSnappyFindUbuntuVersion(c *C) {
	realLsbRelease := lsbRelease
	defer func() { lsbRelease = realLsbRelease }()

	lsbRelease = filepath.Join(c.MkDir(), "mock-lsb-release")
	s := `DISTRIB_RELEASE=18.09`
	err := ioutil.WriteFile(lsbRelease, []byte(s), 0644)
	c.Assert(err, IsNil)

	ver, err := findUbuntuVersion()
	c.Assert(err, IsNil)
	c.Assert(ver, Equals, "18.09")
}

func (a *SecurityTestSuite) TestSnappyFindUbuntuVersionNotFound(c *C) {
	realLsbRelease := lsbRelease
	defer func() { lsbRelease = realLsbRelease }()

	lsbRelease = filepath.Join(c.MkDir(), "mock-lsb-release")
	s := `silly stuff`
	err := ioutil.WriteFile(lsbRelease, []byte(s), 0644)
	c.Assert(err, IsNil)

	_, err = findUbuntuVersion()
	c.Assert(err, Equals, ErrSystemVersionNotFound)
}

func (a *SecurityTestSuite) TestSecurityGenDbusPath(c *C) {
	c.Assert(dbusPath("foo"), Equals, "foo")
	c.Assert(dbusPath("foo bar"), Equals, "foo_20bar")
	c.Assert(dbusPath("foo/bar"), Equals, "foo_2fbar")
}

func (a *SecurityTestSuite) TestSecurityFindWhitespacePrefix(c *C) {
	t := `  ###POLICYGROUPS###`
	c.Assert(findWhitespacePrefix(t, "###POLICYGROUPS###"), Equals, "  ")

	t = `not there`
	c.Assert(findWhitespacePrefix(t, "###POLICYGROUPS###"), Equals, "")
}

// FIXME: need additional test for frameworkPolicy
func (a *SecurityTestSuite) TestSecurityFindTemplateApparmor(c *C) {
	makeMockApparmorTemplate(c, "mock-template", []byte(`something`))

	t, err := findTemplate("mock-template", "apparmor")
	c.Assert(err, IsNil)
	c.Assert(t, Matches, "something")
}

func (a *SecurityTestSuite) TestSecurityFindTemplateApparmorNotFound(c *C) {
	_, err := findTemplate("not-available-templ", "apparmor")
	c.Assert(err, DeepEquals, &errPolicyNotFound{"template", "not-available-templ"})
}

// FIXME: need additional test for frameworkPolicy
func (a *SecurityTestSuite) TestSecurityFindCaps(c *C) {
	aaPolicyDir = c.MkDir()
	for _, f := range []string{"cap1", "cap2"} {
		makeMockApparmorCap(c, f, []byte(f))
	}

	cap, err := findCaps([]string{"cap1", "cap2"}, "mock-template", "apparmor")
	c.Assert(err, IsNil)
	c.Assert(cap, Equals, "cap1\ncap2")
}

func (a *SecurityTestSuite) TestSecurityGetAppArmorVars(c *C) {
	appID := &securityAppID{
		Appname: "foo",
		Version: "1.0",
		AppID:   "id",
		Pkgname: "pkgname",
	}
	c.Assert(getAppArmorVars(appID), Equals, `
# Specified profile variables
@{APP_APPNAME}="foo"
@{APP_ID_DBUS}="id"
@{APP_PKGNAME_DBUS}="pkgname"
@{APP_PKGNAME}="pkgname"
@{APP_VERSION}="1.0"
@{INSTALL_DIR}="{/apps,/oem}"
# Deprecated:
@{CLICK_DIR}="{/apps,/oem}"`)
}

func (a *SecurityTestSuite) TestSecurityGenAppArmorPathRuleSimple(c *C) {
	pr, err := genAppArmorPathRule("/some/path", "rk")
	c.Assert(err, IsNil)
	c.Assert(pr, Equals, "/some/path rk,\n")
}

func (a *SecurityTestSuite) TestSecurityGenAppArmorPathRuleDir(c *C) {
	pr, err := genAppArmorPathRule("/some/path/", "rk")
	c.Assert(err, IsNil)
	c.Assert(pr, Equals, `/some/path/ rk,
/some/path/** rk,
`)
}

func (a *SecurityTestSuite) TestSecurityGenAppArmorPathRuleDirGlob(c *C) {
	pr, err := genAppArmorPathRule("/some/path/**", "rk")
	c.Assert(err, IsNil)
	c.Assert(pr, Equals, `/some/path/ rk,
/some/path/** rk,
`)
}

func (a *SecurityTestSuite) TestSecurityGenAppArmorPathRuleHome(c *C) {
	pr, err := genAppArmorPathRule("/home/something", "rk")
	c.Assert(err, IsNil)
	c.Assert(pr, Equals, "owner /home/something rk,\n")
}

func (a *SecurityTestSuite) TestSecurityGenAppArmorPathRuleError(c *C) {
	_, err := genAppArmorPathRule("some/path", "rk")
	c.Assert(err, Equals, errPolicyGen)
}

var mockApparmorTemplate = []byte(`
# Description: Allows unrestricted access to the system
# Usage: reserved

# vim:syntax=apparmor

#include <tunables/global>

# Define vars with unconfined since autopilot rules may reference them
###VAR###

# v2 compatible wildly permissive profile
###PROFILEATTACH### (attach_disconnected) {
  capability,
  network,
  / rwkl,
  /** rwlkm,
  # Ubuntu Core is a minimal system so don't use 'pix' here. There are few
  # profiles to transition to, and those that exist either won't work right
  # anyway (eg, ubuntu-core-launcher) or would need to be modified to work
  # with snaps (dhclient).
  /** ix,

  mount,
  remount,

  ###ABSTRACTIONS###

  ###POLICYGROUPS###

  ###READS###

  ###WRITES###
}`)

var expectedGeneratedAaProfile = `
# Description: Allows unrestricted access to the system
# Usage: reserved

# vim:syntax=apparmor

#include <tunables/global>

# Define vars with unconfined since autopilot rules may reference them
# Specified profile variables
@{APP_APPNAME}=""
@{APP_ID_DBUS}=""
@{APP_PKGNAME_DBUS}="foo"
@{APP_PKGNAME}="foo"
@{APP_VERSION}="1.0"
@{INSTALL_DIR}="{/apps,/oem}"
# Deprecated:
@{CLICK_DIR}="{/apps,/oem}"

# v2 compatible wildly permissive profile
profile "" (attach_disconnected) {
  capability,
  network,
  / rwkl,
  /** rwlkm,
  # Ubuntu Core is a minimal system so don't use 'pix' here. There are few
  # profiles to transition to, and those that exist either won't work right
  # anyway (eg, ubuntu-core-launcher) or would need to be modified to work
  # with snaps (dhclient).
  /** ix,

  mount,
  remount,

  # No abstractions specified

  # Rules specified via caps (policy groups)
  capito

  # No read paths specified

  # No write paths specified
}`

func (a *SecurityTestSuite) TestSecurityGenAppArmorTemplatePolicy(c *C) {
	makeMockApparmorTemplate(c, "mock-template", mockApparmorTemplate)
	makeMockApparmorCap(c, "cap1", []byte(`capito`))

	m := &packageYaml{
		Name:    "foo",
		Version: "1.0",
	}
	appid := &securityAppID{
		Pkgname: "foo",
		Version: "1.0",
	}
	template := "mock-template"
	caps := []string{"cap1"}
	overrides := &SecurityAppArmorOverrideDefinition{}
	p, err := getAppArmorTemplatedPolicy(m, appid, template, caps, overrides)
	c.Check(err, IsNil)
	c.Check(p, Equals, expectedGeneratedAaProfile)
}

var mockSeccompTemplate = []byte(`
# Description: Allows access to app-specific directories and basic runtime
# Usage: common
#

# Dangerous syscalls that we don't ever want to allow

# kexec
deny kexec_load

# fine
alarm
`)

var expectedGeneratedSeccompProfile = `
# Description: Allows access to app-specific directories and basic runtime
# Usage: common
#

# Dangerous syscalls that we don't ever want to allow

# kexec
# EXPLICITLY DENIED: kexec_load

# fine
alarm

#cap1
capino
`

func (a *SecurityTestSuite) TestSecurityGenSeccompTemplatedPolicy(c *C) {
	makeMockSeccompTemplate(c, "mock-template", mockSeccompTemplate)
	makeMockSeccompCap(c, "cap1", []byte("#cap1\ncapino\n"))

	m := &packageYaml{
		Name:    "foo",
		Version: "1.0",
	}
	appid := &securityAppID{
		Pkgname: "foo",
		Version: "1.0",
	}
	template := "mock-template"
	caps := []string{"cap1"}
	overrides := &SecuritySeccompOverrideDefinition{}
	p, err := getSeccompTemplatedPolicy(m, appid, template, caps, overrides)
	c.Check(err, IsNil)
	c.Check(p, Equals, expectedGeneratedSeccompProfile)
}

var aaCustomPolicy = `
# Description: Allows unrestricted access to the system
# Usage: reserved

# vim:syntax=apparmor

#include <tunables/global>

# Define vars with unconfined since autopilot rules may reference them
###VAR###

# v2 compatible wildly permissive profile
###PROFILEATTACH### (attach_disconnected) {
  capability,
}
`
var expectedAaCustomPolicy = `
# Description: Allows unrestricted access to the system
# Usage: reserved

# vim:syntax=apparmor

#include <tunables/global>

# Define vars with unconfined since autopilot rules may reference them
# Specified profile variables
@{APP_APPNAME}=""
@{APP_ID_DBUS}="foo_5fbar_5f1_2e0"
@{APP_PKGNAME_DBUS}="foo"
@{APP_PKGNAME}="foo"
@{APP_VERSION}="1.0"
@{INSTALL_DIR}="{/apps,/oem}"
# Deprecated:
@{CLICK_DIR}="{/apps,/oem}"

# v2 compatible wildly permissive profile
profile "foo_bar_1.0" (attach_disconnected) {
  capability,
}
`

func (a *SecurityTestSuite) TestSecurityGetApparmorCustomPolicy(c *C) {
	m := &packageYaml{
		Name:    "foo",
		Version: "1.0",
	}
	appid := &securityAppID{
		AppID:   "foo_bar_1.0",
		Pkgname: "foo",
		Version: "1.0",
	}
	customPolicy := filepath.Join(c.MkDir(), "foo")
	err := ioutil.WriteFile(customPolicy, []byte(aaCustomPolicy), 0644)
	c.Assert(err, IsNil)

	p, err := getAppArmorCustomPolicy(m, appid, customPolicy)
	c.Check(err, IsNil)
	c.Check(p, Equals, expectedAaCustomPolicy)
}
