// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package image_test

import (
	"io/ioutil"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/image"
)

func Test(t *testing.T) { TestingT(t) }

type seedYamlTestSuite struct{}

var _ = Suite(&seedYamlTestSuite{})

var seedYaml = []byte(`
assertions:
- type: model
  brand-id: brand
  model: model
  series: 16
snaps:
  name: hello-world
  channel: stable
`)

func (s *seedYamlTestSuite) seedYamlTestSimple(c *C) {
	fn := filepath.Join(c.MkDir(), "seed.yaml")
	err := ioutil.WriteFile(fn, seedYaml, 0644)
	c.Assert(err, IsNil)

	seed, err := image.ReadSeed(fn)
	c.Assert(err, IsNil)
	c.Assert(seed.Assertions, HasLen, 1)
	c.Assert(seed.Snaps, HasLen, 1)
}
