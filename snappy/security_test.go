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
	"regexp"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/logger"
	"github.com/ubuntu-core/snappy/osutil"
)

type SecurityTestSuite struct {
	tempDir  string
	buildDir string
	m        *snapYaml

	loadAppArmorPolicyCalled bool
}

var _ = Suite(&SecurityTestSuite{})

func (a *SecurityTestSuite) SetUpTest(c *C) {
	c.Skip("old security")
	a.buildDir = c.MkDir()
	a.tempDir = c.MkDir()
	os.MkdirAll(filepath.Join(a.buildDir, "meta"), 0755)

	// set global sandbox
	dirs.SetRootDir(c.MkDir())

	a.m = &snapYaml{
		Name:    "foo",
		Version: "1.0",
	}

	// and mock some stuff
	a.loadAppArmorPolicyCalled = false
	loadAppArmorPolicy = func(fn string) ([]byte, error) {
		a.loadAppArmorPolicyCalled = true
		return nil, nil
	}
	runUdevAdm = func(args ...string) error {
		return nil
	}
}

func (a *SecurityTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func ensureFileContentMatches(c *C, fn, expectedContent string) {
	content, err := ioutil.ReadFile(fn)
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, expectedContent)
}

func makeMockSecurityEnv(c *C) {
	makeMockApparmorTemplate(c, "default", []byte(""))
	makeMockSeccompTemplate(c, "default", []byte(""))
	makeMockApparmorCap(c, "network-client", []byte(``))
	makeMockSeccompCap(c, "network-client", []byte(``))
}

func makeMockApparmorTemplate(c *C, templateName string, content []byte) {
	mockTemplate := filepath.Join(securityPolicyTypeAppArmor.policyDir(), "templates", defaultPolicyVendor(), defaultPolicyVersion(), templateName)
	err := os.MkdirAll(filepath.Dir(mockTemplate), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockTemplate, content, 0644)
	c.Assert(err, IsNil)
}

