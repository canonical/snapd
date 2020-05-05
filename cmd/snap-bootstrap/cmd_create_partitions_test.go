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
	"io/ioutil"
	"path/filepath"

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

var testModel = `type: model
authority-id: brand-id1
series: 16
brand-id: brand-id1
model: baz-3000
display-name: Baz 3000
architecture: amd64
system-user-authority: *
base: core20
store: brand-store
snaps:
  -
    name: baz-linux
    id: bazlinuxidididididididididididid
    type: kernel
    default-channel: 20
  -
    name: brand-gadget
    id: brandgadgetdidididididididididid
    type: gadget
  -
    name: other-base
    id: otherbasedididididididididididid
    type: base
grade: secured
body-length: 0
timestamp: 2002-10-02T10:00:00-05:00
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`

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
		c.Check(opts.Model.Model(), Equals, "baz-3000")
		n++
		return nil
	})
	defer restore()

	mockModelPath := filepath.Join(c.MkDir(), "model")
	err := ioutil.WriteFile(mockModelPath, []byte(testModel), 0644)
	c.Assert(err, IsNil)

	rest, err := main.Parser().ParseArgs([]string{
		"create-partitions",
		"--encrypt",
		"--key-file", "keyfile",
		"--recovery-key-file", "recovery",
		"--tpm-lockout-auth", "lockout",
		"--policy-update-data-file", "update",
		"--kernel", "kernel",
		"--model", mockModelPath,
		"gadget-dir",
		"device",
	})
	c.Assert(err, IsNil)
	c.Assert(rest, HasLen, 0)
	c.Assert(n, Equals, 1)
}
