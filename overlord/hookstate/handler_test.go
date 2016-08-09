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

package hookstate_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/hookstate"
)

type handlerSuite struct {
	collection *hookstate.HandlerCollection
}

var _ = Suite(&handlerSuite{})

func (s *handlerSuite) SetUpTest(c *C) {
	s.collection = hookstate.NewHandlerCollection()
}

func (s *handlerSuite) TestAddActiveHandler(c *C) {
	mockHandler1 := hookstate.NewMockHandler()
	mockHandler2 := hookstate.NewMockHandler()

	c.Check(s.collection.HandlerCount(), Equals, 0)
	s.collection.AddHandler(mockHandler1)
	c.Check(s.collection.HandlerCount(), Equals, 1)
	s.collection.AddHandler(mockHandler2)
	c.Check(s.collection.HandlerCount(), Equals, 2)
}

func (s *handlerSuite) TestRemoveActiveHandler(c *C) {
	mockHandler1 := hookstate.NewMockHandler()
	mockHandler2 := hookstate.NewMockHandler()

	handlerID1 := s.collection.AddHandler(mockHandler1)
	handlerID2 := s.collection.AddHandler(mockHandler2)

	s.collection.RemoveHandler(handlerID1)
	s.collection.RemoveHandler(handlerID2)
	c.Check(s.collection.HandlerCount(), Equals, 0)
}

func (s *handlerSuite) TestGetHandlerData(c *C) {
	mockHandler1 := hookstate.NewMockHandler()
	mockHandler2 := hookstate.NewMockHandler()

	handlerID1 := s.collection.AddHandler(mockHandler1)
	handlerID2 := s.collection.AddHandler(mockHandler2)

	s.collection.GetHandlerData(handlerID1, "foo")
	c.Check(mockHandler1.GetCalled, Equals, true)
	c.Check(mockHandler1.Key, Equals, "foo")

	s.collection.GetHandlerData(handlerID2, "bar")
	c.Check(mockHandler2.GetCalled, Equals, true)
	c.Check(mockHandler2.Key, Equals, "bar")
}

func (s *handlerSuite) TestGetHandlerDataWithoutHandlers(c *C) {
	data, err := s.collection.GetHandlerData(1, "foo")
	c.Check(data, IsNil)
	c.Check(err, ErrorMatches, ".*no handler with ID 1.*")
}

func (s *handlerSuite) TestSetHandlerDataWithoutHandlers(c *C) {
	err := s.collection.SetHandlerData(1, "foo", map[string]interface{}{"bar": nil})
	c.Check(err, ErrorMatches, ".*no handler with ID 1.*")
}

func (s *handlerSuite) TestSetHandlerData(c *C) {
	mockHandler1 := hookstate.NewMockHandler()
	mockHandler2 := hookstate.NewMockHandler()

	handlerID1 := s.collection.AddHandler(mockHandler1)
	handlerID2 := s.collection.AddHandler(mockHandler2)

	s.collection.SetHandlerData(handlerID1, "foo", map[string]interface{}{"bar": nil})
	c.Check(mockHandler1.SetCalled, Equals, true)
	c.Check(mockHandler1.Key, Equals, "foo")
	c.Check(mockHandler1.Data, DeepEquals, map[string]interface{}{"bar": nil})

	s.collection.SetHandlerData(handlerID2, "bar", map[string]interface{}{"baz": nil})
	c.Check(mockHandler2.SetCalled, Equals, true)
	c.Check(mockHandler2.Key, Equals, "bar")
	c.Check(mockHandler2.Data, DeepEquals, map[string]interface{}{"baz": nil})
}
