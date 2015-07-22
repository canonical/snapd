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

package config

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	check "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type ConfigSuite struct{}

var _ = check.Suite(&ConfigSuite{})

func testConfigContents(fileName string) string {
	return `{` +
		fmt.Sprintf(`"FileName":"%s",`, fileName) +
		`"Release":"testrelease",` +
		`"Channel":"testchannel",` +
		`"TargetRelease":"testtargetrelease",` +
		`"TargetChannel":"testtargetchannel",` +
		`"Update":true,` +
		`"Rollback":true` +
		`}`
}

func (s *ConfigSuite) TestWriteConfig(c *check.C) {
	tmpDir, err := ioutil.TempDir("", "")
	c.Assert(err, check.IsNil, check.Commentf(
		"Error creating a temporary directory: %v", err))
	configFileName := filepath.Join(tmpDir, "test.config")

	cfg := NewConfig(
		configFileName,
		"testrelease", "testchannel", "testtargetrelease", "testtargetchannel",
		true, true)
	cfg.Write()

	expected := testConfigContents(configFileName)
	writtenConfig, err := ioutil.ReadFile(configFileName)
	c.Assert(err, check.IsNil, check.Commentf("Error reading config: %v", err))
	c.Assert(string(writtenConfig), check.Equals, expected)
}

func (s *ConfigSuite) TestReadConfig(c *check.C) {
	tmpDir, err := ioutil.TempDir("", "")
	c.Assert(err, check.IsNil, check.Commentf(
		"Error creating a temporary directory: %v", err))
	configFileName := filepath.Join(tmpDir, "test.config")

	configContents := testConfigContents(configFileName)
	ioutil.WriteFile(configFileName, []byte(configContents), 0644)

	expected := NewConfig(
		configFileName,
		"testrelease", "testchannel", "testtargetrelease", "testtargetchannel",
		true, true)
	cfg, err := ReadConfig(configFileName)

	c.Assert(err, check.IsNil, check.Commentf("Error reading config: %v", err))
	c.Assert(cfg, check.DeepEquals, expected)
}
