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

package gadget_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
)

type kernelYamlTestSuite struct{}

var _ = Suite(&kernelYamlTestSuite{})

var mockKernelYaml = []byte(`
assets:
  dtbs:
    edition: 1
    content:
      - dtbs/bcm2711-rpi-4-b.dtb
      - dtbs/bcm2836-rpi-2-b.dtb
`)

func (s *kernelYamlTestSuite) TestInfoFromKernelYamlSad(c *C) {
	ki, err := gadget.KernelInfoFromKernelYaml([]byte("foo"))
	c.Check(err, ErrorMatches, "(?m)cannot parse kernel metadata: .*")
	c.Check(ki, IsNil)
}

func (s *kernelYamlTestSuite) TestInfoFromKernelYamlHappy(c *C) {
	ki, err := gadget.KernelInfoFromKernelYaml(mockKernelYaml)
	c.Check(err, IsNil)
	c.Check(ki, DeepEquals, &gadget.KernelInfo{
		Assets: map[string]*gadget.KernelAsset{
			"dtbs": &gadget.KernelAsset{
				Edition: 1,
				Content: []string{
					"dtbs/bcm2711-rpi-4-b.dtb",
					"dtbs/bcm2836-rpi-2-b.dtb",
				},
			},
		},
	})
}

func (s *kernelYamlTestSuite) TestReadKernelYamlSad(c *C) {
	ki, err := gadget.ReadKernelInfo("this-path-does-not-exist")
	c.Check(err, ErrorMatches, `cannot read kernel info: open this-path-does-not-exist/meta/kernel.yaml: no such file or directory`)
	c.Check(ki, IsNil)
}

func (s *kernelYamlTestSuite) TestReadKernelYamlHappy(c *C) {
	mockKernelSnapRoot := c.MkDir()
	kernelYamlPath := filepath.Join(mockKernelSnapRoot, "meta/kernel.yaml")
	err := os.MkdirAll(filepath.Dir(kernelYamlPath), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(kernelYamlPath, mockKernelYaml, 0644)
	c.Assert(err, IsNil)

	ki, err := gadget.ReadKernelInfo(mockKernelSnapRoot)
	c.Assert(err, IsNil)
	c.Check(ki, DeepEquals, &gadget.KernelInfo{
		Assets: map[string]*gadget.KernelAsset{
			"dtbs": &gadget.KernelAsset{
				Edition: 1,
				Content: []string{
					"dtbs/bcm2711-rpi-4-b.dtb",
					"dtbs/bcm2836-rpi-2-b.dtb",
				},
			},
		},
	})
}
