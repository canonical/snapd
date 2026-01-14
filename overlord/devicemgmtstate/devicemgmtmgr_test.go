// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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
	validate      func(st *state.State, msg *devicemgmtstate.PendingMessage) error
	apply         func(st *state.State, msg *devicemgmtstate.PendingMessage) (string, error)
	buildResponse func(chg *state.Change) (map[string]any, asserts.MessageStatus)
}

func (h *mockMessageHandler) Validate(st *state.State, msg *devicemgmtstate.PendingMessage) error {
	return h.validate(st, msg)
}

func (h *mockMessageHandler) Apply(st *state.State, msg *devicemgmtstate.PendingMessage) (string, error) {
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

func (s *deviceMgmtMgrSuite) setFeatureFlag(c *C, value bool) {
	_, confOption := features.RemoteDeviceManagement.ConfigOption()

	tr := config.NewTransaction(s.st)
	err := tr.Set("core", confOption, value)
	c.Assert(err, IsNil)
	tr.Commit()
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
		name           string
		featureFlagOn  bool
		lastExchange   time.Time
		readyResponses map[string]store.Message
		expectedShould bool
		expectedLimit  int
	}

	wayback := time.Date(2025, 6, 14, 12, 0, 0, 0, time.UTC)
	restoreTime := devicemgmtstate.MockTimeNow(wayback)
	defer restoreTime()

	tooSoon := wayback.Add(-5 * time.Second)
	enoughTimePassed := wayback.Add(-2 * devicemgmtstate.DefaultExchangeInterval)

	tests := []test{
		{
			name:         "feature flag off, no responses, too soon",
			lastExchange: tooSoon,
		},
		{
			name:         "feature flag off, no responses, enough time passed",
			lastExchange: enoughTimePassed,
		},
		{
			name:           "feature flag off, has responses, too soon",
			lastExchange:   tooSoon,
			readyResponses: map[string]store.Message{"mesg-1": {}},
		},
		{
			name:           "feature flag off, has responses, enough time passed",
			lastExchange:   enoughTimePassed,
			readyResponses: map[string]store.Message{"mesg-1": {}},
			expectedShould: true,
		},
		{
			name:           "feature flag on, too soon",
			featureFlagOn:  true,
			lastExchange:   tooSoon,
			expectedShould: false,
		},
		{
			name:           "feature flag on, enough time passed",
			featureFlagOn:  true,
			lastExchange:   enoughTimePassed,
			expectedShould: true,
			expectedLimit:  devicemgmtstate.DefaultExchangeLimit,
		},
	}

	s.st.Lock()
	defer s.st.Unlock()

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		ms := &devicemgmtstate.DeviceMgmtState{
			LastExchange:   tt.lastExchange,
			ReadyResponses: tt.readyResponses,
		}

		s.setFeatureFlag(c, tt.featureFlagOn)

		should, cfg := s.mgr.ShouldExchangeMessages(ms)
		c.Check(should, Equals, tt.expectedShould, cmt)
		c.Check(cfg.Limit, Equals, tt.expectedLimit, cmt)
	}
}

func (s *deviceMgmtMgrSuite) TestEnsureOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.setFeatureFlag(c, true)

	s.st.Unlock()
	err := s.mgr.Ensure()
	s.st.Lock()
	c.Assert(err, IsNil)

	changes := s.st.Changes()
	c.Assert(changes, HasLen, 1)
	chg := changes[0]
	c.Check(chg.Kind(), Equals, "device-management-cycle")
	c.Check(chg.Summary(), Equals, "Process device management messages")

	tasks := chg.Tasks()
	c.Check(tasks, HasLen, 2)

	exchange := tasks[0]
	c.Check(exchange.Kind(), Equals, "exchange-messages")
	c.Check(exchange.Summary(), Equals, "Exchange messages with the Store")

	var cfg devicemgmtstate.ExchangeConfig
	err = exchange.Get("config", &cfg)
	c.Assert(err, IsNil)
	c.Check(cfg.Limit, Equals, devicemgmtstate.DefaultExchangeLimit)

	dispatch := tasks[1]
	c.Check(dispatch.Kind(), Equals, "dispatch-messages")
	c.Check(dispatch.Summary(), Equals, "Dispatch message(s) to subsystems")
	c.Check(dispatch.WaitTasks(), DeepEquals, []*state.Task{exchange})
}

