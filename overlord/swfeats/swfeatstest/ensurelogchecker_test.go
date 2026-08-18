// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package swfeatstest_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/snapcore/snapd/overlord/swfeats/swfeatstest"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ensureLogCheckerSuite struct{}

var _ = Suite(&ensureLogCheckerSuite{})

func (s *ensureLogCheckerSuite) TestCheckEnsureLoopLogging(c *C) {
	swfeatstest.CheckEnsureLoopLogging("example_test.go", c, true)
}

type unregisteredEnsureSuite struct {
	filename string
}

func (s *unregisteredEnsureSuite) TestCheckEnsureLoopLogging(c *C) {
	swfeatstest.CheckEnsureLoopLogging(s.filename, c, true)
}

func (s *ensureLogCheckerSuite) TestCheckEnsureLoopLoggingUnregisteredEnsure(c *C) {
	filename := filepath.Join(c.MkDir(), "unregistered-example.go")
	err := os.WriteFile(filename, []byte(`package swfeatstest

import "github.com/snapcore/snapd/logger"

type unregisteredEnsureManager struct{}

func (m *unregisteredEnsureManager) Ensure() error {
	m.ensureChild()
	return nil
}

func (m *unregisteredEnsureManager) ensureChild() {
	logger.Trace("ensure", "manager", "unregisteredEnsureManager", "func", "ensureChild")
}
`), 0644)
	c.Assert(err, IsNil)

	var output bytes.Buffer
	result := Run(&unregisteredEnsureSuite{filename: filename}, &RunConf{Output: &output})
	c.Check(result.Succeeded, Equals, 0)
	c.Check(result.Failed, Equals, 1)
	c.Check(result.Panicked, Equals, 0)
	c.Check(result.FixturePanicked, Equals, 0)
	c.Check(result.RunError, IsNil)
	c.Check(strings.Contains(output.String(), "unregisteredEnsureManager"), Equals, true)
	c.Check(strings.Contains(output.String(), "ensureChild"), Equals, true)
	c.Check(strings.Contains(output.String(), "is not registered"), Equals, true)
}

func (s *ensureLogCheckerSuite) TestCheckEnsureLoopLoggingEnsureNotLogged(c *C) {
	filename := filepath.Join(c.MkDir(), "unregistered-example.go")
	err := os.WriteFile(filename, []byte(`package swfeatstest

import "github.com/snapcore/snapd/logger"

type notLoggedEnsureManager struct{}

func (m *notLoggedEnsureManager) Ensure() error {
	m.ensureChild()
	return nil
}

func (m *notLoggedEnsureManager) ensureChild() {
}
`), 0644)
	c.Assert(err, IsNil)

	var output bytes.Buffer
	result := Run(&unregisteredEnsureSuite{filename: filename}, &RunConf{Output: &output})
	c.Check(result.Succeeded, Equals, 0)
	c.Check(result.Failed, Equals, 1)
	c.Check(result.Panicked, Equals, 0)
	c.Check(result.FixturePanicked, Equals, 0)
	c.Check(result.RunError, IsNil)
	c.Check(strings.Contains(output.String(), "notLoggedEnsureManager"), Equals, true)
	c.Check(strings.Contains(output.String(), "ensureChild"), Equals, true)
	c.Check(strings.Contains(output.String(), "trace log was not found"), Equals, true)
}
