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

package devicemgmtstate_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicemgmtstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type mockStore struct {
	storetest.Store

	exchangeMessages func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error)
}

func (s *mockStore) ExchangeMessages(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
	return s.exchangeMessages(ctx, req)
}

type mockMessageHandler struct {
	validate      func(st *state.State, msg *devicemgmtstate.RequestMessage) error
	apply         func(st *state.State, msg *devicemgmtstate.RequestMessage) (string, error)
	buildResponse func(chg *state.Change) (map[string]any, asserts.MessageStatus)
}

func (h *mockMessageHandler) Validate(st *state.State, msg *devicemgmtstate.RequestMessage) error {
	return h.validate(st, msg)
}

func (h *mockMessageHandler) Apply(st *state.State, msg *devicemgmtstate.RequestMessage) (string, error) {
	return h.apply(st, msg)
}

func (h *mockMessageHandler) BuildResponse(chg *state.Change) (map[string]any, asserts.MessageStatus) {
	return h.buildResponse(chg)
}

type mockSigner struct {
	sign func(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error)
}

func (s *mockSigner) SignResponseMessage(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error) {
	return s.sign(accountID, messageID, status, body)
}

func setRemoteMgmtFeatureFlag(c *C, st *state.State, value any) {
	tr := config.NewTransaction(st)
	_, confOption := features.RemoteDeviceManagement.ConfigOption()
	err := tr.Set("core", confOption, value)
	c.Assert(err, IsNil)
	tr.Commit()
}

type deviceMgmtMgrSuite struct {
	testutil.BaseTest

	st         *state.State
	o          *overlord.Overlord
	storeStack *assertstest.StoreStack
	mgr        *devicemgmtstate.DeviceMgmtManager
	logbuf     *bytes.Buffer
}

var _ = Suite(&deviceMgmtMgrSuite{})

func (s *deviceMgmtMgrSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.o = overlord.Mock()
	s.st = s.o.State()

	s.st.Lock()
	defer s.st.Unlock()

	s.storeStack = assertstest.NewStoreStack("my-brand", nil)

	runner := s.o.TaskRunner()
	s.o.AddManager(runner)

	s.mgr = devicemgmtstate.Manager(s.st, runner, nil)
	s.o.AddManager(s.mgr)

	err := s.o.StartUp()
	c.Assert(err, IsNil)

	var restoreLogger func()
	s.logbuf, restoreLogger = logger.MockLogger()
	s.AddCleanup(restoreLogger)
}

func (s *deviceMgmtMgrSuite) mockModel() {
	as := assertstest.FakeAssertion(map[string]any{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"store":        "my-brand-store",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})

	deviceCtx := &snapstatetest.TrivialDeviceContext{DeviceModel: as.(*asserts.Model)}
	s.AddCleanup(snapstatetest.MockDeviceContext(deviceCtx))
	s.st.Set("seeded", true)
}

func (s *deviceMgmtMgrSuite) mockStore(exchangeMessages func(context.Context, *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error)) {
	snapstate.ReplaceStore(s.st, &mockStore{exchangeMessages: exchangeMessages})
}