func (s *deviceMgmtMgrSuite) TestEnsureChangeAlreadyInFlight(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.setFeatureFlag(c, true)

	ms := &devicemgmtstate.DeviceMgmtState{
		LastExchange: time.Now().Add(-10 * time.Minute),
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("device-management-cycle", "Process device management messages")
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
		ago := time.Now().Add(-1 * time.Hour)
		tomorrow := ago.Add(24 * time.Hour)

		body := []byte(`{"action": "get", "account": "my-brand", "view": "network/access-wifi"}`)
		as, err := s.storeStack.Sign(
			asserts.RequestMessageType,
			map[string]any{
				"authority-id": "my-brand",
				"account-id":   "my-brand",
				"message-id":   "someId",
				"message-kind": "confdb",
				"devices":      []any{"serial-1.my-model.my-brand"},
				"valid-since":  ago.UTC().Format(time.RFC3339),
				"valid-until":  tomorrow.UTC().Format(time.RFC3339),
				"timestamp":    ago.UTC().Format(time.RFC3339),
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

	t := s.st.NewTask("exchange-messages", "test exchange-messages task")
	cfg := devicemgmtstate.ExchangeConfig{Limit: 10}
	t.Set("config", &cfg)

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.PendingAckToken, Equals, "token-123")
	c.Check(ms.LastExchange.IsZero(), Equals, false)
	c.Assert(ms.PendingMessages, HasLen, 1)

	msg := ms.PendingMessages["someId"]
	c.Check(msg.BaseID, Equals, "someId")
	c.Check(msg.SeqNum, Equals, 0)
	c.Check(msg.AccountID, Equals, "my-brand")
	c.Check(msg.AuthorityID, Equals, "my-brand")
	c.Check(msg.Kind, Equals, "confdb")
	c.Check(msg.Source, Equals, "store")
	c.Check(msg.Devices, DeepEquals, []string{"serial-1.my-model.my-brand"})
	c.Check(msg.Body, Equals, `{"action": "get", "account": "my-brand", "view": "network/access-wifi"}`)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesReplyOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Check(req.After, Equals, "token-123")
		c.Check(req.Limit, Equals, 10)
		c.Check(req.Messages, HasLen, 1)

		return &store.MessageExchangeResponse{
			Messages:             []store.MessageWithToken{},
			TotalPendingMessages: 0,
		}, nil
	})

	ms := &devicemgmtstate.DeviceMgmtState{
		ReadyResponses: map[string]store.Message{
			"someId": {Format: "assertion", Data: "response-data"},
		},
		PendingAckToken: "token-123",
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("exchange-messages", "test exchange-messages task")
	cfg := devicemgmtstate.ExchangeConfig{Limit: 10}
	t.Set("config", &cfg)

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.PendingAckToken, Equals, "")
	c.Check(ms.ReadyResponses, HasLen, 0)
	c.Assert(ms.PendingMessages, HasLen, 0)
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

	t := s.st.NewTask("exchange-messages", "test exchange-messages task")
	cfg := devicemgmtstate.ExchangeConfig{Limit: 10}
	t.Set("config", &cfg)

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	c.Check(s.logbuf.String(), testutil.Contains, "cannot parse message with token token-123")

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.PendingAckToken, Equals, "token-123")
	c.Check(ms.PendingMessages, HasLen, 0)
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

	t := s.st.NewTask("exchange-messages", "test exchange-messages task")

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(
		err, ErrorMatches,
		"too early for operation, device not yet seeded or device model not acknowledged",
	)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesNoConfig(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Log("call not expected")
		c.Fail()

		return nil, fmt.Errorf("call not expected")
	})

	t := s.st.NewTask("exchange-messages", "test exchange-messages task")

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, `no state entry for key "config"`)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesStoreError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return nil, fmt.Errorf("network timeout")
	})

	t := s.st.NewTask("exchange-messages", "test exchange-messages task")
	cfg := devicemgmtstate.ExchangeConfig{Limit: 10}
	t.Set("config", &cfg)

	s.st.Unlock()
	err := s.mgr.DoExchangeMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, "network timeout")
}

