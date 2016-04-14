// -*- Mode: Go; indent-tabs-mode: t -*-
// +build darwin freebsd linux
// +build cgo

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

package osutil

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type groupTestSuite struct {
}

var _ = Suite(&groupTestSuite{})

func getgrnamForking(name string) (grp Group, err error) {
	cmd := exec.Command("getent", "group", name)
	output, err := cmd.Output()
	if err != nil {
		return grp, fmt.Errorf("getent failed for %s: %s", name, err)
	}

	parsed := strings.Split(strings.TrimSpace(string(output)), ":")
	gid, err := strconv.Atoi(parsed[2])
	if err != nil {
		return grp, fmt.Errorf("failed to parse gid field %v: %s", gid, err)
	}

	grp.Name = parsed[0]
	grp.Passwd = parsed[1]
	grp.Gid = uint(gid)
	if parsed[3] != "" {
		grp.Mem = strings.Split(parsed[3], ",")
	}

	return grp, nil
}

func (s *groupTestSuite) TestGetgrnam(c *C) {
	expected, err := getgrnamForking("adm")
	c.Assert(err, IsNil)
	groups, err := Getgrnam("adm")
	c.Assert(err, IsNil)
	c.Assert(groups, DeepEquals, expected)
}

func (s *groupTestSuite) TestGetgrnamNoSuchGroup(c *C) {
	needle := "no-such-group-really-no-no"
	_, err := Getgrnam(needle)
	c.Assert(err, ErrorMatches, fmt.Sprintf("group \"%s\" not found", needle))
}

func (s *groupTestSuite) TestGetgrnamEmptyGroup(c *C) {
	expected, err := getgrnamForking("floppy")
	c.Assert(err, IsNil)
	groups, err := Getgrnam("floppy")
	c.Assert(err, IsNil)
	c.Assert(groups, DeepEquals, expected)
}

func (s *groupTestSuite) TestIsUIDInAnyEmpty(c *C) {
	c.Check(IsUIDInAny(0), Equals, false)
}

func (s *groupTestSuite) TestIsUIDInAnyBad(c *C) {
	c.Check(IsUIDInAny(0, "no-such-group-really-no-no"), Equals, false)
}

func (s *groupTestSuite) TestIsUIDInAnySelf(c *C) {
	c.Check(IsUIDInAny(0, "root"), Equals, true)
}
