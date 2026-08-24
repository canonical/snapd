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
 * along with this program. If not, see <http://www.gnu.org/licenses/>.
 *
 */

package confdbstate_test

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/devicemgmtstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

var (
	testDeviceKey, _ = assertstest.GenerateKey(752)
)

type mockDeviceBackend struct {
	confdbControl func() (*asserts.ConfdbControl, error)
}

func (m *mockDeviceBackend) ConfdbControl() (*asserts.ConfdbControl, error) {
	return m.confdbControl()
}

func makeConfdbControl(c *C, groups []any) *asserts.ConfdbControl {
	a, err := asserts.SignWithoutAuthority(asserts.ConfdbControlType, map[string]any{
		"brand-id":  "my-brand",
		"model":     "my-model",
		"serial":    "serial-1",
		"revision":  "1",
		"groups":    groups,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, testDeviceKey)
	c.Assert(err, IsNil)

	return a.(*asserts.ConfdbControl)
}

type confdbHandlerSuite struct {
	testutil.BaseTest

	st     *state.State
	schema *confdb.Schema
}

var _ = Suite(&confdbHandlerSuite{})

func (s *confdbHandlerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.st = state.New(nil)

	views := map[string]any{
		"wifi-admin": map[string]any{
			"rules": []any{
				map[string]any{"request": "ssid", "storage": "v1.wifi.ssid"},
				map[string]any{"request": "password", "storage": "v1.wifi.password"},
			},
		},
	}

	var err error
	s.schema, err = confdb.NewSchema("system", "network", views, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
}

func (s *confdbHandlerSuite) TestValidateOK(c *C) {
	cc := makeConfdbControl(c, []any{
		map[string]any{
			"operators":       []any{"alice"},
			"authentications": []any{"operator-key"},
			"views":           []any{"system/network/wifi-admin"},
		},
	})
	handler := confdbstate.NewConfdbMessageHandler(&mockDeviceBackend{
		confdbControl: func() (*asserts.ConfdbControl, error) { return cc, nil },
	})

	msg := &devicemgmtstate.RequestMessage{
		AccountID: "alice",
		Kind:      "confdb",
		Body:      `{"action":"get","account":"system","view":"network/wifi-admin","constraints":{"iface":"wlan0"}}`,
	}
	err := handler.Validate(context.Background(), s.st, msg)
	c.Assert(err, IsNil)
}

func (s *confdbHandlerSuite) TestValidateUnauthorized(c *C) {
	cc := makeConfdbControl(c, []any{}) // no delegations

	handler := confdbstate.NewConfdbMessageHandler(&mockDeviceBackend{
		confdbControl: func() (*asserts.ConfdbControl, error) { return cc, nil },
	})

	msg := &devicemgmtstate.RequestMessage{
		AccountID: "alice",
		Kind:      "confdb",
		Body:      `{"action":"get","account":"system","view":"network/wifi-admin"}`,
	}
	err := handler.Validate(context.Background(), s.st, msg)
	c.Assert(err, NotNil)

	var authErr *devicemgmtstate.UnauthorizedError
	c.Assert(errors.As(err, &authErr), Equals, true)
	c.Check(authErr.Operator, Equals, "alice")
}

func (s *confdbHandlerSuite) TestValidateNoConfdbControl(c *C) {
	handler := confdbstate.NewConfdbMessageHandler(&mockDeviceBackend{
		confdbControl: func() (*asserts.ConfdbControl, error) {
			return nil, state.ErrNoState
		},
	})

	msg := &devicemgmtstate.RequestMessage{
		AccountID: "alice",
		Kind:      "confdb",
		Body:      `{"action":"get","account":"system","view":"network/wifi-admin"}`,
	}
	err := handler.Validate(context.Background(), s.st, msg)
	c.Assert(err, NotNil)

	var authErr *devicemgmtstate.UnauthorizedError
	c.Assert(errors.As(err, &authErr), Equals, true)
	c.Check(authErr.Operator, Equals, "alice")
}

func (s *confdbHandlerSuite) TestValidateInvalidBody(c *C) {
	cc := makeConfdbControl(c, []any{
		map[string]any{
			"operators":       []any{"alice"},
			"authentications": []any{"operator-key"},
			"views":           []any{"system/network/wifi-admin"},
		},
	})
	handler := confdbstate.NewConfdbMessageHandler(&mockDeviceBackend{
		confdbControl: func() (*asserts.ConfdbControl, error) { return cc, nil },
	})

	type test struct {
		name        string
		body        string
		expectedErr string
	}

	tests := []test{
		{
			name:        "invalid json",
			body:        `{not valid json...`,
			expectedErr: "cannot decode message body: .*",
		},
		{
			name:        "missing account",
			body:        `{"action":"get","account":"","view":"network/wifi-admin"}`,
			expectedErr: "cannot validate message: account is required",
		},
		{
			name:        "unknown action",
			body:        `{"action":"delete","account":"system","view":"network/wifi-admin"}`,
			expectedErr: `cannot validate message: unknown action "delete"`,
		},
		{
			name:        "invalid view",
			body:        `{"action":"get","account":"system","view":"network"}`,
			expectedErr: `cannot validate message: invalid view "network", expected <schema>/<view-name>`,
		},
		{
			name:        "view with too many segments",
			body:        `{"action":"get","account":"system","view":"foo/bar/baz"}`,
			expectedErr: `cannot validate message: invalid view "foo/bar/baz", expected <schema>/<view-name>`,
		},
		{
			name:        "set with no values",
			body:        `{"action":"set","account":"system","view":"network/wifi-admin"}`,
			expectedErr: "cannot validate message: body contains no values to write",
		},
		{
			name:        "set with empty values",
			body:        `{"action":"set","account":"system","view":"network/wifi-admin","values":{}}`,
			expectedErr: "cannot validate message: body contains no values to write",
		},
		{
			name:        "constraint with array value",
			body:        `{"action":"get","account":"system","view":"network/wifi-admin","constraints":{"iface":["wlan0","wlan1"]}}`,
			expectedErr: `cannot validate message: constraint value must be non-null scalar but parameter "iface" has array constraint`,
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		msg := &devicemgmtstate.RequestMessage{AccountID: "alice", Kind: "confdb", Body: tt.body}
		err := handler.Validate(context.Background(), s.st, msg)
		c.Assert(err, NotNil, cmt)
		c.Check(err, ErrorMatches, tt.expectedErr, cmt)

		var authErr *devicemgmtstate.UnauthorizedError
		c.Check(errors.As(err, &authErr), Equals, false, cmt)
	}
}

func (s *confdbHandlerSuite) TestApplyGetOK(c *C) {
	handler := &confdbstate.ConfdbMessageHandler{}

	restore := confdbstate.MockConfdbstateGetView(func(_ *state.State, account, schemaName, viewName string) (*confdb.View, error) {
		c.Check(account, Equals, "system")
		c.Check(schemaName, Equals, "network")
		c.Check(viewName, Equals, "wifi-admin")

		return s.schema.View(viewName), nil
	})
	defer restore()

	restore = confdbstate.MockConfdbstateReadConfdb(func(_ context.Context, st *state.State, view *confdb.View, requests []string, constraints map[string]any, _ confdb.Access) (string, error) {
		c.Check(view.Name, Equals, "wifi-admin")
		c.Check(requests, DeepEquals, []string{"ssid"})
		c.Check(constraints, DeepEquals, map[string]any{"iface": "wlan0"})

		chg := st.NewChange("get-confdb", "test change")
		return chg.ID(), nil
	})
	defer restore()

	msg := &devicemgmtstate.RequestMessage{
		BaseID: "msg-1",
		Kind:   "confdb",
		Body:   `{"action":"get","account":"system","view":"network/wifi-admin","keys":["ssid"],"constraints":{"iface":"wlan0"}}`,
	}
	s.st.Lock()
	defer s.st.Unlock()

	chgID, err := handler.Apply(context.Background(), s.st, msg)
	c.Assert(err, IsNil)
	c.Check(chgID, Not(Equals), "")

	chg := s.st.Change(chgID)
	c.Assert(chg, NotNil)
	var markedID string
	c.Assert(chg.Get("mgmt-message-id", &markedID), IsNil)
	c.Check(markedID, Equals, "msg-1")
}

func (s *confdbHandlerSuite) TestApplySetOK(c *C) {
	handler := &confdbstate.ConfdbMessageHandler{}

	restore := confdbstate.MockConfdbstateGetView(func(_ *state.State, _, _, viewName string) (*confdb.View, error) {
		return s.schema.View(viewName), nil
	})
	defer restore()

	restore = confdbstate.MockConfdbstateWriteConfdb(func(_ context.Context, st *state.State, view *confdb.View, values map[string]any) (string, error) {
		c.Check(view.Name, Equals, "wifi-admin")
		c.Check(values, DeepEquals, map[string]any{"ssid": "my-network"})

		chg := st.NewChange("set-confdb", "test change")
		return chg.ID(), nil
	})
	defer restore()

	msg := &devicemgmtstate.RequestMessage{
		BaseID: "msg-2",
		Kind:   "confdb",
		Body:   `{"action":"set","account":"system","view":"network/wifi-admin","values":{"ssid":"my-network"}}`,
	}
	s.st.Lock()
	defer s.st.Unlock()

	chgID, err := handler.Apply(context.Background(), s.st, msg)
	c.Assert(err, IsNil)
	c.Check(chgID, Not(Equals), "")

	chg := s.st.Change(chgID)
	c.Assert(chg, NotNil)
	var markedID string
	c.Assert(chg.Get("mgmt-message-id", &markedID), IsNil)
	c.Check(markedID, Equals, "msg-2")
}

func (s *confdbHandlerSuite) TestApplyInvalidBody(c *C) {
	handler := &confdbstate.ConfdbMessageHandler{}

	restore := confdbstate.MockConfdbstateGetView(func(_ *state.State, _, _, viewName string) (*confdb.View, error) {
		return s.schema.View(viewName), nil
	})
	defer restore()

	type test struct {
		name        string
		body        string
		expectedErr string
	}

	tests := []test{
		{
			name:        "invalid json",
			body:        `{not valid json...`,
			expectedErr: "cannot decode message body: .*",
		},
		{
			name:        "unknown action",
			body:        `{"action":"delete","account":"system","view":"network/wifi-admin"}`,
			expectedErr: `cannot apply message: unknown action "delete"`,
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		msg := &devicemgmtstate.RequestMessage{Kind: "confdb", Body: tt.body}

		chgID, err := handler.Apply(context.Background(), s.st, msg)
		c.Assert(err, NotNil, cmt)
		c.Check(err, ErrorMatches, tt.expectedErr, cmt)
		c.Check(chgID, Equals, "", cmt)
	}
}

func (s *confdbHandlerSuite) TestApplyGetViewError(c *C) {
	handler := &confdbstate.ConfdbMessageHandler{}

	restore := confdbstate.MockConfdbstateGetView(func(_ *state.State, _, _, _ string) (*confdb.View, error) {
		return nil, &confdbstate.NoViewError{}
	})
	defer restore()

	msg := &devicemgmtstate.RequestMessage{
		Kind: "confdb",
		Body: `{"action":"get","account":"system","view":"network/wifi-who"}`,
	}
	chgID, err := handler.Apply(context.Background(), s.st, msg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "cannot find view .* in confdb schema .*")
	c.Check(chgID, Equals, "")
}

func (s *confdbHandlerSuite) TestApplyWriteConfdbError(c *C) {
	handler := &confdbstate.ConfdbMessageHandler{}

	restore := confdbstate.MockConfdbstateGetView(func(_ *state.State, _, _, viewName string) (*confdb.View, error) {
		return s.schema.View(viewName), nil
	})
	defer restore()

	restore = confdbstate.MockConfdbstateWriteConfdb(func(_ context.Context, _ *state.State, _ *confdb.View, _ map[string]any) (string, error) {
		return "", fmt.Errorf("cannot write confdb")
	})
	defer restore()

	msg := &devicemgmtstate.RequestMessage{
		Kind: "confdb",
		Body: `{"action":"set","account":"system","view":"network/wifi-admin","values":{"ssid":"my-network"}}`,
	}
	chgID, err := handler.Apply(context.Background(), s.st, msg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "cannot write confdb")
	c.Check(chgID, Equals, "")
}

func (s *confdbHandlerSuite) TestResultFromChangeOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &confdbstate.ConfdbMessageHandler{}

	tests := []struct {
		name         string
		chgKind      string
		apiData      any
		expectedBody map[string]any
	}{
		{
			name:         "success",
			chgKind:      "get-confdb",
			apiData:      map[string]any{"values": map[string]any{"ssid": "my-network"}},
			expectedBody: map[string]any{"values": map[string]any{"ssid": "my-network"}},
		},
		{
			name:         "no api-data on set change",
			chgKind:      "set-confdb",
			expectedBody: map[string]any{},
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		chg := s.st.NewChange(tt.chgKind, "test change")
		chg.SetStatus(state.DoneStatus)
		if tt.apiData != nil {
			chg.Set("api-data", tt.apiData)
		}

		body, err := handler.ResultFromChange(context.Background(), chg)
		c.Assert(err, IsNil, cmt)
		c.Check(body, DeepEquals, tt.expectedBody, cmt)
	}
}

func (s *confdbHandlerSuite) TestResultFromChangeInvalid(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &confdbstate.ConfdbMessageHandler{}

	tests := []struct {
		name        string
		chgKind     string
		chgStatus   state.Status
		addTasks    func(st *state.State, chg *state.Change)
		apiData     any
		expectedErr string
	}{
		{
			name:        "no api-data on get change",
			chgKind:     "get-confdb",
			chgStatus:   state.DoneStatus,
			expectedErr: `internal error: "get-confdb" change \(\d+\) done with no api-data`,
		},
		{
			name:      "confdb error in api-data",
			chgKind:   "get-confdb",
			chgStatus: state.DoneStatus,
			apiData: map[string]any{"error": map[string]any{
				"kind":    "option-not-found",
				"message": "not found",
			}},
			expectedErr: "not found",
		},
		{
			name:        "api-data not a map",
			chgKind:     "get-confdb",
			chgStatus:   state.DoneStatus,
			apiData:     "ssid",
			expectedErr: ".*cannot unmarshal.*",
		},
		{
			name:        "api-data error field not a map",
			chgKind:     "get-confdb",
			chgStatus:   state.DoneStatus,
			apiData:     map[string]any{"error": `cannot find view "wifi-admin" in confdb schema system/network`},
			expectedErr: "internal error: api-data error field is not a map",
		},
		{
			name:        "api-data error field has no message",
			chgKind:     "get-confdb",
			chgStatus:   state.DoneStatus,
			apiData:     map[string]any{"error": map[string]any{"kind": "option-not-found"}},
			expectedErr: "internal error: api-data error field has no message",
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		chg := s.st.NewChange(tt.chgKind, "test change")
		if tt.addTasks != nil {
			tt.addTasks(s.st, chg)
		}
		if tt.chgStatus != state.DefaultStatus {
			chg.SetStatus(tt.chgStatus)
		}
		if tt.apiData != nil {
			chg.Set("api-data", tt.apiData)
		}

		body, err := handler.ResultFromChange(context.Background(), chg)
		c.Assert(err, NotNil, cmt)
		c.Check(err, ErrorMatches, tt.expectedErr, cmt)
		c.Check(body, IsNil, cmt)
	}
}
