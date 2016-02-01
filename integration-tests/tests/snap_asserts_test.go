// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package tests

import (
	"bytes"
	"io"

	"gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
)

var _ = check.Suite(&snapAssertsSuite{})

type snapAssertsSuite struct {
	// FIXME: use snapdTestSuite until all tests are moved to
	// assume the snapd/snap command pairing
	snapdTestSuite
}

// FIXME: "snap asserts" shouldn't require sudo, need to tweak snapd test re-activation

func (s *snapAssertsSuite) TestAll(c *check.C) {
	// add an account key
	cli.ExecCommand(c, "sudo", "snap", "assert", "integration-tests/data/dev1.acckey")
	defer cli.ExecCommand(c, "sudo", "rm", "-rf", dev1AccKeyFiles)

	out := cli.ExecCommand(c, "sudo", "snap", "asserts", "account-key")
	dec := asserts.NewDecoder(bytes.NewBufferString(out))
	assertions := []asserts.Assertion{}
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		c.Assert(err, check.IsNil)
		assertions = append(assertions, a)
	}
	c.Check(assertions, check.HasLen, 2)
}

func (s *snapAssertsSuite) TestFilter(c *check.C) {
	// add an account key
	cli.ExecCommand(c, "sudo", "snap", "assert", "integration-tests/data/dev1.acckey")
	defer cli.ExecCommand(c, "sudo", "rm", "-rf", dev1AccKeyFiles)

	out := cli.ExecCommand(c, "sudo", "snap", "asserts", "account-key", "account-id=developer1")
	dec := asserts.NewDecoder(bytes.NewBufferString(out))
	assertions := []asserts.Assertion{}
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		c.Assert(err, check.IsNil)
		assertions = append(assertions, a)
	}
	c.Check(assertions, check.HasLen, 1)
	c.Check(assertions[0].(*asserts.AccountKey).AccountID(), check.Equals, "developer1")
}
