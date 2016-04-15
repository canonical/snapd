// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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
	"io"
	"os/exec"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/cli"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&consoleSuite{})

type consoleSuite struct {
	common.SnappySuite
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	outbr  *bufio.Reader
}

type consoleMsg struct {
	txt string
	err error
}

func (s *consoleSuite) SetUpTest(c *check.C) {
	s.SnappySuite.SetUpTest(c)

	c.Skip("FIXME: port to snap")

	var err error

	cmdsIn := []string{"snappy", "console"}
	cmdsOut, err := cli.AddOptionsToCommand(cmdsIn)
	c.Assert(err, check.IsNil, check.Commentf("Error adding coverage options, %q", err))

	s.cmd = exec.Command(cmdsOut[0], cmdsOut[1:]...)
	s.stdin, err = s.cmd.StdinPipe()
	c.Assert(err, check.IsNil, check.Commentf("Expected nil error, got %s", err))
	s.stdout, err = s.cmd.StdoutPipe()
	c.Assert(err, check.IsNil, check.Commentf("Expected nil error, got %s", err))

	s.outbr = bufio.NewReader(s.stdout)

	err = s.cmd.Start()
	c.Assert(err, check.IsNil, check.Commentf("Expected nil error, got %s", err))

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
	expected := []string{"Welcome to the snappy console\n",
		"Type 'help' for help\n",
		"Type 'shell' for entering a shell\n"}

	match, err := s.matchLinesFromConsole(expected)

	c.Assert(err, check.IsNil, check.Commentf("Expected nil error, got %s", err))
	c.Assert(match, check.Equals, true,
		check.Commentf("Console output didn't match expected value"))
}

func (s *consoleSuite) TestHelp(c *check.C) {
	s.writeToConsole("help\n")

	expected := []string{"> Usage:\n",
		"  snappy [OPTIONS] <command>\n", "\n",
		"Help Options:\n",
		"  -h, --help  Show this help message\n", "\n",
		"Available commands:\n"}

	match, err := s.matchLinesFromConsole(expected)

	c.Assert(err, check.IsNil, check.Commentf("Expected nil error, got %s", err))
	c.Assert(match, check.Equals, true,
		check.Commentf("Console output didn't match expected value"))
}

func (s *consoleSuite) writeToConsole(msg string) (err error) {
	_, err = s.stdin.Write([]byte(msg))
	return
}

func (s *consoleSuite) getConsoleChannel(len int) chan *consoleMsg {
	c := make(chan *consoleMsg)
	go func() {
		for i := 0; i < len; i++ {
			txt, err := s.outbr.ReadString(byte('\n'))
			if err == io.EOF {
				close(c)
				return
			}
			if err != nil {
				c <- &consoleMsg{"", err}
			}
			c <- &consoleMsg{txt, nil}
		}
	}()
	return c
}

func (s *consoleSuite) matchLinesFromConsole(lines []string) (match bool, err error) {
	c := s.getConsoleChannel(len(lines))

	for _, expected := range lines {
		msg := <-c
		if msg.err != nil {
			return false, msg.err
		}
		if msg.txt != expected {
			return false, nil
		}
	}
	return true, nil
}
