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

package handlers_test

import (
	"context"
	"testing"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/devicemgmtstate/handlers"
	"github.com/snapcore/snapd/overlord/state"
	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type handlersSuite struct{}

var _ = Suite(&handlersSuite{})

func (s *handlersSuite) TestRequestMessageIdentifiers(c *C) {
	msg := &handlers.RequestMessage{
		AccountID: "operator",
		BaseID:    "message",
	}

	c.Check(msg.ID(), Equals, "message")
	c.Check(msg.Key(), Equals, "operator/message")
	c.Check(msg.SeqKey(), Equals, "operator/message")

	msg.SeqNum = 3

	c.Check(msg.ID(), Equals, "message-3")
	c.Check(msg.Key(), Equals, "operator/message-3")
	c.Check(msg.SeqKey(), Equals, "operator/message")
}

func (s *handlersSuite) TestRequestMessageValidAt(c *C) {
	since := time.Date(2026, 6, 14, 3, 4, 5, 0, time.UTC)
	until := since.Add(time.Hour)
	msg := &handlers.RequestMessage{
		ValidSince: since,
		ValidUntil: until,
	}

	c.Check(msg.ValidAt(since.Add(-time.Nanosecond)), Equals, false)
	c.Check(msg.ValidAt(since), Equals, true)
	c.Check(msg.ValidAt(since.Add(30*time.Minute)), Equals, true)
	c.Check(msg.ValidAt(until), Equals, false)
	c.Check(msg.ValidAt(until.Add(time.Nanosecond)), Equals, false)
}

func (s *handlersSuite) TestRequestMessageTargets(c *C) {
	msg := &handlers.RequestMessage{
		Devices: []string{
			"serial-1.model-1.brand-1",
			"serial-2.model-2.brand-2",
		},
	}

	c.Check(msg.Targets(asserts.DeviceID{
		Serial:  "serial-2",
		Model:   "model-2",
		BrandID: "brand-2",
	}), Equals, true)
	c.Check(msg.Targets(asserts.DeviceID{
		Serial:  "serial-3",
		Model:   "model-3",
		BrandID: "brand-3",
	}), Equals, false)
}

func (s *handlersSuite) TestChangeMessageKey(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	msg := handlers.RequestMessage{
		AccountID: "operator",
		BaseID:    "message",
		SeqNum:    3,
	}
	chg := st.NewChange("subsystem", "apply message")

	key, ok := handlers.ChangeMessageKey(chg)
	c.Check(ok, Equals, false)
	c.Check(key, Equals, "")

	handlers.MarkChangeForMessage(chg, msg)

	key, ok = handlers.ChangeMessageKey(chg)
	c.Check(ok, Equals, true)
	c.Check(key, Equals, "operator/message-3")
}

type mockMessageHandler struct{}

func (*mockMessageHandler) Validate(context.Context, *state.State, handlers.RequestMessage) error {
	return nil
}

func (*mockMessageHandler) Apply(context.Context, *state.State, handlers.RequestMessage) (string, error) {
	return "", nil
}

func (*mockMessageHandler) ResultFromChange(context.Context, *state.Change) (map[string]any, error) {
	return nil, nil
}

func (s *handlersSuite) TestRegisterAndGet(c *C) {
	c.Check(handlers.Get("test-kind"), IsNil)

	handler := &mockMessageHandler{}
	handlers.Register("test-kind", handler)

	c.Check(handlers.Get("test-kind"), Equals, handler)
}