func (s *deviceMgmtMgrSuite) TestShouldExchangeMessages(c *C) {
	type test struct {
		name             string
		flag             any
		lastExchangeTime time.Time
		readyResponses   map[string]store.Message
		expected         bool
	}

	wayback := time.Date(2025, 6, 14, 12, 0, 0, 0, time.UTC)
	restoreTime := devicemgmtstate.MockTimeNow(wayback)
	defer restoreTime()

	tooSoon := wayback.Add(-5 * time.Second)
	enoughTimePassed := wayback.Add(-2 * devicemgmtstate.DefaultExchangeInterval)

	tests := []test{
		{
			name:             "feature flag off, no responses, too soon",
			flag:             false,
			lastExchangeTime: tooSoon,
		},
		{
			name:             "feature flag off, no responses, enough time passed",
			flag:             false,
			lastExchangeTime: enoughTimePassed,
		},
		{
			name:             "feature flag off, has responses, too soon",
			flag:             false,
			lastExchangeTime: tooSoon,
			readyResponses:   map[string]store.Message{"mesg-1": {}},
		},
		{
			name:             "feature flag off, has responses, enough time passed",
			flag:             false,
			lastExchangeTime: enoughTimePassed,
			readyResponses:   map[string]store.Message{"mesg-1": {}},
			expected:         true,
		},
		{
			name:             "feature flag on, too soon",
			flag:             true,
			lastExchangeTime: tooSoon,
			expected:         false,
		},
		{
			name:             "feature flag on, enough time passed",
			flag:             true,
			lastExchangeTime: enoughTimePassed,
			expected:         true,
		},
		{
			name:             "feature flag check error, has responses, enough time passed",
			flag:             "banana",
			lastExchangeTime: enoughTimePassed,
			readyResponses:   map[string]store.Message{"mesg-1": {}},
			expected:         true,
		},
		{
			name:             "feature flag check error, no responses, enough time passed",
			flag:             "banana",
			lastExchangeTime: enoughTimePassed,
			expected:         false,
		},
	}

	s.st.Lock()
	defer s.st.Unlock()

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		ms := &devicemgmtstate.DeviceMgmtState{
			PendingRequests:  make(map[string]*devicemgmtstate.RequestMessage),
			Sequences:        devicemgmtstate.NewSequenceCache(),
			ReadyResponses:   tt.readyResponses,
			LastExchangeTime: tt.lastExchangeTime,
		}

		setRemoteMgmtFeatureFlag(c, s.st, tt.flag)

		exchange := s.mgr.ShouldExchangeMessages(ms)
		c.Check(exchange, Equals, tt.expected, cmt)
	}
}

func (s *deviceMgmtMgrSuite) TestEnsureOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.st.Unlock()
	err := s.mgr.Ensure()
	s.st.Lock()
	c.Assert(err, IsNil)

	changes := s.st.Changes()
	c.Assert(changes, HasLen, 1)
	chg := changes[0]
	c.Check(chg.Kind(), Equals, "device-management-exchange")
	c.Check(chg.Summary(), Equals, "Process device management messages")

	tasks := chg.Tasks()
	c.Check(tasks, HasLen, 2)

	exchange := tasks[0]
	c.Check(exchange.Kind(), Equals, "exchange-mgmt-messages")
	c.Check(exchange.Summary(), Equals, "Exchange messages with the Store")

	dispatch := tasks[1]
	c.Check(dispatch.Kind(), Equals, "dispatch-mgmt-messages")
	c.Check(dispatch.Summary(), Equals, "Dispatch message(s) to subsystems")
	c.Check(dispatch.WaitTasks(), DeepEquals, []*state.Task{exchange})
}