func (s *deviceMgmtMgrSuite) TestDoDispatchMessagesOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"msg1": makePendingMessage("msg1", "confdb", 0, "16384"), // already dispatched
			"msg2": makePendingMessage("msg2", "confdb", 0, ""),
			"msg3": makePendingMessage("msg3", "confdb", 0, ""),
		},
		ReadyResponses: make(map[string]store.Message),
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	t := s.st.NewTask("dispatch-messages", "test dispatch-messages task")
	cfg := devicemgmtstate.ExchangeConfig{Limit: 10}
	t.Set("config", &cfg)
	chg.AddTask(t)

	s.st.Unlock()
	err := s.mgr.DoDispatchMessages(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 7) // existing dispatch-message + 3 for each message

	validate := tasks[1]
	c.Check(validate.Kind(), Equals, "validate-message")
	c.Check(validate.Summary(), Matches, `Validate message msg\d`)
	c.Check(validate.WaitTasks()[0].Kind(), Equals, "dispatch-messages")
	c.Check(validate.Lanes()[0], Equals, 1)

	apply := tasks[2]
	c.Check(apply.Kind(), Equals, "apply-message")
	c.Check(apply.Summary(), Matches, `Apply message msg\d`)
	c.Check(apply.WaitTasks()[0].Kind(), Equals, "validate-message")
	c.Check(apply.Lanes()[0], Equals, 1)

	queue := tasks[3]
	c.Check(queue.Kind(), Equals, "queue-response")
	c.Check(queue.Summary(), Matches, `Queue response for message msg\d`)
	c.Check(queue.WaitTasks()[0].Kind(), Equals, "apply-message")
	c.Check(queue.Lanes()[0], Equals, 1)
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &mockMessageHandler{
		validate: func(st *state.State, msg *devicemgmtstate.PendingMessage) error {
			return nil
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"msg1": makePendingMessage("msg1", "test-kind", 0, ""),
		},
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("validate-message", "test validate-message task")
	t.Set("id", "msg1")

	s.st.Unlock()
	err := s.mgr.DoValidateMessage(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.PendingMessages["msg1"].ValidationError, Equals, "")
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageSubsystemValidationFailed(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &mockMessageHandler{
		validate: func(st *state.State, msg *devicemgmtstate.PendingMessage) error {
			return fmt.Errorf("invalid payload: missing required field")
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"msg1": makePendingMessage("msg1", "test-kind", 0, ""),
		},
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("validate-message", "test validate-message task")
	t.Set("id", "msg1")

	s.st.Unlock()
	err := s.mgr.DoValidateMessage(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.PendingMessages["msg1"].ValidationError, Equals, "invalid payload: missing required field")
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageNoID(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	t := s.st.NewTask("validate-message", "test validate-message task")

	s.st.Unlock()
	err := s.mgr.DoValidateMessage(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, `no state entry for key "id"`)
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &mockMessageHandler{
		apply: func(st *state.State, msg *devicemgmtstate.PendingMessage) (string, error) {
			chg := st.NewChange("subsys-op", "subsystem operation")
			return chg.ID(), nil
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"msg1": makePendingMessage("msg1", "test-kind", 0, ""),
		},
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("apply-message", "test apply-message task")
	t.Set("id", "msg1")

	s.st.Unlock()
	err := s.mgr.DoApplyMessage(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	msg := ms.PendingMessages["msg1"]
	c.Check(msg.ChangeID, Not(Equals), "")
	c.Check(msg.ApplyError, Equals, "")
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageNoHandlerForKind(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"msg1": makePendingMessage("msg1", "unknown-kind", 0, ""),
		},
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("apply-message", "test apply-message task")
	t.Set("id", "msg1")

	s.st.Unlock()
	err := s.mgr.DoApplyMessage(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, `no handler registered for message kind "unknown-kind"`)
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageSkipIfValidationFailed(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	msg := makePendingMessage("msg1", "test-kind", 0, "")
	msg.ValidationError = "invalid payload: missing required field"
	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{"msg1": msg},
	}
	s.mgr.SetState(ms)

	handler := &mockMessageHandler{
		apply: func(st *state.State, msg *devicemgmtstate.PendingMessage) (string, error) {
			c.Log("call not expected")
			c.Fail()

			return "", fmt.Errorf("call not expected")
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	t := s.st.NewTask("apply-message", "test apply-message task")
	t.Set("id", "msg1")

	s.st.Unlock()
	err := s.mgr.DoApplyMessage(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageApplyFails(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	handler := &mockMessageHandler{
		apply: func(st *state.State, msg *devicemgmtstate.PendingMessage) (string, error) {
			return "", fmt.Errorf("system in inconsistent state")
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"msg1": makePendingMessage("msg1", "test-kind", 0, ""),
		},
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("apply-message", "test apply-message task")
	t.Set("id", "msg1")

	s.st.Unlock()
	err := s.mgr.DoApplyMessage(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	msg := ms.PendingMessages["msg1"]
	c.Check(msg.ChangeID, Equals, "")
	c.Check(msg.ApplyError, Equals, "cannot apply message: system in inconsistent state")
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseForFailureStatuses(c *C) {
	type test struct {
		name            string
		validationError string
		applyError      string
		expectedStatus  asserts.MessageStatus
	}

	handler := &mockMessageHandler{
		buildResponse: func(chg *state.Change) (map[string]any, asserts.MessageStatus) {
			c.Log("call not expected")
			c.Fail()

			return nil, ""
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	tests := []test{
		{
			name:            "validation error",
			validationError: "invalid payload: missing required field",
			expectedStatus:  asserts.MessageStatusRejected,
		},
		{
			name:           "apply error",
			applyError:     "cannot apply message: system in inconsistent state",
			expectedStatus: asserts.MessageStatusError,
		},
	}

	s.st.Lock()
	defer s.st.Unlock()

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		signer := &mockSigner{
			sign: func(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error) {
				c.Check(status, Equals, tt.expectedStatus, cmt)

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

		msg := makePendingMessage("mesg", "test-kind", 1, "")
		if tt.validationError != "" {
			msg.ValidationError = tt.validationError
		} else if tt.applyError != "" {
			msg.ApplyError = tt.applyError
		}

		ms := &devicemgmtstate.DeviceMgmtState{
			PendingMessages: map[string]*devicemgmtstate.PendingMessage{"mesg-1": msg},
			ReadyResponses:  make(map[string]store.Message),
		}
		s.mgr.SetState(ms)

		t := s.st.NewTask("queue-response", "test queue-response task")
		t.Set("id", "mesg-1")

		s.st.Unlock()
		err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
		s.st.Lock()
		c.Assert(err, IsNil, cmt)

		ms, err = s.mgr.GetState()
		c.Assert(err, IsNil, cmt)
		c.Check(ms.PendingMessages, HasLen, 0, cmt)
		c.Assert(ms.ReadyResponses, HasLen, 1, cmt)
		c.Check(ms.ReadyResponses["mesg-1"].Format, Equals, "assertion", cmt)
	}
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseForSuccessStatus(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	subsysChg := s.st.NewChange("subsys-op", "subsystem operation")
	subsysChg.SetStatus(state.DoneStatus)

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"mesg-1": makePendingMessage("mesg", "test-kind", 1, subsysChg.ID()),
		},
		ReadyResponses: make(map[string]store.Message),
	}
	s.mgr.SetState(ms)

	handler := &mockMessageHandler{
		buildResponse: func(chg *state.Change) (map[string]any, asserts.MessageStatus) {
			c.Check(chg.ID(), Equals, subsysChg.ID())
			return map[string]any{"result": "ok"}, asserts.MessageStatusSuccess
		},
	}
	s.mgr.MockHandler("test-kind", handler)

	signer := &mockSigner{
		sign: func(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error) {
			c.Check(status, Equals, asserts.MessageStatusSuccess)

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

	t := s.st.NewTask("queue-response", "test queue-response task")
	t.Set("id", "mesg-1")

	s.st.Unlock()
	err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.PendingMessages, HasLen, 0)
	c.Assert(ms.ReadyResponses, HasLen, 1)
	c.Check(ms.ReadyResponses["mesg-1"].Format, Equals, "assertion")
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseSubsystemChangeNotReady(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	subsysChg := s.st.NewChange("subsys-op", "subsystem operation")
	subsysChg.SetStatus(state.DoingStatus)

	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"mesg-1": makePendingMessage("mesg", "test-kind", 1, subsysChg.ID()),
		},
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

	t := s.st.NewTask("queue-response", "test queue-response task")
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
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{
			"mesg-1": makePendingMessage("mesg", "test-kind", 1, "16384"),
		},
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

	t := s.st.NewTask("queue-response", "test queue-response task")
	t.Set("id", "mesg-1")

	s.st.Unlock()
	err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, `subsystem change \d+ not found`)
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseMessageNotFound(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	t := s.st.NewTask("queue-message", "test queue-message task")
	t.Set("id", "mesg-1")

	s.st.Unlock()
	err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, "message mesg-1 not found in pending messages")
}

func (s *deviceMgmtMgrSuite) TestDoQueueResponseSignerError(c *C) {
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

	msg := makePendingMessage("mesg", "test-kind", 1, "")
	msg.ValidationError = "invalid payload: missing required field"
	ms := &devicemgmtstate.DeviceMgmtState{
		PendingMessages: map[string]*devicemgmtstate.PendingMessage{"mesg-1": msg},
		ReadyResponses:  make(map[string]store.Message),
	}
	s.mgr.SetState(ms)

	t := s.st.NewTask("queue-message", "test queue-message task")
	t.Set("id", "mesg-1")

	s.st.Unlock()
	err := s.mgr.DoQueueResponse(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, ErrorMatches, "cannot sign response: signing key not available")
}

func (s *deviceMgmtMgrSuite) TestParsePendingMessageInvalid(c *C) {
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
			expectedErr: "unsupported format json",
		},
		{
			name: "invalid assertion data",
			message: store.Message{
				Format: "assertion",
				Data:   "not-an-assertion",
			},
			expectedErr: "cannot decode assertion: assertion content/signature separator not found",
		},
		{
			name: "wrong assertion type",
			message: store.Message{
				Format: "assertion",
				Data:   string(asserts.Encode(s.storeStack.TrustedKey)),
			},
			expectedErr: `assertion is "account-key", expected "request-message"`,
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		msg, err := devicemgmtstate.ParsePendingMessage(tt.message)
		c.Check(err, ErrorMatches, tt.expectedErr, cmt)
		c.Check(msg, IsNil, cmt)
	}
}

func makePendingMessage(baseID, kind string, seqNum int, changeID string) *devicemgmtstate.PendingMessage {
	wayback := time.Date(2025, 7, 29, 12, 0, 0, 0, time.UTC)

	return &devicemgmtstate.PendingMessage{
		Source:      "store",
		BaseID:      baseID,
		SeqNum:      seqNum,
		Kind:        kind,
		AccountID:   "my-brand",
		AuthorityID: "my-brand",
		Devices:     []string{"serial-1.my-model.my-brand"},
		ValidSince:  wayback,
		ValidUntil:  wayback.Add(24 * time.Hour),
		Body:        `{"action": "get", "account": "my-brand", "view": "network/access-wifi"}`,
		Received:    wayback.Add(6 * time.Hour),
		ChangeID:    changeID,
	}
}
