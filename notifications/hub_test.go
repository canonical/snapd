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

package notifications

import (
	"encoding/json"
	"errors"
	"testing"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) {
	TestingT(t)
}

type HubSuite struct {
	h *Hub
}

var _ = Suite(&HubSuite{})

type fakeConn struct {
	message []byte
	err     error
}

func (c *fakeConn) WriteMessage(messageType int, data []byte) error {
	c.message = data
	return c.err
}

var _ messageWriter = &fakeConn{}

func (s *HubSuite) SetUpTest(c *C) {
	s.h = NewHub()
	c.Assert(s.h.subscribers, HasLen, 0)
}

func (s *HubSuite) TestSubscribe(c *C) {
	sub := &Subscriber{uuid: "sub"}

	s.h.Subscribe(sub)
	c.Assert(s.h.subscribers, DeepEquals, Subscribers{"sub": sub})

	// can only subscribe once
	s.h.Subscribe(sub)
	c.Assert(s.h.subscribers, DeepEquals, Subscribers{"sub": sub})
}

func (s *HubSuite) TestUnsubscribe(c *C) {
	sub1 := &Subscriber{uuid: "sub1"}
	sub2 := &Subscriber{uuid: "sub2"}
	s.h.subscribers = Subscribers{"sub1": sub1, "sub2": sub2}

	s.h.Unsubscribe(sub1)
	c.Assert(s.h.subscribers, DeepEquals, Subscribers{"sub2": sub2})
}

func (s *HubSuite) TestPublish(c *C) {
	conn := &fakeConn{}
	sub := &Subscriber{uuid: "sub", conn: conn}
	s.h.Subscribe(sub)

	n := &Notification{}
	s.h.Publish(n)

	b, err := json.Marshal(n)
	c.Assert(err, IsNil)
	c.Assert(conn.message, DeepEquals, b)
}

func (s *HubSuite) TestPublishUnsubscribesOnFailedNotify(c *C) {
	sub1 := &Subscriber{uuid: "sub1", conn: &fakeConn{}}
	sub2 := &Subscriber{uuid: "sub2", conn: &fakeConn{err: errors.New("fail")}}
	s.h.Subscribe(sub1)
	s.h.Subscribe(sub2)

	s.h.Publish(&Notification{})
	c.Assert(s.h.subscribers, DeepEquals, Subscribers{"sub1": sub1})
}
