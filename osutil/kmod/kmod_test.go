// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package kmod_test

import (
	"errors"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil/kmod"
	"github.com/snapcore/snapd/testutil"
)

func TestRun(t *testing.T) { TestingT(t) }

type kmodSuite struct {
	testutil.BaseTest
}

var _ = Suite(&kmodSuite{})

func (s *kmodSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
}

func (s *kmodSuite) TearDownTest(c *C) {
	s.BaseTest.TearDownTest(c)
}

func (s *kmodSuite) TestModprobeCommandNotFound(c *C) {
	originalPath := os.Getenv("PATH")
	defer func() {
		os.Setenv("PATH", originalPath)
	}()

	os.Unsetenv("PATH")
	mylog.Check(kmod.ModprobeCommand("name", "opt1=v1", "opt2=v2"))
	c.Check(err, ErrorMatches, `exec: "modprobe": executable file not found in \$PATH`)
}

func (s *kmodSuite) TestModprobeCommandFailure(c *C) {
	cmd := testutil.MockCommand(c, "modprobe", "exit 1")
	defer cmd.Restore()
	mylog.Check(kmod.ModprobeCommand("name", "opt1=v1", "opt2=v2"))
	c.Check(err, ErrorMatches, `modprobe failed with exit status 1 \(see syslog for details\)`)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"modprobe", "--syslog", "name", "opt1=v1", "opt2=v2"},
	})
}

func (s *kmodSuite) TestModprobeCommandHappy(c *C) {
	cmd := testutil.MockCommand(c, "modprobe", "")
	defer cmd.Restore()
	mylog.Check(kmod.ModprobeCommand("name", "opt1=v1", "opt2=v2"))
	c.Check(err, IsNil)
	c.Check(cmd.Calls(), DeepEquals, [][]string{
		{"modprobe", "--syslog", "name", "opt1=v1", "opt2=v2"},
	})
}

func (s *kmodSuite) TestLoadModule(c *C) {
	var returnValue error
	var receivedArguments []string
	restore := kmod.MockModprobeCommand(func(args ...string) error {
		receivedArguments = args
		return returnValue
	})
	defer restore()

	for _, testData := range []struct {
		moduleName    string
		moduleOptions []string
		expectedArgs  []string
		expectedError error
	}{
		{"mymodule", nil, []string{"mymodule"}, nil},
		{"mymodule", []string{"just one"}, []string{"mymodule", "just one"}, nil},
		{"mod1", []string{"opt1=v1", "opt2=v2"}, []string{"mod1", "opt1=v1", "opt2=v2"}, nil},
		{"mod2", []string{}, []string{"mod2"}, errors.New("some error")},
	} {
		returnValue = testData.expectedError
		mylog.Check(kmod.LoadModule(testData.moduleName, testData.moduleOptions))
		c.Check(err, Equals, testData.expectedError)
		c.Check(receivedArguments, DeepEquals, testData.expectedArgs)
	}
}

func (s *kmodSuite) TestUnloadModule(c *C) {
	var returnValue error
	var receivedArguments []string
	restore := kmod.MockModprobeCommand(func(args ...string) error {
		receivedArguments = args
		return returnValue
	})
	defer restore()

	for _, testData := range []struct {
		moduleName    string
		expectedArgs  []string
		expectedError error
	}{
		{"mymodule", []string{"-r", "mymodule"}, nil},
		{"mod2", []string{"-r", "mod2"}, errors.New("some error")},
	} {
		returnValue = testData.expectedError
		mylog.Check(kmod.UnloadModule(testData.moduleName))
		c.Check(err, Equals, testData.expectedError)
		c.Check(receivedArguments, DeepEquals, testData.expectedArgs)
	}
}
