// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package main_test

import (
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"

	snap "github.com/snapcore/snapd/cmd/snap"
)

var statement = []byte(fmt.Sprintf(`{"type": "snap-build",
"authority-id": "devel1",
"series": "16",
"snap-id": "snapidsnapidsnapidsnapidsnapidsn",
"snap-sha3-384": "QlqR0uAWEAWF5Nwnzj5kqmmwFslYPu1IL16MKtLKhwhv0kpBv5wKZ_axf_nf_2cL",
"snap-size": "1",
"grade": "devel",
"timestamp": %q
}`, time.Now().Format(time.RFC3339)))

func (s *SnapKeysSuite) TestHappyDefaultKey(c *C) {
	s.stdin.Write(statement)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	a, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
}

func (s *SnapKeysSuite) TestHappyNonDefaultKey(c *C) {
	s.stdin.Write(statement)

	rest, err := snap.Parser(snap.Client()).ParseArgs([]string{"sign", "-k", "another"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	a, err := asserts.Decode(s.stdout.Bytes())
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapBuildType)
}
