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
	"github.com/snapcore/snapd/logger"
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
		Body:      `{"action":"get","account":"system","view":"network/wifi-admin"}`,
	}
	err := handler.Validate(s.st, msg)
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
	err := handler.Validate(s.st, msg)
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
	err := handler.Validate(s.st, msg)
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
			name:        "invalid view",
			body:        `{"action":"get","account":"system","view":"network"}`,
			expectedErr: `cannot validate message: view "system/network" must be in the format account/confdb/view`,
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		msg := &devicemgmtstate.RequestMessage{AccountID: "alice", Kind: "confdb", Body: tt.body}
		err := handler.Validate(s.st, msg)
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

	restore = confdbstate.MockConfdbstateReadConfdb(func(_ context.Context, _ *state.State, view *confdb.View, requests []string, _ map[string]any, _ confdb.Access) (string, error) {
		c.Check(view.Name, Equals, "wifi-admin")
		c.Check(requests, DeepEquals, []string{"ssid"})

		return "16384", nil
	})
	defer restore()

	msg := &devicemgmtstate.RequestMessage{
		Kind: "confdb",
		Body: `{"action":"get","account":"system","view":"network/wifi-admin","keys":["ssid"]}`,
	}
	chgID, err := handler.Apply(s.st, msg)
	c.Assert(err, IsNil)
	c.Check(chgID, Equals, "16384")
}

func (s *confdbHandlerSuite) TestApplySetOK(c *C) {
	handler := &confdbstate.ConfdbMessageHandler{}

	restore := confdbstate.MockConfdbstateGetView(func(_ *state.State, _, _, viewName string) (*confdb.View, error) {
		return s.schema.View(viewName), nil
	})
	defer restore()

	restore = confdbstate.MockConfdbstateWriteConfdb(func(_ context.Context, _ *state.State, view *confdb.View, values map[string]any) (string, error) {
		c.Check(view.Name, Equals, "wifi-admin")
		c.Check(values, DeepEquals, map[string]any{"ssid": "my-network"})

		return "16384", nil
	})
	defer restore()

	msg := &devicemgmtstate.RequestMessage{
		Kind: "confdb",
		Body: `{"action":"set","account":"system","view":"network/wifi-admin","values":{"ssid":"my-network"}}`,
	}
	chgID, err := handler.Apply(s.st, msg)
	c.Assert(err, IsNil)
	c.Check(chgID, Equals, "16384")
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
			name:        "invalid view",
			body:        `{"action":"get","account":"system","view":"network"}`,
			expectedErr: `cannot apply message: invalid view "network", expected <schema>/<view-name>`,
		},
		{
			name:        "view with too many segments",
			body:        `{"action":"get","account":"system","view":"foo/bar/baz"}`,
			expectedErr: `cannot apply message: invalid view "foo/bar/baz", expected <schema>/<view-name>`,
		},
		{
			name:        "unknown action",
			body:        `{"action":"delete","account":"system","view":"network/wifi-admin"}`,
			expectedErr: `cannot apply message: unknown action "delete"`,
		},
		{
			name:        "set with no values field",
			body:        `{"action":"set","account":"system","view":"network/wifi-admin"}`,
			expectedErr: "cannot apply message: body contains no values to write",
		},
		{
			name:        "set with empty values",
			body:        `{"action":"set","account":"system","view":"network/wifi-admin","values":{}}`,
			expectedErr: "cannot apply message: body contains no values to write",
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		msg := &devicemgmtstate.RequestMessage{Kind: "confdb", Body: tt.body}

		chgID, err := handler.Apply(s.st, msg)
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
	chgID, err := handler.Apply(s.st, msg)
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
	chgID, err := handler.Apply(s.st, msg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "cannot write confdb")
	c.Check(chgID, Equals, "")
}

