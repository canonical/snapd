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

package hookstate

import (
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	. "gopkg.in/check.v1"
)

type handlerSuite struct {
	collection *handlerCollection
}

var _ = Suite(&handlerSuite{})

func (s *handlerSuite) SetUpTest(c *C) {
	s.collection = newHandlerCollection()
}

func (s *handlerSuite) TestAddHandler(c *C) {
	mockHandler1 := hooktest.NewMockHandler()
	mockHandler2 := hooktest.NewMockHandler()

	c.Check(s.collection.handlerCount(), Equals, 0)
	s.collection.addHandler(mockHandler1)
	c.Check(s.collection.handlerCount(), Equals, 1)
	s.collection.addHandler(mockHandler2)
	c.Check(s.collection.handlerCount(), Equals, 2)
}

func (s *handlerSuite) TestRemoveHandler(c *C) {
	mockHandler1 := hooktest.NewMockHandler()
	mockHandler2 := hooktest.NewMockHandler()

	handlerID1, err := s.collection.addHandler(mockHandler1)
	c.Check(err, IsNil)
	handlerID2, err := s.collection.addHandler(mockHandler2)
	c.Check(err, IsNil)

	s.collection.removeHandler(handlerID1)
	s.collection.removeHandler(handlerID2)
	c.Check(s.collection.handlerCount(), Equals, 0)
}

func (s *handlerSuite) TestGetHandler(c *C) {
	mockHandler1 := hooktest.NewMockHandler()
	mockHandler2 := hooktest.NewMockHandler()

	handlerID1, err := s.collection.addHandler(mockHandler1)
	c.Check(err, IsNil)
	handlerID2, err := s.collection.addHandler(mockHandler2)
	c.Check(err, IsNil)

	handler, err := s.collection.getHandler(handlerID1)
	c.Check(err, IsNil)
	c.Check(handler, Equals, mockHandler1)

	handler, err = s.collection.getHandler(handlerID2)
	c.Check(err, IsNil)
	c.Check(handler, Equals, mockHandler2)
}

func (s *handlerSuite) TestGetMissingHandler(c *C) {
	handler, err := s.collection.getHandler("foo")
	c.Check(err, NotNil)
	c.Check(handler, IsNil)
}
