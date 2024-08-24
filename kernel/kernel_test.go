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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/kernel"
	"github.com/snapcore/snapd/snap"
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

type kernelYamlTestSuite struct{}

var _ = Suite(&kernelYamlTestSuite{})

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

func (s *kernelYamlTestSuite) TestInfoFromKernelYamlSad(c *C) {
	ki, err := kernel.InfoFromKernelYaml([]byte("foo"))
	c.Check(err, ErrorMatches, "(?m)cannot parse kernel metadata: .*")
	c.Check(ki, IsNil)
}

func (s *kernelYamlTestSuite) TestInfoFromKernelYamlBadName(c *C) {
	ki, err := kernel.InfoFromKernelYaml(mockInvalidKernelYaml)
	c.Check(err, ErrorMatches, `invalid asset name "non-#alphanumeric", please use only alphanumeric characters and dashes`)
	c.Check(ki, IsNil)
}

func (s *kernelYamlTestSuite) TestInfoFromKernelYamlHappy(c *C) {
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

func (s *kernelYamlTestSuite) TestReadKernelYamlOptional(c *C) {
	ki, err := kernel.ReadInfo("this-path-does-not-exist")
	c.Check(err, IsNil)
	c.Check(ki, DeepEquals, &kernel.Info{})
}

func (s *kernelYamlTestSuite) TestReadKernelYamlSad(c *C) {
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

func (s *kernelYamlTestSuite) TestReadKernelYamlHappy(c *C) {
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

func (s *kernelYamlTestSuite) TestDynamicModulesValues(c *C) {
	const kernelYaml = "dynamic-modules: %s\n"

	for _, val := range []string{"", "$SNAP_DATA", "${SNAP_DATA}"} {
		ki, err := kernel.InfoFromKernelYaml([]byte(fmt.Sprintf(kernelYaml, val)))
		c.Check(err, IsNil)
		c.Check(ki, DeepEquals, &kernel.Info{DynamicModules: val})
		dynDir := ""
		if val != "" {
			dynDir = filepath.Join(dirs.SnapDataDir, "mykernel/33")
		}
		c.Check(ki.DynamicModulesDir("mykernel", snap.R(33)), Equals, dynDir)
	}

	for _, val := range []string{"$SNAP_COMMON", "foo", "-xx-"} {
		ki, err := kernel.InfoFromKernelYaml([]byte(fmt.Sprintf(kernelYaml, val)))
		c.Check(ki, IsNil)
		c.Check(err, ErrorMatches, fmt.Sprintf(`invalid value for dynamic-modules: .* \(only valid value is \$SNAP_DATA at the moment\)`))
	}
}
