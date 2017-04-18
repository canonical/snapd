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

package ifacestate_test

import (
	"encoding/json"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces"
	"github.com/ubuntu-core/snappy/overlord/ifacestate"
	"github.com/ubuntu-core/snappy/overlord/state"
)

type stateSuite struct {
	state *state.State
}

var _ = Suite(&stateSuite{})

func (s *stateSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
	s.state.Lock()
}

func (s *stateSuite) TearDownTest(c *C) {
	s.state.Unlock()
}

var sampleConn = ifacestate.Connection{
	Plug: interfaces.PlugRef{Snap: "consumer", Name: "plug"},
	Slot: interfaces.SlotRef{Snap: "producer", Name: "slot"},
}
var sampleConns = ifacestate.Connections{sampleConn: {Auto: true}}

func (s *stateSuite) TestConnectionString(c *C) {
	c.Check(sampleConn.String(), Equals, "consumer:plug producer:slot")
}

func (s *stateSuite) TestConnectionMarshalJSON(c *C) {
	data, err := json.Marshal(sampleConn)
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, `"consumer:plug producer:slot"`)
}

func (s *stateSuite) TestConnectionUnmarshalJSON(c *C) {
	data := []byte(`"consumer:plug producer:slot"`)
	var conn ifacestate.Connection
	err := json.Unmarshal(data, &conn)
	c.Assert(err, IsNil)
	c.Check(conn, DeepEquals, sampleConn)
}

func (s *stateSuite) TestConnectionsMarshalJSON(c *C) {
	data, err := json.Marshal(sampleConns)
	c.Assert(err, IsNil)
	c.Check(string(data), Equals, `{"consumer:plug producer:slot":{"auto":true}}`)
}

func (s *stateSuite) TestConnectionsUnmarshalJSON(c *C) {
	data := []byte(`{"consumer:plug producer:slot":{"auto":true}}`)
	var conns ifacestate.Connections
	err := json.Unmarshal(data, &conns)
	c.Assert(err, IsNil)
	c.Check(conns, DeepEquals, sampleConns)
}

func (s *stateSuite) TestLoadStoreLoad(c *C) {
	var conns, otherConns ifacestate.Connections

	err := conns.Load(s.state)
	c.Assert(err, IsNil)
	c.Check(conns, HasLen, 0)

	sampleConns.Store(s.state)

	otherConns.Load(s.state)
	c.Check(otherConns, DeepEquals, sampleConns)
}
