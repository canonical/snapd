// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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

package partition

import (
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/mvo5/uboot-go/uenv"
	"gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { check.TestingT(t) }

type bootloaderTestSuite struct {
	filepathGlobCalls        map[string]int
	backFilepathGlob         func(string) ([]string, error)
	filepathGlobFail         bool
	filepathGlobReturnValues []string
	backConfValue            func(string) (string, error)
	fakeConf                 map[string]string
}

var _ = check.Suite(&bootloaderTestSuite{})

func (s *bootloaderTestSuite) SetUpSuite(c *check.C) {
	s.backFilepathGlob = filepathGlob
	filepathGlob = s.fakeFilepathGlob
	s.backConfValue = confValue
	confValue = s.fakeConfValue
	s.fakeConf = map[string]string{}
}

func (s *bootloaderTestSuite) TearDownSuite(c *check.C) {
	filepathGlob = s.backFilepathGlob
	confValue = s.backConfValue
}

func (s *bootloaderTestSuite) SetUpTest(c *check.C) {
	s.filepathGlobCalls = make(map[string]int)
	s.filepathGlobFail = false
	s.filepathGlobReturnValues = nil
}

func (s *bootloaderTestSuite) fakeConfValue(key string) (string, error) {
	return s.fakeConf[key], nil
}

func (s *bootloaderTestSuite) fakeFilepathGlob(path string) (matches []string, err error) {
	if s.filepathGlobFail {
		err = fmt.Errorf("Error calling filepathGlob!!")
		return
	}
	s.filepathGlobCalls[path]++

	return s.filepathGlobReturnValues, nil
}

func (s *bootloaderTestSuite) TestBootDir(c *check.C) {
	c.Assert(BootDir("grub"), check.Equals, grubDir,
		check.Commentf("Expected BootDir of 'grub' to be "+grubDir))
	c.Assert(BootDir("uboot"), check.Equals, ubootDir,
		check.Commentf("Expected OtherPartition of 'uboot' to be "+ubootDir))
}

func (s *bootloaderTestSuite) TestBootSystemReturnsGlobError(c *check.C) {
	s.filepathGlobFail = true

	_, err := BootSystem()

	c.Assert(err, check.NotNil, check.Commentf("Expected error to be nil, %v", err))
}

func (s *bootloaderTestSuite) TestBootSystemCallsFilepathGlob(c *check.C) {
	BootSystem()

	p := bootBase + "/grub"
	calls := s.filepathGlobCalls[p]

	c.Assert(calls, check.Equals, 1,
		check.Commentf("Expected calls to filepath.Glob with path %s to be 1, %d found", p, calls))
}

func (s *bootloaderTestSuite) TestBootSystemForGrub(c *check.C) {
	s.filepathGlobReturnValues = []string{"a-grub-related-dir"}

	bootSystem, err := BootSystem()

	c.Assert(err, check.IsNil, check.Commentf("Unexpected error %v", err))
	c.Assert(bootSystem, check.Equals, "grub",
		check.Commentf("Expected grub boot system not found, %s", bootSystem))
}

func (s *bootloaderTestSuite) TestBootSystemForUBoot(c *check.C) {
	s.filepathGlobReturnValues = []string{}

	bootSystem, err := BootSystem()

	c.Assert(err, check.IsNil, check.Commentf("Unexpected error %v", err))
	c.Assert(bootSystem, check.Equals, "uboot",
		check.Commentf("Expected uboot boot system not found, %s", bootSystem))
}

func (s *bootloaderTestSuite) TestModeReturnsSnappyModeFromConf(c *check.C) {
	s.fakeConf = map[string]string{
		"snappy_mode": "test_mode",
	}

	mode, err := Mode()

	c.Assert(err, check.IsNil, check.Commentf("Unexpected error %v", err))
	c.Assert(mode, check.Equals, "test_mode", check.Commentf("Wrong mode"))
}

func (s *bootloaderTestSuite) TestOSSnapNameReturnsSnapFromConf(c *check.C) {
	s.fakeConf = map[string]string{
		"snappy_os": "test-os-snap.developer_version.snap",
	}

	osSnapName := OSSnapName(c)

	c.Assert(osSnapName, check.Equals, "test-os-snap",
		check.Commentf("Wrong os snap"))
}

func (s *bootloaderTestSuite) TestSnappyKernelReturnsSnapFromConf(c *check.C) {
	s.fakeConf = map[string]string{
		"snappy_kernel": "test-kernel-snap.developer_version.snap",
	}

	snappyOS, err := SnappyKernel()

	c.Assert(err, check.IsNil, check.Commentf("Unexpected error %v", err))
	c.Assert(snappyOS, check.Equals, "test-kernel-snap.developer_version.snap",
		check.Commentf("Wrong os snap"))
}

type confTestSuite struct {
	backBootSystem  func() (string, error)
	system          string
	backConfigFiles map[string]string
}

var _ = check.Suite(&confTestSuite{})

func (s *confTestSuite) SetUpSuite(c *check.C) {
	s.backBootSystem = BootSystem
	BootSystem = s.fakeBootSystem
	s.backConfigFiles = configFiles
}

func (s *confTestSuite) fakeBootSystem() (system string, err error) {
	return s.system, nil
}

func (s *confTestSuite) TearDownSuite(c *check.C) {
	BootSystem = s.backBootSystem
	configFiles = s.backConfigFiles
}

func createConfigFile(c *check.C, system string, contents map[string]string) (name string) {
	if system == "grub" {
		name = createGrubConfigFile(c, contents)
	} else if system == "uboot" {
		name = createUbootConfigFile(c, contents)
	}
	return
}

func createGrubConfigFile(c *check.C, contents map[string]string) (name string) {
	var contentsStr string
	for key, value := range contents {
		contentsStr += fmt.Sprintf("%s=%s\n", key, value)
	}
	file, err := ioutil.TempFile("", "snappy-grub-test")
	c.Assert(err, check.IsNil, check.Commentf("Error creating temp file: %s", err))
	err = ioutil.WriteFile(file.Name(), []byte(contentsStr), 0644)
	c.Assert(err, check.IsNil, check.Commentf("Error writing test bootloader file: %s", err))

	return file.Name()
}

func createUbootConfigFile(c *check.C, contents map[string]string) (name string) {
	tmpDir, err := ioutil.TempDir("", "snappy-uboot-test")
	c.Assert(err, check.IsNil, check.Commentf("Error creating temp dir: %s", err))
	fileName := path.Join(tmpDir, "uboot.env")
	env, err := uenv.Create(fileName, 4096)
	c.Assert(err, check.IsNil, check.Commentf("Error creating the uboot env file: %s", err))
	for key, value := range contents {
		env.Set(key, value)
	}
	err = env.Save()
	c.Assert(err, check.IsNil, check.Commentf("Error saving the uboot env file: %s", err))
	return fileName
}

func (s *confTestSuite) TestGetConfValue(c *check.C) {
	for _, s.system = range []string{"grub", "uboot"} {
		cfgFileContents := map[string]string{
			"blabla1": "bla",
			"testkey": "testvalue",
			"blabla2": "bla",
		}
		cfgFile := createConfigFile(c, s.system, cfgFileContents)
		defer os.Remove(cfgFile)
		configFiles = map[string]string{s.system: cfgFile}

		value, err := confValue("testkey")
		c.Check(err, check.IsNil, check.Commentf("Error getting configuration value: %s", err))
		c.Check(value, check.Equals, "testvalue", check.Commentf("Wrong configuration value"))
	}
}
