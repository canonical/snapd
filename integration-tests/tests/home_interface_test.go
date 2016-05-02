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
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&homeInterfaceSuite{
	interfaceSuite: interfaceSuite{
		sampleSnap: data.HomeConsumerSnapName,
		slot:       "home",
		plug:       "home-consumer"}})

type homeInterfaceSuite struct {
	interfaceSuite
}

func (s *homeInterfaceSuite) TestPlugDisconnectionDisablesRead(c *check.C) {
	cli.ExecCommand(c, "sudo", "snap", "connect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	fileName, err := createHomeFile("readable", okOutput)
	c.Assert(err, check.IsNil)
	defer os.Remove(fileName)

	output := cli.ExecCommand(c, "home-consumer.reader", fileName)
	c.Assert(output, check.Equals, okOutput)

	cli.ExecCommand(c, "sudo", "snap", "disconnect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	output, err = cli.ExecCommandErr("home-consumer.reader", fileName)
	c.Assert(err, check.NotNil)
	c.Assert(output == okOutput, check.Equals, false)
}

func (s *homeInterfaceSuite) TestPlugDisconnectionDisablesAppend(c *check.C) {
	cli.ExecCommand(c, "sudo", "snap", "connect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	previousContent := "previous content\n"
	fileName, err := createHomeFile("writable", previousContent)
	c.Assert(err, check.IsNil)

	cli.ExecCommand(c, "home-consumer.writer", fileName)

	dat, err := ioutil.ReadFile(fileName)
	c.Assert(err, check.IsNil)
	c.Assert(string(dat), check.Equals, previousContent+okOutput)

	err = os.Remove(fileName)
	c.Assert(err, check.IsNil)

	cli.ExecCommand(c, "sudo", "snap", "disconnect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	_, err = cli.ExecCommandErr("home-consumer.writer", fileName)
	c.Assert(err, check.NotNil)
}

func (s *homeInterfaceSuite) TestPlugDisconnectionDisablesCreate(c *check.C) {
	cli.ExecCommand(c, "sudo", "snap", "connect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	home := os.Getenv("HOME")
	fileName := filepath.Join(home, "writable")

	cli.ExecCommand(c, "home-consumer.writer", fileName)

	dat, err := ioutil.ReadFile(fileName)
	c.Assert(err, check.IsNil)
	c.Assert(string(dat), check.Equals, okOutput)

	err = os.Remove(fileName)
	c.Assert(err, check.IsNil)

	cli.ExecCommand(c, "sudo", "snap", "disconnect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	_, err = cli.ExecCommandErr("home-consumer.writer", fileName)
	c.Assert(err, check.NotNil)
}

func (s *homeInterfaceSuite) TestReadHiddenFilesForbidden(c *check.C) {
	cli.ExecCommand(c, "sudo", "snap", "connect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	fileName, err := createHomeFile(".readable", okOutput)
	c.Assert(err, check.IsNil)
	defer os.Remove(fileName)

	output, err := cli.ExecCommandErr("home-consumer.reader", fileName)
	c.Assert(err, check.NotNil)
	c.Assert(output == okOutput, check.Equals, false)
}

func (s *homeInterfaceSuite) TestWriteHiddenFilesForbidden(c *check.C) {
	cli.ExecCommand(c, "sudo", "snap", "connect",
		s.plug+":"+s.slot, "ubuntu-core:"+s.slot)

	previousContent := "previous content\n"
	fileName, err := createHomeFile(".writable", previousContent)
	c.Assert(err, check.IsNil)
	defer os.Remove(fileName)

	_, err = cli.ExecCommandErr("home-consumer.writer", fileName)
	c.Assert(err, check.NotNil)

	dat, err := ioutil.ReadFile(fileName)
	c.Assert(err, check.IsNil)
	c.Assert(string(dat), check.Equals, previousContent)
}

func createHomeFile(name, content string) (path string, err error) {
	home := os.Getenv("HOME")
	path = filepath.Join(home, name)
	err = ioutil.WriteFile(path, []byte(content), 0644)

	return path, err
}
