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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"
)

const configPassthroughScript = `#!/bin/sh

# temp location to store cfg
CFG="%s/config.out"

# just dump out for the tes
cat - > $CFG

# and config
cat $CFG
`

const configErrorScript = `#!/bin/sh

printf "error: some error"
exit 1
`

const mockAaExecScript = `#!/bin/sh
[ "$1" = "-d" ]
shift
[ -n "$1" ]
shift
exec "$@"
`

const configYaml = `
config:
  hello-world:
    key: value
`

func (s *SnapTestSuite) makeInstalledMockSnapWithConfig(c *C, configScript string, yamls ...string) (snapDir string, err error) {
	yamlFile, err := s.makeInstalledMockSnap(yamls...)
	c.Assert(err, IsNil)
	metaDir := filepath.Dir(yamlFile)
	err = os.Mkdir(filepath.Join(metaDir, "hooks"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(metaDir, "hooks", "config"), []byte(configScript), 0755)
	c.Assert(err, IsNil)

	snapDir = filepath.Dir(metaDir)
	return snapDir, nil
}

var mockOsSnap = `
name: ubuntu-core
version: 1.0
type: os
`

func (s *SnapTestSuite) TestConfigSimple(c *C) {
	mockConfig := fmt.Sprintf(configPassthroughScript, s.tempdir)
	snapDir, err := s.makeInstalledMockSnapWithConfig(c, mockConfig)
	c.Assert(err, IsNil)

	newConfig, err := snapConfig(snapDir, "sergiusens", []byte(configYaml))
	c.Assert(err, IsNil)
	content, err := ioutil.ReadFile(filepath.Join(s.tempdir, "config.out"))
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, []byte(configYaml))
	c.Assert(string(newConfig), Equals, configYaml)
}

func (s *SnapTestSuite) TestConfigOS(c *C) {
	snapYaml, err := s.makeInstalledMockSnap(mockOsSnap)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnapPart(snapYaml, testOrigin)
	c.Assert(err, IsNil)

	var cfg []byte
	inCfg := []byte(`something`)
	coreConfig = func(configuration []byte) ([]byte, error) {
		cfg = configuration
		return cfg, nil
	}
	defer func() { coreConfig = coreConfigImpl }()

	_, err = (&Overlord{}).Configure(snap, inCfg)
	c.Assert(err, IsNil)
	c.Assert(cfg, DeepEquals, inCfg)
}

func (s *SnapTestSuite) TestConfigGeneratesRightAA(c *C) {
	aas := []string{}
	runConfigScript = func(cs, aa string, rc []byte, env []string) ([]byte, error) {
		aas = append(aas, aa)
		return nil, nil
	}
	defer func() { runConfigScript = runConfigScriptImpl }()

	mockConfig := fmt.Sprintf(configPassthroughScript, s.tempdir)

	snapDir, err := s.makeInstalledMockSnapWithConfig(c, mockConfig, `name: fmk
type: framework
version: 42`)
	c.Assert(err, IsNil)
	_, err = snapConfig(snapDir, testOrigin, []byte(configYaml))
	c.Assert(err, IsNil)

	snapDir, err = s.makeInstalledMockSnapWithConfig(c, mockConfig, `name: potato
type: potato
version: 42`)
	c.Assert(err, IsNil)
	_, err = snapConfig(snapDir, testOrigin, []byte(configYaml))
	c.Assert(err, IsNil)

	c.Check(aas, DeepEquals, []string{
		"fmk_snappy-config_42",
		"potato." + testOrigin + "_snappy-config_42",
	})
}

func (s *SnapTestSuite) TestConfigError(c *C) {
	snapDir, err := s.makeInstalledMockSnapWithConfig(c, configErrorScript)
	c.Assert(err, IsNil)

	newConfig, err := snapConfig(snapDir, "sergiusens", []byte(configYaml))
	c.Assert(err, NotNil)
	c.Assert(newConfig, IsNil)
	c.Assert(err, ErrorMatches, ".*failed with: 'error: some error'.*")
}

func (s *SnapTestSuite) TestSnip2Yaml(c *C) {
	bs := snip2yaml("hello-world", []string{"foo", "bar"}, "42")
	c.Check(string(bs), Equals, "config: {hello-world: {foo: {bar: 42}}}")
}

func (s *SnapTestSuite) TestConfigSnippetGetIntegration(c *C) {
	ins := []string{}
	out := []byte{}
	var errOut error
	runConfigScript = func(_, _ string, rc []byte, _ []string) ([]byte, error) {
		ins = append(ins, string(rc))
		return out, errOut
	}
	defer func() { runConfigScript = runConfigScriptImpl }()

	mockConfig := fmt.Sprintf(configPassthroughScript, s.tempdir)

	snapDir, err := s.makeInstalledMockSnapWithConfig(c, mockConfig)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnapPart(filepath.Join(snapDir, "meta", "snap.yaml"), testOrigin)
	c.Assert(err, IsNil)

	ov := &Overlord{}

	out = []byte(`
config:
  hello-app:
    foo:
      bar:
        42
`)
	bs, err := ov.ConfigureFromSnippet(snap, []string{"foo", "bar"}, "")
	c.Assert(err, IsNil)
	c.Check(ins, DeepEquals, []string{""})
	c.Check(bs, Equals, "42")

	bs, err = ov.ConfigureFromSnippet(snap, []string{"foo", "bar"}, "8")
	c.Assert(err, IsNil)
	c.Check(ins, DeepEquals, []string{"", "config: {hello-app: {foo: {bar: 8}}}"})
	c.Check(bs, Equals, "42")

	errOut = fmt.Errorf("hi")
	_, err = ov.ConfigureFromSnippet(snap, []string{"foo", "bar"}, "42")
	c.Check(err, Equals, errOut)

	errOut = nil
	out = []byte(`hello`)
	_, err = ov.ConfigureFromSnippet(snap, []string{"foo", "bar"}, "42")
	c.Check(err, ErrorMatches, `configuration succeeded but can't unmarshal result: (?m).*`)

	out = []byte(`foo: {bar: 42}`)
	_, err = ov.ConfigureFromSnippet(snap, []string{"foo", "bar"}, "42")
	c.Check(err, ErrorMatches, `configuration succeeded but returned unexpected value: (?m).*`)

	out = []byte(`config: {hello-app: {foo: 42}}`)
	_, err = ov.ConfigureFromSnippet(snap, []string{"foo", "bar"}, "42")
	c.Check(err, ErrorMatches, `configuration succeeded but got unexpected value in which to look for "bar": 42`)

	out = []byte(`config: {hello-app: {foo: {a: b}}}`)
	_, err = ov.ConfigureFromSnippet(snap, []string{"foo", "bar"}, "42")
	c.Check(err, ErrorMatches, `configuration succeeded but result has no entry "bar"`)
}
