// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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
	"bufio"
	"bytes"
	"io"
	"os/exec"

	"launchpad.net/snappy/_integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

const (
	welcomePromptLines = 3
	helpPromptLines    = 7
)

var _ = check.Suite(&consoleSuite{})

type consoleSuite struct {
	common.SnappySuite
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	outbr  *bufio.Reader
}

func (s *consoleSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	var err error
	s.cmd = exec.Command("snappy", "console")
	s.stdin, err = s.cmd.StdinPipe()
	c.Assert(err, check.IsNil)
	s.stdout, err = s.cmd.StdoutPipe()
	c.Assert(err, check.IsNil)

	s.outbr = bufio.NewReader(s.stdout)

	err = s.cmd.Start()
	c.Assert(err, check.IsNil)

	s.checkPrompt(c)
}

func (s *consoleSuite) TearDownTest(c *check.C) {
	s.SnappySuite.TearDownTest(c)

	s.stdin.Close()
	s.stdout.Close()

	proc := s.cmd.Process
	if proc != nil {
		proc.Kill()
	}
}

func (s *consoleSuite) checkPrompt(c *check.C) {
	output, err := s.readLinesFromConsole(welcomePromptLines)
	c.Assert(err, check.IsNil)

	expected := `Welcome to the snappy console
Type 'help' for help
Type 'shell' for entering a shell
`
	c.Assert(output, check.Matches, expected)
}

func (s *consoleSuite) TestHelp(c *check.C) {
	s.writeToConsole("help\n")

	output, err := s.readLinesFromConsole(helpPromptLines)
	c.Assert(err, check.IsNil)

	expected := `(?ms)> Usage:
  snappy \[OPTIONS\] <command>

Help Options:
  -h, --help  Show this help message

Available commands:
`
	c.Assert(output, check.Matches, expected)
}

func (s *consoleSuite) writeToConsole(msg string) (err error) {
	_, err = s.stdin.Write([]byte(msg))
	return
}

func (s *consoleSuite) readLinesFromConsole(nLines int) (line string, err error) {
	var buffer bytes.Buffer

	var byteLine []byte
	for i := 0; i < nLines; i++ {
		byteLine, _, err = s.outbr.ReadLine()
		if err != nil {
			return
		}
		_, err = buffer.Write(byteLine)
		if err != nil {
			return
		}
		_, err = buffer.WriteString("\n")
		if err != nil {
			return
		}
	}
	return buffer.String(), err
}
