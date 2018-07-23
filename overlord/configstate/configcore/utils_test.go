// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package configcore_test

import (
	"bytes"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/configcore"
)

type utilsSuite struct{}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) TestUpdateKeyValueStreamNoNewConfig(c *C) {
	in := bytes.NewBufferString("foo=bar")
	newConfig := map[string]string{}
	supportedConfigKeys := map[string]bool{}

	toWrite, err := configcore.UpdateKeyValueStream(in, supportedConfigKeys, newConfig)
	c.Check(err, IsNil)
	c.Check(toWrite, IsNil)
}

func (s *utilsSuite) TestUpdateKeyValueStreamConfigNotInAllConfig(c *C) {
	in := bytes.NewBufferString("")
	newConfig := map[string]string{"unsupported-options": "cannot be set"}
	supportedConfigKeys := map[string]bool{
		"foo": true,
	}

	_, err := configcore.UpdateKeyValueStream(in, supportedConfigKeys, newConfig)
	c.Check(err, ErrorMatches, `cannot set unsupported configuration value "unsupported-options"`)
}

func (s *utilsSuite) TestUpdateKeyValueStreamOneChange(c *C) {
	in := bytes.NewBufferString("foo=bar")
	newConfig := map[string]string{"foo": "baz"}
	supportedConfigKeys := map[string]bool{
		"foo": true,
	}

	toWrite, err := configcore.UpdateKeyValueStream(in, supportedConfigKeys, newConfig)
	c.Check(err, IsNil)
	c.Check(toWrite, DeepEquals, []string{"foo=baz"})
}
