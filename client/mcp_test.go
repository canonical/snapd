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

package client_test

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/snapcore/snapd/client"
	. "gopkg.in/check.v1"
)

func (cs *clientSuite) TestClientMCP(c *C) {
	cs.rsp = `{"type":"sync","result":{"jsonrpc":"2.0","id":1,"result":{}}}`

	result, err := cs.cli.MCP(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	c.Assert(err, IsNil)
	c.Check(result, DeepEquals, client.MCPResult{
		Payload:     json.RawMessage(`{"jsonrpc":"2.0","id":1,"result":{}}`),
		HasResponse: true,
	})
	c.Check(cs.reqs, HasLen, 1)
	c.Check(cs.reqs[0].Method, Equals, "POST")
	c.Check(cs.reqs[0].URL.Path, Equals, "/v2/mcp")
	body, err := io.ReadAll(cs.reqs[0].Body)
	c.Assert(err, IsNil)
	c.Check(string(body), Equals, `{"jsonrpc":"2.0","id":1,"method":"ping"}`)
}

func (cs *clientSuite) TestClientMCPUpdatesWarningsSummary(c *C) {
	stamp := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	cs.rsp = `{"type":"sync","result":{"jsonrpc":"2.0","id":1,"result":{}},"warning-count":2,"warning-timestamp":"` + stamp.Format(time.RFC3339) + `"}`

	_, err := cs.cli.MCP(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	c.Assert(err, IsNil)

	count, gotStamp := cs.cli.WarningsSummary()
	c.Check(count, Equals, 2)
	c.Check(gotStamp, Equals, stamp)
}

func (cs *clientSuite) TestClientMCPReturnsDaemonError(c *C) {
	cs.status = 400
	cs.rsp = `{"type":"error","result":{"message":"boom"}}`

	result, err := cs.cli.MCP(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	c.Check(result, DeepEquals, client.MCPResult{})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Equals, "boom")
}

func (cs *clientSuite) TestClientMCPRejectsInvalidEnvelope(c *C) {
	cs.rsp = `{`

	result, err := cs.cli.MCP(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	c.Check(result, DeepEquals, client.MCPResult{})
	c.Assert(err, NotNil)
	c.Check(err.Error(), Matches, `cannot decode .*|cannot unmarshal .*|unexpected EOF`)
}

func (cs *clientSuite) TestClientMCPRejectsNonSyncResponse(c *C) {
	cs.rsp = `{"type":"async","result":{"jsonrpc":"2.0","id":1,"result":{}}}`

	result, err := cs.cli.MCP(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	c.Check(result, DeepEquals, client.MCPResult{})
	c.Assert(err, ErrorMatches, `expected sync response, got "async"`)
}

func (cs *clientSuite) TestClientMCPReturnsNoResponseForNotification(c *C) {
	cs.rsp = `{"type":"sync","result":null}`

	result, err := cs.cli.MCP(context.Background(), []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	c.Assert(err, IsNil)
	c.Check(result, DeepEquals, client.MCPResult{})
}

func (cs *clientSuite) TestClientMCPRejectsMissingSyncResult(c *C) {
	cs.rsp = `{"type":"sync"}`

	result, err := cs.cli.MCP(context.Background(), []byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	c.Check(result, DeepEquals, client.MCPResult{})
	c.Assert(err, ErrorMatches, `missing MCP response payload in sync result`)
}
