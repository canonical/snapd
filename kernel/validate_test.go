// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/kernel"
)

type validateKernelSuite struct {
	dir string
}

var _ = Suite(&validateKernelSuite{})

func (s *validateKernelSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
}

func (s *validateKernelSuite) TestValidateMissingContentFile(c *C) {
	var kernelYaml = `
assets:
  dtbs:
    edition: 1
    content:
      - foo
`
	mockKernelRoot := makeMockKernel(c, kernelYaml, nil)

	err := kernel.Validate(mockKernelRoot)
	c.Assert(err, ErrorMatches, `asset "dtbs": content "foo" source path does not exist`)
}

func (s *validateKernelSuite) TestValidateMissingContentDir(c *C) {
	var kernelYaml = `
assets:
  dtbs:
    edition: 1
    content:
      - dir/
`
	mockKernelRoot := makeMockKernel(c, kernelYaml, map[string]string{"dir": ""})

	err := kernel.Validate(mockKernelRoot)
	c.Assert(err, ErrorMatches, `asset "dtbs": content "dir/" is not a directory`)
}

func (s *validateKernelSuite) TestValidateHappy(c *C) {
	var kernelYaml = `
assets:
  dtbs:
    edition: 1
    content:
      - foo
      - dir/
`
	mockKernelRoot := makeMockKernel(c, kernelYaml, map[string]string{
		"foo": "",
	})
	err := os.MkdirAll(filepath.Join(mockKernelRoot, "dir"), 0755)
	c.Assert(err, IsNil)

	err = kernel.Validate(mockKernelRoot)
	c.Assert(err, IsNil)
}

func (s *validateKernelSuite) TestValidateHappyNoKernelYaml(c *C) {
	emptyDir := c.MkDir()
	err := kernel.Validate(emptyDir)
	c.Assert(err, IsNil)
}

func (s *validateKernelSuite) TestValidateBadContent(c *C) {
	var kernelYamlFmt = `
assets:
  dtbs:
    edition: 1
    content:
      - %s
`
	for _, tc := range []string{
		"../",
		"/foo/../bar/..",
		"..",
		"//",
	} {
		mockKernelRoot := makeMockKernel(c, fmt.Sprintf(kernelYamlFmt, tc), nil)

		err := kernel.Validate(mockKernelRoot)
		c.Assert(err, ErrorMatches, fmt.Sprintf(`asset "dtbs": invalid content %q`, tc))
	}
}
