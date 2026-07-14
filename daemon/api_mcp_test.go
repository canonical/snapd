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

package daemon_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type failingReadCloser struct{}

func (failingReadCloser) Read(p []byte) (int, error) {
	return 0, errors.New("boom")
}

func (failingReadCloser) Close() error {
	return nil
}

type mcpSuite struct {
	apiBaseSuite
}

var _ = Suite(&mcpSuite{})

func (s *mcpSuite) setFeatureFlag(c *C, st *state.State) {
	_, confOption := features.MCP.ConfigOption()

	st.Lock()
	defer st.Unlock()

	tr := config.NewTransaction(st)
	err := tr.Set("core", confOption, true)
	c.Assert(err, IsNil)
	tr.Commit()
}

func (s *mcpSuite) TestPostMCPReturnsProcessorResponse(c *C) {
	d := s.daemon(c)
	st := d.Overlord().State()
	s.setFeatureFlag(c, st)

	// Add a snap to the state
	st.Lock()
	snapst := &snapstate.SnapState{
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "test-snap", Revision: snap.R(1)},
		}),
		Current: snap.R(1),
		Active:  true,
	}
	snapstate.Set(st, "test-snap", snapst)
	st.Unlock()

	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.mcp"})

	req := httptest.NewRequest("POST", "/v2/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	rec := httptest.NewRecorder()

	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.Body.String(), Matches, `(?s).*"type":"sync".*"result":\{.*\}.*`)

	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, daemon.ResponseTypeSync)
	result := rsp.Result.(map[string]any)
	c.Check(result["jsonrpc"], Equals, "2.0")
	c.Check(result["id"], Equals, float64(1))
	c.Check(result["result"], DeepEquals, map[string]any{})
}

func (s *mcpSuite) TestPostMCPWithInvalidJSONReturnsParseError(c *C) {
	d := s.daemon(c)
	s.setFeatureFlag(c, d.Overlord().State())
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.mcp"})

	req := httptest.NewRequest("POST", "/v2/mcp", bytes.NewBufferString(`{`))
	rec := httptest.NewRecorder()

	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 200)

	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	result := rsp.Result.(map[string]any)
	errorObj := result["error"].(map[string]any)
	c.Check(errorObj["code"], Equals, float64(-32700))
}

func (s *mcpSuite) TestPostMCPRejectsOversizedRequest(c *C) {
	d := s.daemon(c)
	s.setFeatureFlag(c, d.Overlord().State())
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.mcp"})

	oversized := strings.Repeat("x", 1024*1024+1)
	req := httptest.NewRequest("POST", "/v2/mcp", bytes.NewBufferString(oversized))
	rec := httptest.NewRecorder()

	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 400)

	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, daemon.ResponseTypeError)
	errResult := rsp.Result.(map[string]any)
	c.Check(errResult["message"], Matches, "request body too large.*")
}

func (s *mcpSuite) TestPostMCPFeatureFlagDisabled(c *C) {
	s.daemon(c)
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.mcp"})

	req := httptest.NewRequest("POST", "/v2/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	rec := httptest.NewRecorder()

	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 400)

	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, daemon.ResponseTypeError)
	errResult := rsp.Result.(map[string]any)
	c.Check(errResult["message"], Equals, `feature flag "mcp" is disabled, enable with: snap set system experimental.mcp=true`)
}

func (s *mcpSuite) TestPostMCPNotificationReturnsNilResult(c *C) {
	d := s.daemon(c)
	s.setFeatureFlag(c, d.Overlord().State())
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.mcp"})

	req := httptest.NewRequest("POST", "/v2/mcp", bytes.NewBufferString(`{"jsonrpc":"2.0","method":"notifications/initialized"}`))
	rec := httptest.NewRecorder()

	s.req(c, req, nil, actionIsExpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 200)
	c.Check(rec.Body.String(), Matches, `(?s).*"type":"sync".*"result":null.*`)

	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Result, IsNil)
}

func (s *mcpSuite) TestPostMCPReadBodyError(c *C) {
	d := s.daemon(c)
	s.setFeatureFlag(c, d.Overlord().State())
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.mcp"})

	req := httptest.NewRequest("POST", "/v2/mcp", io.NopCloser(strings.NewReader("")))
	req.Body = failingReadCloser{}
	rec := httptest.NewRecorder()

	s.req(c, req, nil, actionIsUnexpected).ServeHTTP(rec, nil)
	c.Check(rec.Code, Equals, 400)

	var rsp daemon.RespJSON
	c.Assert(json.Unmarshal(rec.Body.Bytes(), &rsp), IsNil)
	c.Check(rsp.Type, Equals, daemon.ResponseTypeError)
	errResult := rsp.Result.(map[string]any)
	c.Check(errResult["message"], Matches, `cannot read request body: boom`)
}
