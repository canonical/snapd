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

func (s *snapAssertsSuite) TestAll(c *check.C) {
	// add an account key
	cli.ExecCommand(c, "sudo", "snap", "assert", "integration-tests/data/dev1.acckey")
	// XXX: need to cleanup that assertion, same in snap_assert_test!

	out := cli.ExecCommand(c, "sudo", "snap", "asserts", "account-key")
	dec := asserts.NewDecoder(bytes.NewBufferString(out))
	asserts := []asserts.Assertion{}
	for {
		a, err := dec.Decode()
		if err == io.EOF {
			break
		}
		c.Assert(err, check.IsNil)
		asserts = append(asserts, a)
	}
	c.Check(asserts, check.HasLen, 1) // XXX: should be 2
}
