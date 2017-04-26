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

package hooktest_test

import (
	"fmt"
	"testing"

	"github.com/snapcore/snapd/overlord/hookstate/hooktest"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type hooktestSuite struct {
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&hooktestSuite{})

func (s *hooktestSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()
}

func (s *hooktestSuite) TestBefore(c *C) {
	c.Check(s.mockHandler.BeforeCalled, Equals, false)
	c.Check(s.mockHandler.Before(), IsNil)
	c.Check(s.mockHandler.BeforeCalled, Equals, true)
}

func (s *hooktestSuite) TestBeforeError(c *C) {
	s.mockHandler.BeforeError = true
	c.Check(s.mockHandler.Before(), NotNil)
	c.Check(s.mockHandler.BeforeCalled, Equals, true)
}

func (s *hooktestSuite) TestDone(c *C) {
	c.Check(s.mockHandler.DoneCalled, Equals, false)
	s.mockHandler.Executed = 1
	c.Check(s.mockHandler.Done(), IsNil)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
}

func (s *hooktestSuite) TestDoneError(c *C) {
	s.mockHandler.DoneError = true
	s.mockHandler.Executed = 1
	c.Check(s.mockHandler.Done(), NotNil)
	c.Check(s.mockHandler.DoneCalled, Equals, true)
}

func (s *hooktestSuite) TestError(c *C) {
	err := fmt.Errorf("test error")
	c.Check(s.mockHandler.ErrorCalled, Equals, false)
	c.Check(s.mockHandler.Error(err), IsNil)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)
	c.Check(s.mockHandler.Err, Equals, err)
}

func (s *hooktestSuite) TestErrorError(c *C) {
	s.mockHandler.ErrorError = true
	err := fmt.Errorf("test error")
	c.Check(s.mockHandler.Error(err), NotNil)
	c.Check(s.mockHandler.ErrorCalled, Equals, true)
	c.Check(s.mockHandler.Err, Equals, err)
}
