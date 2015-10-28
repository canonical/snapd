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

// TODO this should be it's own package, but depends on splitting out
// package.yaml's

package snappy

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
)

type OemSuite struct {
}

var _ = Suite(&OemSuite{})

var (
	getOemOrig = getOem
)

func (s *OemSuite) SetUpTest(c *C) {
	getOem = func() (*packageYaml, error) {
		return &packageYaml{
			OEM: OEM{
				Software: Software{[]string{"makeuppackage", "anotherpackage"}},
				Store:    Store{"ninjablocks"},
			},
		}, nil
	}
}

func (s *OemSuite) TearDownTest(c *C) {
	getOem = getOemImpl
}

func (s *OemSuite) TestIsBuildIn(c *C) {
	c.Check(IsBuiltInSoftware("notapackage"), Equals, false)
	c.Check(IsBuiltInSoftware("makeuppackage"), Equals, true)
	c.Check(IsBuiltInSoftware("anotherpackage"), Equals, true)
}

func (s *OemSuite) TestStoreID(c *C) {
	c.Assert(StoreID(), Equals, "ninjablocks")
}

func (s *OemSuite) TestWriteApparmorAdditionalFile(c *C) {
	m, err := parsePackageYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)

	err = writeApparmorAdditionalFile(m)
	c.Assert(err, IsNil)

	content, err := ioutil.ReadFile(filepath.Join(dirs.SnapAppArmorDir, "device-hive-iot-hal.json.additional"))
	c.Assert(err, IsNil)
	c.Assert(string(content), Equals, apparmorAdditionalContent)
}

func (s *OemSuite) TestCleanupOemHardwareRules(c *C) {
	m, err := parsePackageYamlData(hardwareYaml, false)
	c.Assert(err, IsNil)

	err = writeApparmorAdditionalFile(m)
	c.Assert(err, IsNil)

	additionalFile := filepath.Join(dirs.SnapAppArmorDir, "device-hive-iot-hal.json.additional")
	c.Assert(helpers.FileExists(additionalFile), Equals, true)

	err = cleanupOemHardwareUdevRules(m)
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(additionalFile), Equals, false)
}