func (s *confdbHandlerSuite) TestResultFromChangeSuccess(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &confdbstate.ConfdbMessageHandler{}

	chg := s.st.NewChange("get-confdb", "test change")
	chg.SetStatus(state.DoneStatus)

	apiData := map[string]any{"values": map[string]any{"ssid": "my-network"}}
	chg.Set("api-data", apiData)

	body, err := handler.ResultFromChange(chg)
	c.Assert(err, IsNil)
	c.Check(body, DeepEquals, apiData)
}

func (s *confdbHandlerSuite) TestResultFromChangeError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &confdbstate.ConfdbMessageHandler{}

	chg := s.st.NewChange("get-confdb", "test change")
	t := s.st.NewTask("load-confdb-change", "Load confdb data into the change")
	chg.AddTask(t)
	t.SetStatus(state.ErrorStatus)
	t.Errorf("cannot read view")

	body, err := handler.ResultFromChange(chg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, ""+
		"cannot perform the following tasks:\n"+
		"- Load confdb data into the change \\(cannot read view\\)")
	c.Check(body, IsNil)
}

func (s *confdbHandlerSuite) TestResultFromChangeNotDone(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	handler := &confdbstate.ConfdbMessageHandler{}

	chg := s.st.NewChange("get-confdb", "test change")
	chg.SetStatus(state.DoingStatus)

	body, err := handler.ResultFromChange(chg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `internal: unexpected change status Doing`)
	c.Check(body, IsNil)
	c.Check(logbuf.String(), testutil.Contains, "internal: ResultFromChange called on change in unexpected status Doing")
}

func (s *confdbHandlerSuite) TestResultFromChangeNoApiData(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &confdbstate.ConfdbMessageHandler{}

	chg := s.st.NewChange("set-confdb", "test change")
	chg.SetStatus(state.DoneStatus)

	body, err := handler.ResultFromChange(chg)
	c.Assert(err, IsNil)
	c.Check(body, DeepEquals, map[string]any{})
}

func (s *confdbHandlerSuite) TestResultFromChangeNoApiDataOnGetChange(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	handler := &confdbstate.ConfdbMessageHandler{}

	chg := s.st.NewChange("get-confdb", "test change")
	chg.SetStatus(state.DoneStatus)

	body, err := handler.ResultFromChange(chg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, `internal: change "get-confdb" done with no api-data`)
	c.Check(body, IsNil)
	c.Check(logbuf.String(), testutil.Contains, `internal: change "get-confdb" done with no api-data`)
}

func (s *confdbHandlerSuite) TestResultFromChangeConfdbError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &confdbstate.ConfdbMessageHandler{}

	chg := s.st.NewChange("get-confdb", "test change")
	chg.SetStatus(state.DoneStatus)

	errBody := map[string]any{
		"kind":    "option-not-found",
		"message": "not found",
	}
	chg.Set("api-data", map[string]any{"error": errBody})

	body, err := handler.ResultFromChange(chg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "not found")
	c.Check(body, IsNil)
}

func (s *confdbHandlerSuite) TestResultFromChangeApiDataNotAMap(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &confdbstate.ConfdbMessageHandler{}

	chg := s.st.NewChange("get-confdb", "test change")
	chg.SetStatus(state.DoneStatus)

	chg.Set("api-data", "ssid")

	body, err := handler.ResultFromChange(chg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, ".*cannot unmarshal.*")
	c.Check(body, IsNil)
}

func (s *confdbHandlerSuite) TestResultFromChangeApiDataErrorFieldNotAMap(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	handler := &confdbstate.ConfdbMessageHandler{}

	chg := s.st.NewChange("get-confdb", "test change")
	chg.SetStatus(state.DoneStatus)

	chg.Set("api-data", map[string]any{"error": `cannot find view "wifi-admin" in confdb schema system/network`})

	body, err := handler.ResultFromChange(chg)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "internal: api-data error field is not a map")
	c.Check(body, IsNil)
	c.Check(logbuf.String(), testutil.Contains, "internal: api-data error field is not a map")
}
