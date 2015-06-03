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

	. "launchpad.net/gocheck"

	"launchpad.net/snappy/pkg"
)

type SecurityTestSuite struct {
	buildDir string
	m        *packageYaml
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
}

func (a *SecurityTestSuite) verifyApparmorFile(c *C, expected string) {

	// ensure the integraton hook is setup correctly for click-apparmor
	c.Assert(a.m.Integration["app"]["apparmor"], Equals, "meta/app.apparmor")

	apparmorJSONFile := a.m.Integration["app"]["apparmor"]
	content, err := ioutil.ReadFile(filepath.Join(a.buildDir, apparmorJSONFile))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, expected)
}

// no special security settings generate the default
func (a *SecurityTestSuite) TestSnappyHandleApparmorSecurityDefault(c *C) {
	sec := &SecurityDefinitions{}

	a.m.Binaries = append(a.m.Binaries, Binary{Name: "app", SecurityDefinitions: *sec})
	a.m.integrate()

	err := handleApparmor(a.buildDir, a.m, "app", sec)
	c.Assert(err, IsNil)

	// verify file content
	a.verifyApparmorFile(c, `{
  "template": "default",
  "policy_groups": [
    "networking"
  ],
  "policy_vendor": "ubuntu-core",
  "policy_version": 15.04
}`)
}

func (a *SecurityTestSuite) TestSnappyHandleApparmorCaps(c *C) {
	sec := &SecurityDefinitions{
		SecurityCaps: []string{"cap1", "cap2"},
	}

	a.m.Binaries = append(a.m.Binaries, Binary{Name: "app", SecurityDefinitions: *sec})
	a.m.integrate()

	err := handleApparmor(a.buildDir, a.m, "app", sec)
	c.Assert(err, IsNil)

	// verify file content
	a.verifyApparmorFile(c, `{
  "template": "default",
  "policy_groups": [
    "cap1",
    "cap2"
  ],
  "policy_vendor": "ubuntu-core",
  "policy_version": 15.04
}`)
}

func (a *SecurityTestSuite) TestSnappyHandleApparmorTemplate(c *C) {
	sec := &SecurityDefinitions{
		SecurityTemplate: "docker-client",
	}

	a.m.Binaries = append(a.m.Binaries, Binary{Name: "app", SecurityDefinitions: *sec})
	a.m.integrate()

	err := handleApparmor(a.buildDir, a.m, "app", sec)
	c.Assert(err, IsNil)

	// verify file content
	a.verifyApparmorFile(c, `{
  "template": "docker-client",
  "policy_groups": [],
  "policy_vendor": "ubuntu-core",
  "policy_version": 15.04
}`)
}

func (a *SecurityTestSuite) TestSnappyHandleApparmorOverride(c *C) {
	sec := &SecurityDefinitions{
		SecurityOverride: &SecurityOverrideDefinition{
			Apparmor: "meta/custom.json",
		},
	}

	a.m.Binaries = append(a.m.Binaries, Binary{Name: "app", SecurityDefinitions: *sec})
	a.m.integrate()

	err := handleApparmor(a.buildDir, a.m, "app", sec)
	c.Assert(err, IsNil)

	c.Assert(a.m.Integration["app"]["apparmor"], Equals, "meta/custom.json")
}

func (a *SecurityTestSuite) TestSnappyHandleApparmorPolicy(c *C) {
	sec := &SecurityDefinitions{
		SecurityPolicy: &SecurityPolicyDefinition{
			Apparmor: "meta/custom-policy.apparmor",
		},
	}

	a.m.Binaries = append(a.m.Binaries, Binary{Name: "app", SecurityDefinitions: *sec})
	a.m.integrate()

	err := handleApparmor(a.buildDir, a.m, "app", sec)
	c.Assert(err, IsNil)

	c.Assert(a.m.Integration["app"]["apparmor-profile"], Equals, "meta/custom-policy.apparmor")
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
