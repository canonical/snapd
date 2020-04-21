// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-bootstrap"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
)

func (s *cmdSuite) TestCreatePartitionsHappy(c *C) {
	n := 0
	restore := main.MockBootstrapRun(func(gadgetRoot, device string, opts bootstrap.Options) error {
		c.Check(gadgetRoot, Equals, "gadget-dir")
		c.Check(device, Equals, "device")
		n++
		return nil
	})
	defer restore()

	rest, err := main.Parser().ParseArgs([]string{"create-partitions", "gadget-dir", "device"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Assert(n, Equals, 1)
}

func (s *cmdSuite) TestCreatePartitionsMount(c *C) {
	n := 0
	restore := main.MockBootstrapRun(func(gadgetRoot, device string, opts bootstrap.Options) error {
		c.Check(gadgetRoot, Equals, "gadget-dir")
		c.Check(device, Equals, "device")
		c.Check(opts.Mount, Equals, true)
		n++
		return nil
	})
	defer restore()

	rest, err := main.Parser().ParseArgs([]string{"create-partitions", "--mount", "gadget-dir", "device"})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Assert(n, Equals, 1)
}

func (s *cmdSuite) TestCreatePartitionsWithEncryption(c *C) {
	n := 0
	restore := main.MockBootstrapRun(func(gadgetRoot, device string, opts bootstrap.Options) error {
		c.Check(gadgetRoot, Equals, "gadget-dir")
		c.Check(device, Equals, "device")
		c.Check(opts.Encrypt, Equals, true)
		c.Check(opts.KeyFile, Equals, "keyfile")
		c.Check(opts.RecoveryKeyFile, Equals, "recovery")
		c.Check(opts.TPMLockoutAuthFile, Equals, "lockout")
		c.Check(opts.PolicyUpdateDataFile, Equals, "update")
		c.Check(opts.KernelPath, Equals, "kernel")
		c.Check(opts.SystemLabel, Equals, "20041020")
		n++
		return nil
	})
	defer restore()

	rest, err := main.Parser().ParseArgs([]string{
		"create-partitions",
		"--encrypt",
		"--key-file", "keyfile",
		"--recovery-key-file", "recovery",
		"--tpm-lockout-auth", "lockout",
		"--policy-update-data-file", "update",
		"--kernel", "kernel",
		"--system-label", "20041020",
		"gadget-dir",
		"device",
	})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Assert(n, Equals, 1)
}
