// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package kernel_test

import (
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

func makeMockKernel(c *C, kernelYaml string, filesWithContent map[string]string) string {
	kernelRootDir := c.MkDir()
	err := os.MkdirAll(filepath.Join(kernelRootDir, "meta"), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(filepath.Join(kernelRootDir, "meta/kernel.yaml"), []byte(kernelYaml), 0644)
	c.Assert(err, IsNil)

	for fname, content := range filesWithContent {
		p := filepath.Join(kernelRootDir, fname)
		err = os.MkdirAll(filepath.Dir(p), 0755)
		c.Assert(err, IsNil)
		err = os.WriteFile(p, []byte(content), 0644)
		c.Assert(err, IsNil)
	}

	return kernelRootDir
}

type kernelTestSuite struct {
	testutil.BaseTest
}

var _ = Suite(&kernelTestSuite{})

func (s *kernelTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

func TestCommand(t *testing.T) { TestingT(t) }

var mockKernelYaml = []byte(`
assets:
  dtbs:
    update: true
    content:
      - dtbs/bcm2711-rpi-4-b.dtb
      - dtbs/bcm2836-rpi-2-b.dtb
`)

var mockInvalidKernelYaml = []byte(`
assets:
  non-#alphanumeric:
`)

func (s *kernelTestSuite) TestInfoFromKernelYamlSad(c *C) {
	ki, err := kernel.InfoFromKernelYaml([]byte("foo"))
	c.Check(err, ErrorMatches, "(?m)cannot parse kernel metadata: .*")
	c.Check(ki, IsNil)
}

func (s *kernelTestSuite) TestInfoFromKernelYamlBadName(c *C) {
	ki, err := kernel.InfoFromKernelYaml(mockInvalidKernelYaml)
	c.Check(err, ErrorMatches, `invalid asset name "non-#alphanumeric", please use only alphanumeric characters and dashes`)
	c.Check(ki, IsNil)
}

func (s *kernelTestSuite) TestInfoFromKernelYamlHappy(c *C) {
	ki, err := kernel.InfoFromKernelYaml(mockKernelYaml)
	c.Check(err, IsNil)
	c.Check(ki, DeepEquals, &kernel.Info{
		Assets: map[string]*kernel.Asset{
			"dtbs": {
				Update: true,
				Content: []string{
					"dtbs/bcm2711-rpi-4-b.dtb",
					"dtbs/bcm2836-rpi-2-b.dtb",
				},
			},
		},
	})
}

func (s *kernelTestSuite) TestReadKernelYamlOptional(c *C) {
	ki, err := kernel.ReadInfo("this-path-does-not-exist")
	c.Check(err, IsNil)
	c.Check(ki, DeepEquals, &kernel.Info{})
}

func (s *kernelTestSuite) TestReadKernelYamlSad(c *C) {
	mockKernelSnapRoot := c.MkDir()
	kernelYamlPath := filepath.Join(mockKernelSnapRoot, "meta/kernel.yaml")
	err := os.MkdirAll(filepath.Dir(kernelYamlPath), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(kernelYamlPath, []byte(`invalid-kernel-yaml`), 0644)
	c.Assert(err, IsNil)

	ki, err := kernel.ReadInfo(mockKernelSnapRoot)
	c.Check(err, ErrorMatches, `(?m)cannot parse kernel metadata: yaml: unmarshal errors:.*`)
	c.Check(ki, IsNil)
}

func (s *kernelTestSuite) TestReadKernelYamlHappy(c *C) {
	mockKernelSnapRoot := c.MkDir()
	kernelYamlPath := filepath.Join(mockKernelSnapRoot, "meta/kernel.yaml")
	err := os.MkdirAll(filepath.Dir(kernelYamlPath), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(kernelYamlPath, mockKernelYaml, 0644)
	c.Assert(err, IsNil)

	ki, err := kernel.ReadInfo(mockKernelSnapRoot)
	c.Assert(err, IsNil)
	c.Check(ki, DeepEquals, &kernel.Info{
		Assets: map[string]*kernel.Asset{
			"dtbs": {
				Update: true,
				Content: []string{
					"dtbs/bcm2711-rpi-4-b.dtb",
					"dtbs/bcm2836-rpi-2-b.dtb",
				},
			},
		},
	})
}

func (s *kernelTestSuite) TestKernelVersionFromPlaceInfo(c *C) {
	spi := snap.MinimalPlaceInfo("pc-kernel", snap.R(1))

	c.Assert(os.MkdirAll(spi.MountDir(), 0755), IsNil)

	// No map file
	ver, err := kernel.KernelVersionFromPlaceInfo(spi)
	c.Check(err, ErrorMatches, `number of matches for .* is 0`)
	c.Check(ver, Equals, "")

	// Create file so kernel version can be found
	c.Assert(os.WriteFile(filepath.Join(
		spi.MountDir(), "System.map-5.15.0-78-generic"), []byte{}, 0644), IsNil)
	ver, err = kernel.KernelVersionFromPlaceInfo(spi)
	c.Check(err, IsNil)
	c.Check(ver, Equals, "5.15.0-78-generic")

	// Too many matches
	c.Assert(os.WriteFile(filepath.Join(
		spi.MountDir(), "System.map-6.8.0-71-generic"), []byte{}, 0644), IsNil)
	ver, err = kernel.KernelVersionFromPlaceInfo(spi)
	c.Check(err, ErrorMatches, `number of matches for .* is 2`)
	c.Check(ver, Equals, "")
}

func (s *kernelTestSuite) TestKernelVersionFromPlaceInfoNotSetInFile(c *C) {
	spi := snap.MinimalPlaceInfo("pc-kernel", snap.R(1))

	c.Assert(os.MkdirAll(spi.MountDir(), 0755), IsNil)

	// Create bad file name
	c.Assert(os.WriteFile(filepath.Join(
		spi.MountDir(), "System.map-"), []byte{}, 0644), IsNil)
	ver, err := kernel.KernelVersionFromPlaceInfo(spi)
	c.Check(err, ErrorMatches, `kernel version not set in .*System\.map\-`)
	c.Check(ver, Equals, "")
}