func makeMockApparmorCap(c *C, capname string, content []byte) {
	mockPG := filepath.Join(securityPolicyTypeAppArmor.policyDir(), "policygroups", defaultPolicyVendor(), defaultPolicyVersion(), capname)
	err := os.MkdirAll(filepath.Dir(mockPG), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(mockPG, []byte(content), 0644)
	c.Assert(err, IsNil)
}

func makeMockSeccompTemplate(c *C, templateName string, content []byte) {
	mockTemplate := filepath.Join(securityPolicyTypeSeccomp.policyDir(), "templates", defaultPolicyVendor(), defaultPolicyVersion(), templateName)
	err := os.MkdirAll(filepath.Dir(mockTemplate), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(mockTemplate, content, 0644)
	c.Assert(err, IsNil)
}

func makeMockSeccompCap(c *C, capname string, content []byte) {
	mockPG := filepath.Join(securityPolicyTypeSeccomp.policyDir(), "policygroups", defaultPolicyVendor(), defaultPolicyVersion(), capname)
	err := os.MkdirAll(filepath.Dir(mockPG), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(mockPG, []byte(content), 0644)
	c.Assert(err, IsNil)
}

func (a *SecurityTestSuite) TestSnappyGetSecurityProfile(c *C) {
	m := snapYaml{
		Name:    "foo",
		Version: "1.0",
	}
	b := AppYaml{Name: "bin/app"}
	ap := getSecurityProfile(&m, b.Name, "/snap/foo/1.0/")
	c.Check(ap, Equals, "foo_bin-app_1.0")
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

	t = `not there`
	c.Assert(findWhitespacePrefix(t, "###POLICYGROUPS###"), Equals, "")
}

func (a *SecurityTestSuite) TestSecurityFindWhitespacePrefixNeedsQuoting(c *C) {
	s := `I need quoting: [`
	t := ``
	c.Assert(findWhitespacePrefix(s, t), Equals, t)
}

func (a *SecurityTestSuite) TestSecurityFindTemplateApparmor(c *C) {
	makeMockApparmorTemplate(c, "mock-template", []byte(`something`))

	t, err := securityPolicyTypeAppArmor.findTemplate("mock-template")
	c.Assert(err, IsNil)
	c.Assert(t, Matches, "something")
}

func (a *SecurityTestSuite) TestSecurityFindTemplateApparmorNotFound(c *C) {
	_, err := securityPolicyTypeAppArmor.findTemplate("not-available-templ")
	c.Assert(err, DeepEquals, &errPolicyNotFound{"template", &securityPolicyTypeAppArmor, "not-available-templ"})
}

func (a *SecurityTestSuite) TestSecurityFindCaps(c *C) {
	for _, f := range []string{"cap1", "cap2"} {
		makeMockApparmorCap(c, f, []byte(f))
	}

	cap, err := securityPolicyTypeAppArmor.findCaps([]string{"cap1", "cap2"}, "mock-template")
	c.Assert(err, IsNil)
	c.Assert(cap, DeepEquals, []string{"cap1", "cap2"})
}

func (a *SecurityTestSuite) TestSecurityFindCapsMultipleErrorHandling(c *C) {
	makeMockApparmorCap(c, "existing-cap", []byte("something"))

	_, err := securityPolicyTypeAppArmor.findCaps([]string{"existing-cap", "not-existing-cap"}, "mock-template")
	c.Check(err, ErrorMatches, "could not find specified cap: not-existing-cap.*")

	_, err = securityPolicyTypeAppArmor.findCaps([]string{"not-existing-cap", "existing-cap"}, "mock-template")
	c.Check(err, ErrorMatches, "could not find specified cap: not-existing-cap.*")

	_, err = securityPolicyTypeAppArmor.findCaps([]string{"existing-cap"}, "mock-template")
	c.Check(err, IsNil)
}

func (a *SecurityTestSuite) TestSecurityGetAppArmorVars(c *C) {
	appID := &securityAppID{
		AppName:  "foo",
		Version:  "1.0",
		AppID:    "id",
		SnapName: "pkgname",
	}
	c.Assert(appID.appArmorVars(), Equals, `
# Specified profile variables
@{APP_APPNAME}="foo"
@{APP_ID_DBUS}="id"
@{APP_PKGNAME_DBUS}="pkgname"
@{APP_PKGNAME}="pkgname"
@{APP_VERSION}="1.0"
@{INSTALL_DIR}="/snap"
# Deprecated:
@{CLICK_DIR}="/snap"`)
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
@{INSTALL_DIR}="/snap"
# Deprecated:
@{CLICK_DIR}="/snap"

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

	m := &snapYaml{
		Name:    "foo",
		Version: "1.0",
	}
	appid := &securityAppID{
		SnapName: "foo",
		Version:  "1.0",
	}
	template := "mock-template"
	caps := []string{"cap1"}
	overrides := &SecurityOverrideDefinition{}
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
capino`

func (a *SecurityTestSuite) TestSecurityGenSeccompTemplatedPolicy(c *C) {
	makeMockSeccompTemplate(c, "mock-template", mockSeccompTemplate)
	makeMockSeccompCap(c, "cap1", []byte("#cap1\ncapino\n"))

	m := &snapYaml{
		Name:    "foo",
		Version: "1.0",
	}
	appid := &securityAppID{
		SnapName: "foo",
		Version:  "1.0",
	}
	template := "mock-template"
	caps := []string{"cap1"}
	overrides := &SecurityOverrideDefinition{}
	p, err := getSeccompTemplatedPolicy(m, appid, template, caps, overrides)
	c.Check(err, IsNil)
	c.Check(p, Equals, expectedGeneratedSeccompProfile)
}

var aaCustomPolicy = `
# Description: Some custom aa policy
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
# Description: Some custom aa policy
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
@{INSTALL_DIR}="/snap"
# Deprecated:
@{CLICK_DIR}="/snap"

# v2 compatible wildly permissive profile
profile "foo_bar_1.0" (attach_disconnected) {
  capability,

# No read paths specified
# No write paths specified
# No abstractions specified
}
`

func (a *SecurityTestSuite) TestSecurityGetApparmorCustomPolicy(c *C) {
	m := &snapYaml{
		Name:    "foo",
		Version: "1.0",
	}
	appid := &securityAppID{
		AppID:    "foo_bar_1.0",
		SnapName: "foo",
		Version:  "1.0",
	}
	customPolicy := filepath.Join(c.MkDir(), "foo")
	err := ioutil.WriteFile(customPolicy, []byte(aaCustomPolicy), 0644)
	c.Assert(err, IsNil)

	p, err := getAppArmorCustomPolicy(m, appid, customPolicy, nil)
	c.Check(err, IsNil)
	c.Check(p, Equals, expectedAaCustomPolicy)
}

func (a *SecurityTestSuite) TestSecurityGetSeccompCustomPolicy(c *C) {
	// yes, getSeccompCustomPolicy does not care for snapYaml or appid
	m := &snapYaml{}
	appid := &securityAppID{}

	customPolicy := filepath.Join(c.MkDir(), "foo")
	err := ioutil.WriteFile(customPolicy, []byte(`canary`), 0644)
	c.Assert(err, IsNil)

	p, err := getSeccompCustomPolicy(m, appid, customPolicy)
	c.Check(err, IsNil)
	c.Check(p, Equals, `canary`)
}

func (a *SecurityTestSuite) TestSecurityGetAppID(c *C) {
	id, err := newAppID("pkg_app_1.0")
	c.Assert(err, IsNil)
	c.Assert(id, DeepEquals, &securityAppID{
		AppID:    "pkg_app_1.0",
		SnapName: "pkg",
		AppName:  "app",
		Version:  "1.0",
	})
}

func (a *SecurityTestSuite) TestSecurityGetAppIDInvalid(c *C) {
	_, err := newAppID("invalid")
	c.Assert(err, Equals, errInvalidAppID)
}

func (a *SecurityTestSuite) TestSecurityMergeApparmorSecurityOverridesNilDoesNotCrash(c *C) {
	sd := &SecurityDefinitions{}
	sd.mergeAppArmorSecurityOverrides(nil)
	c.Assert(sd, DeepEquals, &SecurityDefinitions{})
}

func (a *SecurityTestSuite) TestSecurityMergeApparmorSecurityOverridesTrivial(c *C) {
	sd := &SecurityDefinitions{}
	hwaccessOverrides := &SecurityOverrideDefinition{}
	sd.mergeAppArmorSecurityOverrides(hwaccessOverrides)

	c.Assert(sd, DeepEquals, &SecurityDefinitions{
		SecurityOverride: hwaccessOverrides,
	})
}

func (a *SecurityTestSuite) TestSecurityMergeApparmorSecurityOverridesOverrides(c *C) {
	sd := &SecurityDefinitions{}
	hwaccessOverrides := &SecurityOverrideDefinition{
		ReadPaths:  []string{"read1"},
		WritePaths: []string{"write1"},
	}
	sd.mergeAppArmorSecurityOverrides(hwaccessOverrides)

	c.Assert(sd, DeepEquals, &SecurityDefinitions{
		SecurityOverride: hwaccessOverrides,
	})
}

func (a *SecurityTestSuite) TestSecurityMergeApparmorSecurityOverridesMerges(c *C) {
	sd := &SecurityDefinitions{
		SecurityOverride: &SecurityOverrideDefinition{
			ReadPaths: []string{"orig1"},
		},
	}
	hwaccessOverrides := &SecurityOverrideDefinition{
		ReadPaths:  []string{"read1"},
		WritePaths: []string{"write1"},
	}
	sd.mergeAppArmorSecurityOverrides(hwaccessOverrides)

	c.Assert(sd, DeepEquals, &SecurityDefinitions{
		SecurityOverride: &SecurityOverrideDefinition{
			ReadPaths:  []string{"orig1", "read1"},
			WritePaths: []string{"write1"},
		},
	})
}

func (a *SecurityTestSuite) TestSecurityGeneratePolicyForServiceBinaryEmpty(c *C) {
	makeMockApparmorTemplate(c, "default", []byte(`# apparmor
###POLICYGROUPS###
`))
	makeMockApparmorCap(c, "network-client", []byte(`
aa-network-client`))
	makeMockSeccompTemplate(c, "default", []byte(`write`))
	makeMockSeccompCap(c, "network-client", []byte(`
sc-network-client
`))

	// empty SecurityDefinition means "network-client" cap
	sd := &SecurityDefinitions{}
	m := &snapYaml{
		Name:    "pkg",
		Version: "1.0",
	}

	// generate the apparmor profile
	err := sd.generatePolicyForServiceBinary(m, "binary", "/snap/app/1.0")
	c.Assert(err, IsNil)

	// ensure the apparmor policy got loaded
	c.Assert(a.loadAppArmorPolicyCalled, Equals, true)

	aaProfile := filepath.Join(dirs.SnapAppArmorDir, "pkg_binary_1.0")
	ensureFileContentMatches(c, aaProfile, `# apparmor
# Rules specified via caps (policy groups)

aa-network-client
`)
	scProfile := filepath.Join(dirs.SnapSeccompDir, "pkg_binary_1.0")
	ensureFileContentMatches(c, scProfile, `write

sc-network-client`)

}

var mockSecuritySnapYaml = `
name: hello-world
vendor: someone
version: 1.0
apps:
 binary1:
   plugs: [binary1]
 service1:
   plugs: [service1]
   daemon: forking
plugs:
 binary1:
  interface: old-security
  caps: []
 service1:
  interface: old-security
  caps: []
`

func (a *SecurityTestSuite) TestSecurityGeneratePolicyFromFileSimple(c *C) {
	// we need to create some fake data
	makeMockApparmorTemplate(c, "default", []byte(`# some header
###POLICYGROUPS###
`))
	makeMockSeccompTemplate(c, "default", []byte(`
deny kexec
read
write
`))

	mockSnapYamlFn, err := makeInstalledMockSnap(mockSecuritySnapYaml, 11)
	c.Assert(err, IsNil)

	// the acutal thing that gets tested
	err = GeneratePolicyFromFile(mockSnapYamlFn, false)
	c.Assert(err, IsNil)

	// ensure the apparmor policy got loaded
	c.Assert(a.loadAppArmorPolicyCalled, Equals, true)

	// apparmor
	generatedProfileFn := filepath.Join(dirs.SnapAppArmorDir, "hello-world_binary1_1.0")
	ensureFileContentMatches(c, generatedProfileFn, `# some header
# No caps (policy groups) specified
`)
	// ... and seccomp
	generatedProfileFn = filepath.Join(dirs.SnapSeccompDir, "hello-world_binary1_1.0")
	ensureFileContentMatches(c, generatedProfileFn, `
# EXPLICITLY DENIED: kexec
read
write

`)
}

func (a *SecurityTestSuite) TestSecurityGeneratePolicyFileForConfig(c *C) {
	// we need to create some fake data
	makeMockApparmorTemplate(c, "default", []byte(`# some header
###POLICYGROUPS###
`))
	makeMockSeccompTemplate(c, "default", []byte(`
deny kexec
read
write
`))

	mockSnapYamlFn, err := makeInstalledMockSnap(mockSecuritySnapYaml, 11)
	c.Assert(err, IsNil)
	configHook := filepath.Join(filepath.Dir(mockSnapYamlFn), "hooks", "config")
	os.MkdirAll(filepath.Dir(configHook), 0755)
	err = ioutil.WriteFile(configHook, []byte("true"), 0755)
	c.Assert(err, IsNil)

	// generate config
	err = GeneratePolicyFromFile(mockSnapYamlFn, false)
	c.Assert(err, IsNil)

	// and for snappy-config
	generatedProfileFn := filepath.Join(dirs.SnapAppArmorDir, "hello-world_snappy-config_1.0")
	ensureFileContentMatches(c, generatedProfileFn, `# some header
# No caps (policy groups) specified
`)

}

func (a *SecurityTestSuite) TestSecurityCompareGeneratePolicyFromFileSimple(c *C) {
	// we need to create some fake data
	makeMockApparmorTemplate(c, "default", []byte(`# some header
###POLICYGROUPS###
`))
	makeMockSeccompTemplate(c, "default", []byte(`
deny kexec
read
write
`))
	mockSnapYamlFn, err := makeInstalledMockSnap(mockSecuritySnapYaml, 11)
	c.Assert(err, IsNil)

	err = GeneratePolicyFromFile(mockSnapYamlFn, false)
	c.Assert(err, IsNil)

	// nothing changed, compare is happy
	err = CompareGeneratePolicyFromFile(mockSnapYamlFn)
	c.Assert(err, IsNil)

	// now change the templates
	makeMockApparmorTemplate(c, "default", []byte(`# some different header
###POLICYGROUPS###
`))
	// ...and ensure that the difference is found
	err = CompareGeneratePolicyFromFile(mockSnapYamlFn)
	c.Assert(err, ErrorMatches, "policy differs.*")
}

func (a *SecurityTestSuite) TestSecurityGeneratePolicyFromFileHwAccess(c *C) {
	// we need to create some fake data
	makeMockApparmorTemplate(c, "default", []byte(`# some header
###POLICYGROUPS###
###READS###
###WRITES###
`))
	makeMockSeccompTemplate(c, "default", []byte(`
deny kexec
read
write
`))
	mockSnapYamlFn, err := makeInstalledMockSnap(mockSecuritySnapYaml, 11)
	c.Assert(err, IsNil)
	err = GeneratePolicyFromFile(mockSnapYamlFn, false)
	c.Assert(err, IsNil)

	// ensure that AddHWAccess does the right thing
	a.loadAppArmorPolicyCalled = false
	err = AddHWAccess("hello-world", "/dev/kmesg")
	c.Assert(err, IsNil)

	// ensure the apparmor policy got loaded
	c.Check(a.loadAppArmorPolicyCalled, Equals, true)

	// apparmor got updated with the new read path
	generatedProfileFn := filepath.Join(dirs.SnapAppArmorDir, "hello-world_binary1_1.0")
	ensureFileContentMatches(c, generatedProfileFn, `# some header
# No caps (policy groups) specified
# Additional read-paths from security-override
/run/udev/data/ rk,
/run/udev/data/* rk,

# Additional write-paths from security-override
/dev/kmesg rwk,

`)
}

func (a *SecurityTestSuite) TestSecurityRegenerateAll(c *C) {
	// we need to create some fake data
	makeMockApparmorTemplate(c, "default", []byte(`# some header
###POLICYGROUPS###
`))
	makeMockSeccompTemplate(c, "default", []byte(`
deny kexec
read
write
`))
	mockSnapYamlFn, err := makeInstalledMockSnap(mockSecuritySnapYaml, 11)
	c.Assert(err, IsNil)

	err = GeneratePolicyFromFile(mockSnapYamlFn, false)
	c.Assert(err, IsNil)

	// now change the templates
	makeMockApparmorTemplate(c, "default", []byte(`# some different header
###POLICYGROUPS###
`))
	// ...and regenerate the templates
	err = RegenerateAllPolicy(false)
	c.Assert(err, IsNil)

	// ensure apparmor got updated with the new read path
	generatedProfileFn := filepath.Join(dirs.SnapAppArmorDir, "hello-world_binary1_1.0")
	ensureFileContentMatches(c, generatedProfileFn, `# some different header
# No caps (policy groups) specified
`)

}

func makeCustomAppArmorPolicy(c *C) string {
	content := []byte(`# custom apparmor policy
###VAR###

###PROFILEATTACH### (attach_disconnected) {
 stuff

}
`)
	fn := filepath.Join(c.MkDir(), "custom-aa-policy")
	err := ioutil.WriteFile(fn, content, 0644)
	c.Assert(err, IsNil)

	return fn
}

func (a *SecurityTestSuite) TestSecurityGenerateCustomPolicyAdditionalIsConsidered(c *C) {
	m := &snapYaml{
		Name:    "foo",
		Version: "1.0",
	}
	appid := &securityAppID{
		SnapName: "foo",
		Version:  "1.0",
	}
	fn := makeCustomAppArmorPolicy(c)

	content, err := getAppArmorCustomPolicy(m, appid, fn, nil)
	c.Assert(err, IsNil)
	c.Assert(content, Matches, `(?ms).*^# No read paths specified$`)
	c.Assert(content, Matches, `(?ms).*^# No write paths specified$`)
	c.Assert(content, Matches, `(?ms).*^# No abstractions specified$`)
}

var mockSecurityDeprecatedSnapYaml = `
name: hello-world
vendor: someone
version: 1.0
apps:
 binary1:
   plugs: [binary1]
plugs:
 binary1:
   interface: old-security
   caps: []
`

var mockSecurityDeprecatedSnapYamlApparmor1 = `
   security-override:
    apparmor:
     read-path: [foo]
`
var mockSecurityDeprecatedSnapYamlApparmor2 = `
   security-override:
    apparmor: {}
`
var mockSecurityDeprecatedSnapYamlSeccomp1 = `
   security-override:
    seccomp: {}
`

var mockSecurityDeprecatedSnapYamlSeccomp2 = `
   security-override:
    seccomp:
     syscalls: [1]
`

type mockLogger struct {
	notice []string
	debug  []string
}

func (l *mockLogger) Notice(msg string) {
	l.notice = append(l.notice, msg)
}

func (l *mockLogger) Debug(msg string) {
	l.debug = append(l.debug, msg)
}

func (a *SecurityTestSuite) TestSecurityWarnsNot(c *C) {
	makeMockApparmorTemplate(c, "default", []byte(``))
	makeMockSeccompTemplate(c, "default", []byte(``))

	ml := &mockLogger{}
	logger.SetLogger(ml)

	mockSnapYamlFn, err := makeInstalledMockSnap(mockSecurityDeprecatedSnapYaml, 11)
	c.Assert(err, IsNil)

	err = GeneratePolicyFromFile(mockSnapYamlFn, false)
	c.Assert(err, IsNil)

	c.Assert(ml.notice, DeepEquals, []string(nil))
}

func (a *SecurityTestSuite) TestSecurityWarnsOnDeprecatedApparmor(c *C) {
	makeMockApparmorTemplate(c, "default", []byte(``))
	makeMockSeccompTemplate(c, "default", []byte(``))

	for _, s := range []string{mockSecurityDeprecatedSnapYamlApparmor1, mockSecurityDeprecatedSnapYamlApparmor2} {

		ml := &mockLogger{}
		logger.SetLogger(ml)

		mockSnapYamlFn, err := makeInstalledMockSnap(mockSecurityDeprecatedSnapYaml+s, 11)
		c.Assert(err, IsNil)

		err = GeneratePolicyFromFile(mockSnapYamlFn, false)
		c.Assert(err, IsNil)

		c.Assert(ml.notice, DeepEquals, []string{"The security-override.apparmor key is no longer supported, please use use security-override directly"})
	}
}

func (a *SecurityTestSuite) TestSecurityWarnsOnDeprecatedSeccomp(c *C) {
	makeMockApparmorTemplate(c, "default", []byte(``))
	makeMockSeccompTemplate(c, "default", []byte(``))

	for _, s := range []string{mockSecurityDeprecatedSnapYamlSeccomp1, mockSecurityDeprecatedSnapYamlSeccomp2} {

		ml := &mockLogger{}
		logger.SetLogger(ml)

		mockSnapYamlFn, err := makeInstalledMockSnap(mockSecurityDeprecatedSnapYaml+s, 11)
		c.Assert(err, IsNil)

		err = GeneratePolicyFromFile(mockSnapYamlFn, false)
		c.Assert(err, IsNil)

		c.Assert(ml.notice, DeepEquals, []string{"The security-override.seccomp key is no longer supported, please use use security-override directly"})
	}
}

func makeInstalledMockSnapSideloaded(c *C) string {
	mockSnapYamlFn, err := makeInstalledMockSnap(mockSecuritySnapYaml, 11)
	c.Assert(err, IsNil)
	// pretend its sideloaded
	basePath := regexp.MustCompile(`(.*)/hello-world`).FindString(mockSnapYamlFn)
	oldPath := filepath.Join(basePath, "1.0")
	newPath := filepath.Join(basePath, "IsSideloadVer")
	err = os.Rename(oldPath, newPath)
	mockSnapYamlFn = filepath.Join(basePath, "IsSideloadVer", "meta", "snap.yaml")

	return mockSnapYamlFn
}

func (a *SecurityTestSuite) TestSecurityGeneratePolicyFromFileSideload(c *C) {
	// we need to create some fake data
	makeMockApparmorTemplate(c, "default", []byte(``))
	makeMockSeccompTemplate(c, "default", []byte(``))

	mockSnapYamlFn := makeInstalledMockSnapSideloaded(c)

	// the acutal thing that gets tested
	err := GeneratePolicyFromFile(mockSnapYamlFn, false)
	c.Assert(err, IsNil)

	// ensure the apparmor policy got loaded
	c.Assert(a.loadAppArmorPolicyCalled, Equals, true)

	// apparmor
	generatedProfileFn := filepath.Join(dirs.SnapAppArmorDir, "hello-world_binary1_IsSideloadVer")
	c.Assert(osutil.FileExists(generatedProfileFn), Equals, true)

	// ... and seccomp
	generatedProfileFn = filepath.Join(dirs.SnapSeccompDir, "hello-world_binary1_IsSideloadVer")
	c.Assert(osutil.FileExists(generatedProfileFn), Equals, true)
}

func (a *SecurityTestSuite) TestSecurityCompareGeneratePolicyFromFileSideload(c *C) {
	// we need to create some fake data
	makeMockApparmorTemplate(c, "default", []byte(``))
	makeMockSeccompTemplate(c, "default", []byte(``))

	mockSnapYamlFn := makeInstalledMockSnapSideloaded(c)
	// generate policy
	err := GeneratePolicyFromFile(mockSnapYamlFn, false)
	c.Assert(err, IsNil)

	// nothing changed, ensure compare is happy even for sideloaded pkgs
	err = CompareGeneratePolicyFromFile(mockSnapYamlFn)
	c.Assert(err, IsNil)
}

func (a *SecurityTestSuite) TestParseSnapYamlWithVersion(c *C) {
	testVersion := "1.0"
	dir := filepath.Join(a.tempDir, "foo", testVersion, "meta")
	os.MkdirAll(dir, 0755)
	y := filepath.Join(dir, "snap.yaml")
	ioutil.WriteFile(y, []byte(`
name: foo
version: 123456789
`), 0644)
	m, err := parseSnapYamlFileWithVersion(y)
	c.Assert(err, IsNil)
	c.Assert(m.Version, Equals, testVersion)
}

func (a *SecurityTestSuite) TestParseSnapYamlWithVersionSymlink(c *C) {
	testVersion := "1.0"
	verDir := filepath.Join(a.tempDir, "foo", testVersion)
	symDir := filepath.Join(a.tempDir, "foo", "current")
	os.MkdirAll(filepath.Join(verDir, "meta"), 0755)
	os.Symlink(verDir, symDir)
	y := filepath.Join(symDir, "meta", "snap.yaml")
	ioutil.WriteFile(y, []byte(`
name: foo
version: 123456789
`), 0644)
	m, err := parseSnapYamlFileWithVersion(y)
	c.Assert(err, IsNil)
	c.Assert(m.Version, Equals, testVersion)

}

func (a *SecurityTestSuite) TestFindPlugForAppEmpty(c *C) {
	app := &AppYaml{}
	m := &snapYaml{}
	plug, err := findPlugForApp(m, app)
	c.Check(err, IsNil)
	c.Check(plug, IsNil)
}

func (a *SecurityTestSuite) TestFindPluglForAppTooMany(c *C) {
	app := &AppYaml{
		PlugsRef: []string{"one", "two"},
	}
	m := &snapYaml{}
	plug, err := findPlugForApp(m, app)
	c.Check(plug, IsNil)
	c.Check(err, ErrorMatches, "only a single plug is supported, 2 found")
}

func (a *SecurityTestSuite) TestFindPlugForAppNotFound(c *C) {
	app := &AppYaml{
		PlugsRef: []string{"not-there"},
	}
	m := &snapYaml{}
	plug, err := findPlugForApp(m, app)
	c.Check(plug, IsNil)
	c.Check(err, ErrorMatches, `can not find plug "not-there"`)
}

func (a *SecurityTestSuite) TestFindPlugFinds(c *C) {
	app := &AppYaml{
		PlugsRef: []string{"plug"},
	}
	m := &snapYaml{
		Plugs: map[string]*plugYaml{
			"plug": &plugYaml{Interface: "some-type"},
		},
	}
	plug, err := findPlugForApp(m, app)
	c.Check(err, IsNil)
	c.Check(plug.Interface, Equals, "some-type")
}