func (s *deviceMgmtMgrSuite) TestEnsureChangeAlreadyInFlight(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	setRemoteMgmtFeatureFlag(c, s.st, true)

	expired := time.Now().Add(-(devicemgmtstate.DefaultExchangeInterval + time.Minute))
	ms := &devicemgmtstate.DeviceMgmtState{
		PendingRequests:  make(map[string]*devicemgmtstate.RequestMessage),
		Sequences:        devicemgmtstate.NewSequenceCache(),
		ReadyResponses:   make(map[string]store.Message),
		LastExchangeTime: expired,
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("device-management-exchange", "Process device management messages")
	chg.SetStatus(state.DoingStatus)

	s.st.Unlock()
	err := s.mgr.Ensure()
	s.st.Lock()
	c.Assert(err, IsNil)

	changes := s.st.Changes()
	c.Assert(changes, HasLen, 1)
	c.Check(changes[0].ID(), Equals, chg.ID())
}

func (s *deviceMgmtMgrSuite) TestEnsureFeatureDisabled(c *C) {
	err := s.mgr.Ensure()
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	changes := s.st.Changes()
	c.Assert(changes, HasLen, 0)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesFetchOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Check(req.After, Equals, "")
		c.Check(req.Limit, Equals, devicemgmtstate.DefaultExchangeLimit)
		c.Check(req.Messages, HasLen, 0)

		oneHourAgo := time.Now().Add(-1 * time.Hour)
		tomorrow := oneHourAgo.Add(24 * time.Hour)

		body := []byte(`{"action": "get", "account": "my-brand", "view": "network/access-wifi"}`)
		as, err := s.storeStack.Sign(
			asserts.RequestMessageType,
			map[string]any{
				"authority-id": "my-brand",
				"account-id":   "my-brand",
				"message-id":   "someId",
				"message-kind": "confdb",
				"devices":      []any{"serial-1.my-model.my-brand"},
				"valid-since":  oneHourAgo.UTC().Format(time.RFC3339),
				"valid-until":  tomorrow.UTC().Format(time.RFC3339),
				"timestamp":    oneHourAgo.UTC().Format(time.RFC3339),
			},
			body, "",
		)
		c.Assert(err, IsNil)

		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				{
					Token: "token-123",
					Message: store.Message{
						Format: "assertion",
						Data:   string(asserts.Encode(as)),
					},
				},
			},
			TotalPendingMessages: 0,
		}, nil
	})

	setRemoteMgmtFeatureFlag(c, s.st, true)

	t := s.st.NewTask("exchange-mgmt-messages", "test exchange-mgmt-messages task")

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.LastReceivedToken, Equals, "token-123")
	c.Check(ms.LastExchangeTime.IsZero(), Equals, false)
	c.Assert(ms.PendingRequests, HasLen, 1)

	msg := ms.PendingRequests["someId"]
	c.Check(msg.BaseID, Equals, "someId")
	c.Check(msg.SeqNum, Equals, 0)
	c.Check(msg.AccountID, Equals, "my-brand")
	c.Check(msg.AuthorityID, Equals, "my-brand")
	c.Check(msg.Kind, Equals, "confdb")
	c.Check(msg.Devices, DeepEquals, []string{"serial-1.my-model.my-brand"})
	c.Check(msg.Body, Equals, `{"action": "get", "account": "my-brand", "view": "network/access-wifi"}`)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesReplyOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Check(req.After, Equals, "token-123")
		c.Check(req.Limit, Equals, 0)
		c.Check(req.Messages, HasLen, 1)

		return &store.MessageExchangeResponse{
			Messages:             []store.MessageWithToken{},
			TotalPendingMessages: 0,
		}, nil
	})

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingRequests:   make(map[string]*devicemgmtstate.RequestMessage),
		Sequences:         devicemgmtstate.NewSequenceCache(),
		LastReceivedToken: "token-123",
		ReadyResponses: map[string]store.Message{
			"someId": {Format: "assertion", Data: "response-data"},
		},
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("exchange-mgmt-messages", "test exchange-mgmt-messages task")

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.LastReceivedToken, Equals, "")
	c.Check(ms.ReadyResponses, HasLen, 0)
	c.Check(ms.PendingRequests, HasLen, 0)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesInvalidMessage(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				{
					Token: "token-123",
					Message: store.Message{
						Format: "assertion",
						Data:   "not-an-assertion",
					},
				},
			},
			TotalPendingMessages: 0,
		}, nil
	})

	setRemoteMgmtFeatureFlag(c, s.st, true)

	t := s.st.NewTask("exchange-mgmt-messages", "test exchange-mgmt-messages task")

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	c.Check(s.logbuf.String(), testutil.Contains, "cannot parse request-message with token token-123")

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.LastReceivedToken, Equals, "token-123")
	c.Check(ms.PendingRequests, HasLen, 0)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesDeviceNotSeeded(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.AddCleanup(snapstatetest.MockDeviceContext(nil))
	s.st.Set("seeded", false)

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Log("call not expected")
		c.Fail()

		return nil, fmt.Errorf("call not expected")
	})

	t := s.st.NewTask("exchange-mgmt-messages", "test exchange-mgmt-messages task")

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(
		err, ErrorMatches,
		"too early for operation, device not yet seeded or device model not acknowledged",
	)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesStoreError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return nil, fmt.Errorf("network timeout")
	})

	setRemoteMgmtFeatureFlag(c, s.st, true)

	t := s.st.NewTask("exchange-mgmt-messages", "test exchange-mgmt-messages task")

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, "network timeout")
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponse(c *C) {
	type test struct {
		name           string
		msg            func() *devicemgmtstate.RequestMessage
		buildResponse  func(chg *state.Change) (map[string]any, asserts.MessageStatus)
		expectedStatus asserts.MessageStatus
		expectedBody   map[string]any
	}

	tests := []test{
		{
			name: "success",
			msg: func() *devicemgmtstate.RequestMessage {
				change := s.st.NewChange("subsys-op", "subsystem operation")
				change.SetStatus(state.DoneStatus)

				return makeRequestMessage("mesg", 1, "test-kind", change.ID())
			},
			buildResponse: func(chg *state.Change) (map[string]any, asserts.MessageStatus) {
				return map[string]any{"result": "ok"}, asserts.MessageStatusSuccess
			},
			expectedStatus: asserts.MessageStatusSuccess,
			expectedBody:   map[string]any{"result": "ok"},
		},
		{
			name: "error",
			msg: func() *devicemgmtstate.RequestMessage {
				change := s.st.NewChange("subsys-op", "subsystem operation")
				change.SetStatus(state.ErrorStatus)

				return makeRequestMessage("mesg", 1, "test-kind", change.ID())
			},
			buildResponse: func(chg *state.Change) (map[string]any, asserts.MessageStatus) {
				return map[string]any{"error": "operation failed"}, asserts.MessageStatusError
			},
			expectedStatus: asserts.MessageStatusError,
			expectedBody:   map[string]any{"error": "operation failed"},
		},
		{
			name: "rejected",
			msg: func() *devicemgmtstate.RequestMessage {
				msg := makeRequestMessage("mesg", 1, "test-kind", "")
				msg.Status = asserts.MessageStatusRejected
				msg.Error = "invalid payload: missing required field"
				return msg
			},
			buildResponse: func(chg *state.Change) (map[string]any, asserts.MessageStatus) {
				c.Log("buildResponse call not expected")
				c.Fail()

				return nil, ""
			},
			expectedStatus: asserts.MessageStatusRejected,
			expectedBody:   map[string]any{"message": "invalid payload: missing required field"},
		},
	}

	s.st.Lock()
	defer s.st.Unlock()

	for _, tt := range tests {
		cmt := Commentf("%s status test", tt.name)

		msg := tt.msg()

		ms := &devicemgmtstate.DeviceMgmtState{
			PendingRequests: map[string]*devicemgmtstate.RequestMessage{"mesg-1": msg},
			Sequences:       devicemgmtstate.NewSequenceCache(),
			ReadyResponses:  make(map[string]store.Message),
		}
		s.mgr.SetState(ms)

		handler := &mockMessageHandler{
			buildResponse: tt.buildResponse,
		}
		s.mgr.MockHandler("test-kind", handler)

		signer := &mockSigner{
			sign: func(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error) {
				c.Check(status, Equals, tt.expectedStatus, cmt)
				var bodyMap map[string]any
				c.Assert(json.Unmarshal(body, &bodyMap), IsNil, cmt)
				c.Check(bodyMap, DeepEquals, tt.expectedBody, cmt)

				return assertstest.FakeAssertionWithBody(
					body,
					map[string]any{
						"type":        "response-message",
						"account-id":  accountID,
						"message-id":  messageID,
						"device":      "serial-1.my-model.my-brand",
						"status":      string(status),
						"body-length": fmt.Sprintf("%d", len(body)),
					},
				).(*asserts.ResponseMessage), nil
			},
		}
		s.mgr.MockSigner(signer)

		t := s.st.NewTask("queue-mgmt-response", "test queue-mgmt-response task")
		t.Set("id", "mesg-1")

		s.st.Unlock()
		err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
		s.st.Lock()
		c.Assert(err, IsNil)

		ms, err = s.mgr.GetState()
		c.Assert(err, IsNil)
		c.Check(ms.PendingRequests, HasLen, 0)
		c.Assert(ms.ReadyResponses, HasLen, 1)
		c.Check(ms.ReadyResponses["mesg-1"].Format, Equals, "assertion")
		c.Check(ms.Sequences.Applied["mesg"], Equals, 1)
	}
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseSubsystemChangeNotReady(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	subsysChg := s.st.NewChange("subsys-op", "subsystem operation")
	subsysChg.SetStatus(state.DoingStatus)

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingRequests: map[string]*devicemgmtstate.RequestMessage{
			"mesg-1": makeRequestMessage("mesg", 1, "test-kind", subsysChg.ID()),
		},
		Sequences:      devicemgmtstate.NewSequenceCache(),
		ReadyResponses: make(map[string]store.Message),
	}
	s.mgr.SetState(ms)

	handler := &mockMessageHandler{
		buildResponse: func(chg *state.Change) (map[string]any, asserts.MessageStatus) {
			c.Log("call not expected")
			c.Fail()

			return nil, ""
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	t := s.st.NewTask("queue-mgmt-response", "test queue-mgmt-response task")
	t.Set("id", "mesg-1")

	s.st.Unlock()
	err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, FitsTypeOf, &state.Retry{})
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseSubsystemChangeNotFound(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingRequests: map[string]*devicemgmtstate.RequestMessage{
			"mesg-1": makeRequestMessage("mesg", 1, "test-kind", "16384"),
		},
		Sequences:      devicemgmtstate.NewSequenceCache(),
		ReadyResponses: make(map[string]store.Message),
	}
	s.mgr.SetState(ms)

	handler := &mockMessageHandler{
		buildResponse: func(chg *state.Change) (map[string]any, asserts.MessageStatus) {
			c.Log("call not expected")
			c.Fail()

			return nil, ""
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	t := s.st.NewTask("queue-mgmt-response", "test queue-mgmt-response task")
	t.Set("id", "mesg-1")

	s.st.Unlock()
	err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, `cannot find subsystem change "\d+"`)
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseMessageNotFound(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	t := s.st.NewTask("queue-mgmt-response", "test queue-mgmt-response task")
	t.Set("id", "mesg-1")

	s.st.Unlock()
	err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, `cannot find message with id "mesg-1"`)
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseSigningError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &mockMessageHandler{
		buildResponse: func(chg *state.Change) (map[string]any, asserts.MessageStatus) {
			c.Log("call not expected")
			c.Fail()

			return nil, ""
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	signer := &mockSigner{
		sign: func(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error) {
			return nil, fmt.Errorf("signing key not available")
		},
	}
	s.mgr.MockSigner(signer)

	msg := makeRequestMessage("mesg", 1, "test-kind", "")
	msg.Status = asserts.MessageStatusRejected
	msg.Error = "cannot parse payload: missing required field"
	ms := &devicemgmtstate.DeviceMgmtState{
		PendingRequests: map[string]*devicemgmtstate.RequestMessage{"mesg-1": msg},
		Sequences:       devicemgmtstate.NewSequenceCache(),
		ReadyResponses:  make(map[string]store.Message),
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("queue-mgmt-response", "test queue-mgmt-response task")
	t.Set("id", "mesg-1")

	s.st.Unlock()
	err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, "cannot sign response message: signing key not available")
}

func (s *deviceMgmtMgrSuite) TestParseRequestMessageInvalid(c *C) {
	type test struct {
		name        string
		message     store.Message
		expectedErr string
	}

	tests := []test{
		{
			name: "unsupported format",
			message: store.Message{
				Format: "json",
				Data:   `{"some": "data"}`,
			},
			expectedErr: `cannot process assertion: unsupported format "json"`,
		},
		{
			name: "invalid assertion data",
			message: store.Message{
				Format: "assertion",
				Data:   "not-an-assertion",
			},
			expectedErr: `cannot decode assertion: assertion content/signature separator not found`,
		},
		{
			name: "wrong assertion type",
			message: store.Message{
				Format: "assertion",
				Data:   string(asserts.Encode(s.storeStack.TrustedKey)),
			},
			expectedErr: `cannot process assertion: expected "request-message" but got \"account-key\"`,
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		msg, err := devicemgmtstate.ParseRequestMessage(tt.message)
		c.Check(err, ErrorMatches, tt.expectedErr, cmt)
		c.Check(msg, IsNil, cmt)
	}
}

func makeRequestMessage(baseID string, seqNum int, kind string, changeID string) *devicemgmtstate.RequestMessage {
	wayback := time.Date(2025, 7, 29, 12, 0, 0, 0, time.UTC)

	return &devicemgmtstate.RequestMessage{
		AccountID:   "my-brand",
		AuthorityID: "my-brand",
		BaseID:      baseID,
		SeqNum:      seqNum,
		Kind:        kind,
		Devices:     []string{"serial-1.my-model.my-brand"},
		ValidSince:  wayback,
		ValidUntil:  wayback.Add(24 * time.Hour),
		Body:        `{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`,
		ReceiveTime: wayback.Add(6 * time.Hour),
		ChangeID:    changeID,
	}
}
