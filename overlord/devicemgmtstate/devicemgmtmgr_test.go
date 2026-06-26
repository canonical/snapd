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
	"fmt"
	"sort"
	"strconv"
	"strings"
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
	"github.com/snapcore/snapd/overlord/assertstate"
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

var noopTask = func(*state.Task, *tomb.Tomb) error { return nil }

type mockStore struct {
	storetest.Store

	exchangeMessages func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error)
}

func (s *mockStore) ExchangeMessages(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
	return s.exchangeMessages(ctx, req)
}

type mockDeviceBackend struct {
	serial *asserts.Serial
	sign   func(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error)
}

func (m *mockDeviceBackend) Serial() (*asserts.Serial, error) {
	return m.serial, nil
}

func (m *mockDeviceBackend) SignResponseMessage(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error) {
	return m.sign(accountID, messageID, status, body)
}

type mockMessageHandler struct {
	validate         func(st *state.State, msg *devicemgmtstate.RequestMessage) error
	apply            func(st *state.State, msg *devicemgmtstate.RequestMessage) (string, error)
	resultFromChange func(chg *state.Change) (map[string]any, error)
}

func (h *mockMessageHandler) Validate(st *state.State, msg *devicemgmtstate.RequestMessage) error {
	if h.validate != nil {
		return h.validate(st, msg)
	}

	return nil
}

func (h *mockMessageHandler) Apply(st *state.State, msg *devicemgmtstate.RequestMessage) (string, error) {
	if h.apply != nil {
		return h.apply(st, msg)
	}

	return "", nil
}

func (h *mockMessageHandler) ResultFromChange(chg *state.Change) (map[string]any, error) {
	if h.resultFromChange != nil {
		return h.resultFromChange(chg)
	}

	return nil, nil
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
	runner     *state.TaskRunner
	storeStack *assertstest.StoreStack
	mgr        *devicemgmtstate.DeviceMgmtManager
	logbuf     *bytes.Buffer
}

var _ = Suite(&deviceMgmtMgrSuite{})

var fixedTestTime = time.Date(2025, 6, 14, 12, 0, 0, 0, time.UTC)

func (s *deviceMgmtMgrSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime))
	s.AddCleanup(devicemgmtstate.MockFetchAccountKeys(func(_ *state.State, _ int, _ []string) error {
		return nil
	}))

	s.o = overlord.Mock()
	s.st = s.o.State()

	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()
	s.storeStack = assertstest.NewStoreStack("my-brand", nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeStack.Trusted,
	})
	c.Assert(err, IsNil)
	c.Assert(db.Add(s.storeStack.StoreAccountKey("")), IsNil)
	assertstate.ReplaceDB(s.st, db)

	s.runner = s.o.TaskRunner()
	s.o.AddManager(s.runner)

	s.mgr = devicemgmtstate.Manager(s.st, s.runner, nil)
	s.o.AddManager(s.mgr)

	s.mgr.MockBackend(&mockDeviceBackend{serial: s.makeSerial(c, "serial-1")})

	err = s.o.StartUp()
	c.Assert(err, IsNil)

	var restoreLogger func()
	s.logbuf, restoreLogger = logger.MockLogger()
	s.AddCleanup(restoreLogger)

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mgr.RegisterHandler("test-kind", &mockMessageHandler{
		validate: func(*state.State, *devicemgmtstate.RequestMessage) error {
			return nil
		},
		apply: func(st *state.State, msg *devicemgmtstate.RequestMessage) (string, error) {
			chg := st.NewChange("subsystem", "apply payload")
			devicemgmtstate.MarkChangeForMessage(chg, msg)
			return chg.ID(), nil
		},
		resultFromChange: func(*state.Change) (map[string]any, error) {
			return map[string]any{"key": "value"}, nil
		},
	})
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

func (s *deviceMgmtMgrSuite) makeSerial(c *C, serial string) *asserts.Serial {
	devKey, _ := assertstest.GenerateKey(752)
	encDevKey, err := asserts.EncodePublicKey(devKey.PublicKey())
	c.Assert(err, IsNil)

	as, err := s.storeStack.Sign(asserts.SerialType, map[string]any{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              serial,
		"device-key":          string(encDevKey),
		"device-key-sha3-384": devKey.PublicKey().ID(),
		"timestamp":           fixedTestTime.UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	return as.(*asserts.Serial)
}

func (s *deviceMgmtMgrSuite) mockStore(exchangeMessages func(context.Context, *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error)) {
	snapstate.ReplaceStore(s.st, &mockStore{exchangeMessages: exchangeMessages})
}

func (s *deviceMgmtMgrSuite) makeStoreRequestMessage(c *C, messageID, kind, token string) store.MessageWithToken {
	oneHourAgo := fixedTestTime.Add(-time.Hour)
	tomorrow := oneHourAgo.Add(24 * time.Hour)
	body := []byte(`{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`)
	as, err := s.storeStack.Sign(
		asserts.RequestMessageType,
		map[string]any{
			"authority-id": "my-brand",
			"account-id":   "my-brand",
			"message-id":   messageID,
			"message-kind": kind,
			"devices":      []any{"serial-1.my-model.my-brand"},
			"valid-since":  oneHourAgo.UTC().Format(time.RFC3339),
			"valid-until":  tomorrow.UTC().Format(time.RFC3339),
			"timestamp":    oneHourAgo.UTC().Format(time.RFC3339),
		},
		body, "",
	)
	c.Assert(err, IsNil)

	return store.MessageWithToken{
		Token: token,
		Message: store.Message{
			Format: "assertion",
			Data:   string(asserts.Encode(as)),
		},
	}
}

func (s *deviceMgmtMgrSuite) settle(c *C) {
	s.st.Unlock()
	defer s.st.Lock()

	err := s.o.Settle(testutil.HostScaledTimeout(5 * time.Second))
	c.Assert(err, IsNil)
}

func (s *deviceMgmtMgrSuite) TestShouldExchangeMessages(c *C) {
	type test struct {
		name             string
		flag             any
		lastExchangeTime time.Time
		readyResponses   map[string]store.Message
		expected         bool
	}

	tooSoon := fixedTestTime.Add(-5 * time.Second)
	enoughTimePassed := fixedTestTime.Add(-2 * devicemgmtstate.DefaultExchangeInterval)

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
			Sequences:        make(map[string]*devicemgmtstate.SequenceState),
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

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{}, nil
	})

	s.settle(c)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 1)

	chg := changes[0]
	c.Check(chg.Summary(), Equals, "Process device management messages")

	tasks := chg.Tasks()
	c.Assert(tasks, HasLen, 2)

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

	expired := fixedTestTime.Add(-2 * devicemgmtstate.DefaultExchangeInterval)
	ms := &devicemgmtstate.DeviceMgmtState{
		LastExchangeTime: expired,
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("device-management-exchange", "Process device management messages")
	chg.SetStatus(state.DoingStatus)

	s.settle(c)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 1)
	c.Check(changes[0].ID(), Equals, chg.ID())
}

func (s *deviceMgmtMgrSuite) TestEnsureFeatureDisabled(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	setRemoteMgmtFeatureFlag(c, s.st, false)

	s.settle(c)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 0)
}

func (s *deviceMgmtMgrSuite) TestEnsureFeatureDisabledWithReadyResponses(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	setRemoteMgmtFeatureFlag(c, s.st, false)

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Check(req.Limit, Equals, 0)
		c.Check(req.Messages, HasLen, 1)

		return &store.MessageExchangeResponse{}, nil
	})

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: make(map[string]*devicemgmtstate.SequenceState),
		ReadyResponses: map[string]store.Message{
			"someId": {Format: "assertion", Data: "response-data"},
		},
	}
	s.mgr.SetState(ms)

	s.settle(c)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 1)
	c.Check(changes[0].Err(), IsNil)

	c.Assert(changes[0].Tasks(), HasLen, 2)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.LastReceivedToken, Equals, "")
	c.Check(ms.ReadyResponses, HasLen, 0)
	c.Check(ms.Sequences, HasLen, 0)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesFetchOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Check(req.After, Equals, "")
		c.Check(req.Limit, Equals, devicemgmtstate.DefaultExchangeLimit)
		c.Check(req.Messages, HasLen, 0)

		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "someId", "test-kind", "token-123"),
			},
			TotalPendingMessages: 0,
		}, nil
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.LastReceivedToken, Equals, "token-123")
	c.Check(ms.LastExchangeTime.IsZero(), Equals, false)
	c.Assert(ms.Sequences, HasLen, 1)
	c.Assert(ms.Sequences["someId"].Messages, HasLen, 1)

	msg := ms.Sequences["someId"].Messages[0]
	c.Check(msg.BaseID, Equals, "someId")
	c.Check(msg.SeqNum, Equals, 0)
	c.Check(msg.AccountID, Equals, "my-brand")
	c.Check(msg.AuthorityID, Equals, "my-brand")
	c.Check(msg.Kind, Equals, "test-kind")
	c.Check(msg.Devices, DeepEquals, []string{"serial-1.my-model.my-brand"})
	c.Check(msg.Body, Equals, `{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesReplyOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

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
		Sequences:         make(map[string]*devicemgmtstate.SequenceState),
		LastReceivedToken: "token-123",
		ReadyResponses: map[string]store.Message{
			"someId": {Format: "assertion", Data: "response-data"},
		},
	}
	s.mgr.SetState(ms)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.LastReceivedToken, Equals, "")
	c.Check(ms.ReadyResponses, HasLen, 0)
	c.Check(ms.Sequences, HasLen, 0)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesSequenceLRU(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "seqA-1", "test-kind", "token-seqA-1"),
				s.makeStoreRequestMessage(c, "seqB-1", "test-kind", "token-seqB-1"),
				s.makeStoreRequestMessage(c, "uns7", "test-kind", "token-uns1"),
				s.makeStoreRequestMessage(c, "seqB-2", "test-kind", "token-seqB-2"),
				s.makeStoreRequestMessage(c, "seqC-1", "test-kind", "token-seqC-1"),
				s.makeStoreRequestMessage(c, "seqA-2", "test-kind", "token-seqA-2"),
				s.makeStoreRequestMessage(c, "uns8", "test-kind", "token-uns2"),
			},
		}, nil
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	// seqA's second touch moves it after seqC, leaving seqB least recently used.
	c.Check(ms.SequenceLRU, DeepEquals, []string{"seqB", "seqC", "seqA"})
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesInvalidMessage(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

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

	s.settle(c)

	c.Check(s.logbuf.String(), testutil.Contains, "cannot parse request-message with token token-123")

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.LastReceivedToken, Equals, "token-123")
	c.Check(ms.Sequences, HasLen, 0)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesDuplicateMessage(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		msg := s.makeStoreRequestMessage(c, "someId", "test-kind", "token-1")
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				msg,
				{Token: "token-2", Message: msg.Message},
			},
			TotalPendingMessages: 0,
		}, nil
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	// The duplicate should have been dropped, leaving one message in the sequence.
	c.Assert(ms.Sequences["someId"].Messages, HasLen, 1)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesDeviceNotSeeded(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.AddCleanup(snapstatetest.MockDeviceContext(nil))
	s.st.Set("seeded", false)

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Fatal("call not expected")

		return nil, fmt.Errorf("call not expected")
	})

	s.settle(c)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 1)
	c.Assert(
		changes[0].Err(), ErrorMatches,
		"(?s).*too early for operation, device not yet seeded or device model not acknowledged.*",
	)
	c.Assert(changes[0].Tasks(), HasLen, 2)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesStoreError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return nil, fmt.Errorf("network timeout")
	})

	s.settle(c)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 1)
	c.Assert(changes[0].Err(), ErrorMatches, "(?s).*network timeout.*")
	c.Assert(changes[0].Tasks(), HasLen, 2)
}

func (s *deviceMgmtMgrSuite) TestDoExchangeMessagesIdempotent(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "someId", "test-kind", "token-1"),
			},
		}, nil
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Assert(ms.Sequences["someId"].Messages, HasLen, 1)

	// Advance time past the exchange interval to trigger a second exchange.
	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime.Add(2 * devicemgmtstate.DefaultExchangeInterval)))

	s.settle(c)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Assert(ms.Sequences["someId"].Messages, HasLen, 1)
}

func (s *deviceMgmtMgrSuite) TestDoDispatchMessagesUnsequenced(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	// Exchange 1: receive msg1 only so it gets dispatched.
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.settle(c)

	// Exchange 2: msg1 is dedup'd by exchange; msg2 and msg3 are new.
	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime.Add(2 * devicemgmtstate.DefaultExchangeInterval)))

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
				s.makeStoreRequestMessage(c, "msg2", "test-kind", "token-2"),
				s.makeStoreRequestMessage(c, "msg3", "test-kind", "token-3"),
			},
		}, nil
	})

	s.settle(c)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 2)

	ti := buildTaskIndex(changes[1])
	assertMessagesDispatched(c, ti, []string{"msg2", "msg3"}, "unsequenced")
	assertMessagesNotDispatched(c, ti, []string{"msg1"}, "unsequenced")

	waitOn := map[string]string{"msg2": "<dispatch>", "msg3": "<dispatch>"}
	assertMessagesWaitOn(c, ti, waitOn, "unsequenced")
}

func (s *deviceMgmtMgrSuite) TestDoDispatchMessagesSequenced(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	makeRequestMessage := func(messageID, kind string, dispatched bool) *devicemgmtstate.RequestMessage {
		baseID, seqStr, hasSeq := strings.Cut(messageID, "-")
		seqNum := 0
		if hasSeq {
			seqNum, _ = strconv.Atoi(seqStr)
		}

		return &devicemgmtstate.RequestMessage{
			AccountID:   "my-brand",
			AuthorityID: "my-brand",
			BaseID:      baseID,
			SeqNum:      seqNum,
			Kind:        kind,
			Devices:     []string{"serial-1.my-model.my-brand"},
			ValidSince:  fixedTestTime,
			ValidUntil:  fixedTestTime.Add(24 * time.Hour),
			Body:        `{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`,
			ReceiveTime: fixedTestTime.Add(6 * time.Hour),
			Dispatched:  dispatched,
		}
	}

	type test struct {
		name            string
		sequences       map[string]int // last applied message per sequence
		pendingRequests []*devicemgmtstate.RequestMessage
		expectedChain   map[string]string
	}

	tests := []test{
		{
			name: "consecutive from start",
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("seqA-1", "test-kind", false),
				makeRequestMessage("seqA-2", "test-kind", false),
				makeRequestMessage("seqA-3", "test-kind", false),
			},
			expectedChain: map[string]string{
				"seqA-1": "<dispatch>",
				"seqA-2": "seqA-1",
				"seqA-3": "seqA-2",
			},
		},
		{
			name: "gap stops chaining",
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("seqA-1", "test-kind", false),
				makeRequestMessage("seqA-2", "test-kind", false),
				makeRequestMessage("seqA-4", "test-kind", false), // 3 is missing
				makeRequestMessage("seqA-5", "test-kind", false),
			},
			expectedChain: map[string]string{
				"seqA-1": "<dispatch>",
				"seqA-2": "seqA-1",
			},
		},
		{
			name:      "resume from last message applied",
			sequences: map[string]int{"seqA": 2},
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("seqA-3", "test-kind", false),
				makeRequestMessage("seqA-4", "test-kind", false),
			},
			expectedChain: map[string]string{
				"seqA-3": "<dispatch>",
				"seqA-4": "seqA-3",
			},
		},
		{
			name: "no dispatchable messages",
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("seqA-5", "test-kind", false), // can't start here
			},
		},
		{
			name:      "already dispatched skipped",
			sequences: map[string]int{"seqA": 1},
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("seqA-1", "test-kind", true), // already dispatched
				makeRequestMessage("seqA-2", "test-kind", false),
				makeRequestMessage("seqA-3", "test-kind", false),
			},
			expectedChain: map[string]string{
				"seqA-2": "<dispatch>",
				"seqA-3": "seqA-2",
			},
		},
		{
			name: "message with final status is skipped and blocks successor",
			pendingRequests: []*devicemgmtstate.RequestMessage{
				func() *devicemgmtstate.RequestMessage {
					msg := makeRequestMessage("seqA-1", "test-kind", false)
					msg.ResponseStatus = asserts.MessageStatusRejected
					return msg
				}(),
				makeRequestMessage("seqA-2", "test-kind", false),
			},
			expectedChain: map[string]string{},
		},
		{
			name: "mixed sequenced and unsequenced",
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("uns1", "test-kind", false),
				makeRequestMessage("uns2", "test-kind", false),
				makeRequestMessage("seqA-1", "test-kind", false),
				makeRequestMessage("seqA-2", "test-kind", false),
			},
			expectedChain: map[string]string{
				"uns1":   "<dispatch>",
				"uns2":   "<dispatch>",
				"seqA-1": "<dispatch>",
				"seqA-2": "seqA-1",
			},
		},
		{
			name: "multiple independent sequences",
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("seqA-1", "test-kind", false),
				makeRequestMessage("seqA-2", "test-kind", false),
				makeRequestMessage("seqB-1", "test-kind", false),
				makeRequestMessage("seqB-2", "test-kind", false),
			},
			expectedChain: map[string]string{
				"seqA-1": "<dispatch>",
				"seqA-2": "seqA-1",
				"seqB-1": "<dispatch>",
				"seqB-2": "seqB-1",
			},
		},
	}

	for _, tt := range tests {
		cmt := Commentf("%s test", tt.name)

		sequences := make(map[string]*devicemgmtstate.SequenceState)
		for _, msg := range tt.pendingRequests {
			if sequences[msg.BaseID] == nil {
				sequences[msg.BaseID] = &devicemgmtstate.SequenceState{}
			}

			sequences[msg.BaseID].Messages = append(sequences[msg.BaseID].Messages, msg)
		}

		sequenceLRU := make([]string, 0)
		for seqID, lastApplied := range tt.sequences {
			sequences[seqID].Applied = lastApplied
			sequenceLRU = append(sequenceLRU, seqID)
		}

		ms := &devicemgmtstate.DeviceMgmtState{
			Sequences:      sequences,
			SequenceLRU:    sequenceLRU,
			ReadyResponses: make(map[string]store.Message),
		}
		s.mgr.SetState(ms)

		chg := s.st.NewChange("test", "test change")
		dispatchTask := s.st.NewTask("dispatch-mgmt-messages", "test dispatch-messages task")
		chg.AddTask(dispatchTask)

		alreadyDispatched := make(map[string]bool)
		for _, msg := range tt.pendingRequests {
			if msg.Dispatched {
				alreadyDispatched[msg.ID()] = true
			}
		}

		s.st.Unlock()
		err := s.mgr.DoDispatchMessages(dispatchTask, &tomb.Tomb{})
		s.st.Lock()
		c.Assert(err, IsNil, cmt)

		ms, err = s.mgr.GetState()
		c.Assert(err, IsNil, cmt)

		var notDispatched []string
		dispatched := make([]string, 0, len(tt.expectedChain))
		for _, seq := range ms.Sequences {
			for _, msg := range seq.Messages {
				_, inChain := tt.expectedChain[msg.ID()]
				if inChain {
					dispatched = append(dispatched, msg.ID())
				} else {
					notDispatched = append(notDispatched, msg.ID())
				}

				c.Check(msg.Dispatched, Equals, alreadyDispatched[msg.ID()] || inChain, Commentf("%s: %s", tt.name, msg.ID()))
			}
		}

		ti := buildTaskIndex(chg)
		assertMessagesDispatched(c, ti, dispatched, tt.name)
		assertMessagesNotDispatched(c, ti, notDispatched, tt.name)
		assertMessagesWaitOn(c, ti, tt.expectedChain, tt.name)
	}
}

func (s *deviceMgmtMgrSuite) TestDoDispatchMessagesEvictedSequenceRejected(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	const maxSequences = 4
	s.AddCleanup(devicemgmtstate.MockMaxSequences(maxSequences))

	// seqA and seqB are the 2 oldest in LRU and will be evicted; each gets 2
	// messages to verify the 2nd is dropped on eviction.
	messages := []store.MessageWithToken{
		s.makeStoreRequestMessage(c, "seqA-1", "test-kind", "token-seqA-1"),
		s.makeStoreRequestMessage(c, "seqA-2", "test-kind", "token-seqA-2"),
		s.makeStoreRequestMessage(c, "seqB-1", "test-kind", "token-seqB-1"),
		s.makeStoreRequestMessage(c, "seqB-2", "test-kind", "token-seqB-2"),
	}
	for i := 3; i <= maxSequences+2; i++ {
		baseID := fmt.Sprintf("seq%c", rune('A'+i-1))
		messages = append(messages,
			s.makeStoreRequestMessage(c, fmt.Sprintf("%s-1", baseID), "test-kind", fmt.Sprintf("token-%s-1", baseID)),
		)
	}

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{Messages: messages}, nil
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 1)

	// seqA evicted (oldest in LRU).
	seqA := ms.Sequences["seqA"]
	c.Assert(seqA.Messages, HasLen, 1, Commentf("the 2nd message in seqA should have been deleted"))
	c.Check(seqA.Messages[0].ResponseStatus, Equals, asserts.MessageStatusRejected)
	c.Check(seqA.Messages[0].ResponseBody["message"], Equals, "cannot process message: sequence evicted due to capacity limits")

	ti := buildTaskIndex(changes[0])
	c.Check(ti.validate["seqA-1"], IsNil)
	c.Check(ti.apply["seqA-1"], IsNil)
	c.Check(ti.queue["seqA-1"], NotNil)

	// seqB also evicted.
	seqB := ms.Sequences["seqB"]
	c.Assert(seqB.Messages, HasLen, 1, Commentf("the 2nd message in seqB should have been deleted"))
	c.Check(seqB.Messages[0].ResponseStatus, Equals, asserts.MessageStatusRejected)

	c.Check(ms.SequenceLRU, DeepEquals, []string{"seqC", "seqD", "seqE", "seqF"})
}

func (s *deviceMgmtMgrSuite) TestDoDispatchMessagesBlockedSequenceRejected(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	const maxBlockedMessagesPerSequence = 4
	s.AddCleanup(devicemgmtstate.MockMaxBlockedMessagesPerSequence(maxBlockedMessagesPerSequence))

	// Build a sequence stuck at a gap: seqNum 1 is missing, messages start at 2.
	messages := make([]store.MessageWithToken, maxBlockedMessagesPerSequence+1)
	for i := range messages {
		seqNum := i + 2
		messages[i] = s.makeStoreRequestMessage(c, fmt.Sprintf("seqA-%d", seqNum), "test-kind", fmt.Sprintf("token-seqA-%d", seqNum))
	}

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{Messages: messages}, nil
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	seqA := ms.Sequences["seqA"]
	c.Assert(seqA.Messages, HasLen, 1, Commentf("remaining messages should have been deleted"))
	c.Check(seqA.Messages[0].ResponseStatus, Equals, asserts.MessageStatusRejected)
	c.Check(seqA.Messages[0].ResponseBody["message"], Equals, "cannot process message: too many messages waiting on missing predecessors in sequence")

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 1)
	ti := buildTaskIndex(changes[0])
	c.Check(ti.queue["seqA-2"], NotNil)
	c.Check(ti.validate["seqA-2"], IsNil)
	c.Check(ti.apply["seqA-2"], IsNil)
}

func (s *deviceMgmtMgrSuite) TestDoDispatchMessagesIdempotent(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{}, nil
	})

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"msg1": {
				Messages: []*devicemgmtstate.RequestMessage{
					{
						AccountID:   "my-brand",
						AuthorityID: "my-brand",
						BaseID:      "msg1",
						Kind:        "test-kind",
						Devices:     []string{"serial-1.my-model.my-brand"},
						ValidSince:  fixedTestTime,
						ValidUntil:  fixedTestTime.Add(24 * time.Hour),
						Body:        `{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`,
					},
				},
			},
			"msg2": {
				Messages: []*devicemgmtstate.RequestMessage{
					{
						AccountID:   "my-brand",
						AuthorityID: "my-brand",
						BaseID:      "msg2",
						Kind:        "test-kind",
						Devices:     []string{"serial-1.my-model.my-brand"},
						ValidSince:  fixedTestTime,
						ValidUntil:  fixedTestTime.Add(24 * time.Hour),
						Body:        `{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`,
					},
				},
			},
		},
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	for i := 1; i <= 3; i++ {
		t := s.st.NewTask("dispatch-mgmt-messages", fmt.Sprintf("test dispatch %d", i))
		chg.AddTask(t)
	}

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)

	// Each message should have been dispatched exactly once:
	// 3 dispatch tasks + 2 messages * 3 tasks each = 9 tasks.
	c.Assert(chg.Tasks(), HasLen, 9)

	ti := buildTaskIndex(chg)
	c.Check(ti.validate["msg1"], NotNil)
	c.Check(ti.apply["msg1"], NotNil)
	c.Check(ti.queue["msg1"], NotNil)
	c.Check(ti.validate["msg2"], NotNil)
	c.Check(ti.apply["msg2"], NotNil)
	c.Check(ti.queue["msg2"], NotNil)
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.runner.AddHandler("apply-mgmt-message", noopTask, nil)
	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatus("")) // message wasn't rejected
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageBadRawAssertion(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{}, nil
	})

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"msg1": {
				Messages: []*devicemgmtstate.RequestMessage{
					{
						AccountID:    "my-brand",
						AuthorityID:  "my-brand",
						BaseID:       "msg1",
						Kind:         "test-kind",
						Devices:      []string{"serial-1.my-model.my-brand"},
						ValidSince:   fixedTestTime.Add(-time.Hour),
						ValidUntil:   fixedTestTime.Add(24 * time.Hour),
						Body:         `{"action": "get"}`,
						RawAssertion: []byte("not a valid assertion"),
					},
				},
			},
		},
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	t := s.st.NewTask("validate-mgmt-message", "validate msg1")
	t.Set("message-id", "msg1")
	chg.AddTask(t)

	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusRejected)
	c.Check(msg.ResponseBody["message"], Equals, "cannot decode message: assertion content/signature separator not found")
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageFetchAccountKeysError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	fetchErr := fmt.Errorf("store unavailable")
	s.AddCleanup(devicemgmtstate.MockFetchAccountKeys(func(_ *state.State, _ int, _ []string) error {
		return fetchErr
	}))

	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatus(""))
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageBadSignature(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{}, nil
	})

	storeMsg := s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1")
	reqMsg, err := devicemgmtstate.ParseRequestMessage(storeMsg.Message)
	c.Assert(err, IsNil)

	// tamper the raw assertion body
	reqMsg.RawAssertion = bytes.Replace(reqMsg.RawAssertion, []byte("get"), []byte("set"), 1)

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"msg1": {Messages: []*devicemgmtstate.RequestMessage{reqMsg}},
		},
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	t := s.st.NewTask("validate-mgmt-message", "validate msg1")
	t.Set("message-id", "msg1")
	chg.AddTask(t)

	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusRejected)
	c.Check(msg.ResponseBody["message"], Equals,
		"cannot verify message signature: failed signature verification: openpgp: invalid signature: hash tag doesn't match")
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageDeviceNotTargeted(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.mgr.MockBackend(&mockDeviceBackend{serial: s.makeSerial(c, "other-serial")})

	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusRejected)
	c.Check(msg.ResponseBody["message"], Equals, "cannot process message: not intended for device other-serial.my-model.my-brand")
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageExpired(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime.Add(48 * time.Hour)))

	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusRejected)
	c.Check(msg.ResponseBody["message"], Equals, "cannot process message: not valid at 2025-06-16T12:00:00Z")
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageUnknownKind(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "unknown-kind", "token-1"),
			},
		}, nil
	})

	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusRejected)
	c.Check(msg.ResponseBody["message"], Equals, `cannot find handler for message kind "unknown-kind"`)
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageUnauthorized(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.mgr.RegisterHandler("test-kind", &mockMessageHandler{
		validate: func(_ *state.State, _ *devicemgmtstate.RequestMessage) error {
			return &devicemgmtstate.UnauthorizedError{Operator: "alice"}
		},
	})

	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusUnauthorized)
	c.Check(msg.ResponseBody["message"], Equals, `operator "alice" is not authorized to perform this operation`)
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageHandlerError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.mgr.RegisterHandler("test-kind", &mockMessageHandler{
		validate: func(_ *state.State, _ *devicemgmtstate.RequestMessage) error {
			return fmt.Errorf("invalid payload")
		},
	})

	s.runner.AddHandler("queue-mgmt-response", noopTask, nil)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusRejected)
	c.Check(msg.ResponseBody["message"], Equals, "invalid payload")
}

func (s *deviceMgmtMgrSuite) TestDoValidateMessageIdempotent(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{}, nil
	})

	validateCalls := 0
	s.mgr.RegisterHandler("test-kind", &mockMessageHandler{
		validate: func(_ *state.State, _ *devicemgmtstate.RequestMessage) error {
			validateCalls++
			return fmt.Errorf("invalid payload")
		},
	})

	storeMsg := s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1")
	reqMsg, err := devicemgmtstate.ParseRequestMessage(storeMsg.Message)
	c.Assert(err, IsNil)

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"msg1": {
				Messages: []*devicemgmtstate.RequestMessage{reqMsg},
			},
		},
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	for i := 1; i <= 3; i++ {
		t := s.st.NewTask("validate-mgmt-message", fmt.Sprintf("validate msg1 attempt %d", i))
		t.Set("message-id", "msg1")
		chg.AddTask(t)
	}

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(validateCalls, Equals, 1)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusRejected)
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageOK(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ApplyChangeID, Not(Equals), "")
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageSkipIfAlreadyFailed(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.runner.AddHandler("validate-mgmt-message", func(t *state.Task, _ *tomb.Tomb) error {
		t.State().Lock()
		defer t.State().Unlock()

		var messageID string
		err := t.Get("message-id", &messageID)
		c.Assert(err, IsNil)

		ms, err := s.mgr.GetState()
		c.Assert(err, IsNil)

		ms.Sequences[messageID].Messages[0].ResponseStatus = asserts.MessageStatusRejected
		ms.Sequences[messageID].Messages[0].ResponseBody = map[string]any{
			"message": "cannot process message: device not in target list",
		}

		s.mgr.SetState(ms)

		return nil
	}, nil)

	s.mgr.RegisterHandler("test-kind", &mockMessageHandler{
		apply: func(*state.State, *devicemgmtstate.RequestMessage) (string, error) {
			c.Fatal("apply call not expected for already-failed message")

			return "", nil
		},
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ApplyChangeID, Equals, "")
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusRejected)
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageNoHandlerForMessageKind(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{}, nil
	})

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"msg1": {
				Messages: []*devicemgmtstate.RequestMessage{
					{
						AccountID:   "my-brand",
						AuthorityID: "my-brand",
						BaseID:      "msg1",
						Kind:        "unknown-kind",
						Devices:     []string{"serial-1.my-model.my-brand"},
						ValidSince:  fixedTestTime,
						ValidUntil:  fixedTestTime.Add(24 * time.Hour),
						Body:        `{"action": "get"}`,
					},
				},
			},
		},
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	t := s.st.NewTask("apply-mgmt-message", "apply message with unknown kind")
	t.Set("message-id", "msg1")
	chg.AddTask(t)

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ApplyChangeID, Equals, "")
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusError)
	c.Check(msg.ResponseBody["message"], Equals, `cannot find handler for message kind "unknown-kind"`)
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageApplyError(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreRequestMessage(c, "msg1", "test-kind", "token-1"),
			},
		}, nil
	})

	s.mgr.RegisterHandler("test-kind", &mockMessageHandler{
		apply: func(st *state.State, msg *devicemgmtstate.RequestMessage) (string, error) {
			return "", fmt.Errorf("system in inconsistent state")
		},
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	msg := ms.Sequences["msg1"].Messages[0]
	c.Check(msg.ApplyChangeID, Equals, "")
	c.Check(msg.ResponseStatus, Equals, asserts.MessageStatusError)
	c.Check(msg.ResponseBody["message"], Equals, "system in inconsistent state")
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageIdempotent(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockStore(func(_ context.Context, _ *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{}, nil
	})

	applyCalls := 0
	s.mgr.RegisterHandler("test-kind", &mockMessageHandler{
		apply: func(st *state.State, msg *devicemgmtstate.RequestMessage) (string, error) {
			applyCalls++
			chg := st.NewChange("subsystem", "apply payload")
			devicemgmtstate.MarkChangeForMessage(chg, msg)
			return chg.ID(), nil
		},
	})

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"msg1": {
				Messages: []*devicemgmtstate.RequestMessage{
					{
						AccountID:   "my-brand",
						AuthorityID: "my-brand",
						BaseID:      "msg1",
						Kind:        "test-kind",
						Devices:     []string{"serial-1.my-model.my-brand"},
						ValidSince:  fixedTestTime,
						ValidUntil:  fixedTestTime.Add(24 * time.Hour),
						Body:        `{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`,
					},
				},
			},
		},
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	for i := 1; i <= 3; i++ {
		t := s.st.NewTask("apply-mgmt-message", fmt.Sprintf("apply msg1 attempt %d", i))
		t.Set("message-id", "msg1")
		chg.AddTask(t)
	}

	s.settle(c)

	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(applyCalls, Equals, 1)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.Sequences["msg1"].Messages[0].ApplyChangeID, Not(Equals), "")
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageRecoverExistingChange(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"msg1": {
				Messages: []*devicemgmtstate.RequestMessage{
					{
						AccountID:   "my-brand",
						AuthorityID: "my-brand",
						BaseID:      "msg1",
						Kind:        "test-kind",
						Devices:     []string{"serial-1.my-model.my-brand"},
						ValidSince:  fixedTestTime,
						ValidUntil:  fixedTestTime.Add(24 * time.Hour),
						Body:        `{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`,
					},
				},
			},
		},
	}
	s.mgr.SetState(ms)

	// Simulate a change that was created and marked before the crash.
	existingChg := s.st.NewChange("subsystem", "apply payload")
	devicemgmtstate.MarkChangeForMessage(existingChg, ms.Sequences["msg1"].Messages[0])

	s.mgr.RegisterHandler("test-kind", &mockMessageHandler{
		apply: func(*state.State, *devicemgmtstate.RequestMessage) (string, error) {
			c.Fatal("apply must not be called when a marked change already exists")
			return "", nil
		},
	})

	chg := s.st.NewChange("test", "test change")
	t := s.st.NewTask("apply-mgmt-message", "apply msg1")
	t.Set("message-id", "msg1")
	chg.AddTask(t)

	s.st.Unlock()
	err := s.mgr.DoApplyMessage(t, &tomb.Tomb{})
	s.st.Lock()
	c.Assert(err, IsNil)

	ms, err = s.mgr.GetState()
	c.Assert(err, IsNil)
	c.Check(ms.Sequences["msg1"].Messages[0].ApplyChangeID, Equals, existingChg.ID())
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageSequenceNotFound(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences:      make(map[string]*devicemgmtstate.SequenceState),
		ReadyResponses: make(map[string]store.Message),
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	t := s.st.NewTask("apply-mgmt-message", "apply message with unknown base id")
	t.Set("message-id", "seqA-2")
	chg.AddTask(t)

	s.st.Unlock()
	err := s.mgr.DoApplyMessage(t, &tomb.Tomb{})
	s.st.Lock()

	c.Assert(err, ErrorMatches, `cannot find sequence "seqA"`)
}

func (s *deviceMgmtMgrSuite) TestDoApplyMessageMessageNotFound(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"seqA": {
				Messages: []*devicemgmtstate.RequestMessage{
					{
						AccountID:  "my-brand",
						BaseID:     "seqA",
						SeqNum:     1,
						Kind:       "test-kind",
						ValidSince: fixedTestTime,
						ValidUntil: fixedTestTime.Add(24 * time.Hour),
					},
				},
			},
		},
		ReadyResponses: make(map[string]store.Message),
	}
	s.mgr.SetState(ms)

	chg := s.st.NewChange("test", "test change")
	t := s.st.NewTask("apply-mgmt-message", "apply missing sequenced message")
	t.Set("message-id", "seqA-2")
	chg.AddTask(t)

	s.st.Unlock()
	err := s.mgr.DoApplyMessage(t, &tomb.Tomb{})
	s.st.Lock()

	c.Assert(err, ErrorMatches, `cannot find message "seqA-2"`)
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

func (s *deviceMgmtMgrSuite) TestMarkChangeForMessage(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	msg := &devicemgmtstate.RequestMessage{BaseID: "msg1"}

	chg := s.st.NewChange("subsystem", "apply payload")
	devicemgmtstate.MarkChangeForMessage(chg, msg)
	c.Check(chg.Has(devicemgmtstate.MgmtMessageIDKey), Equals, true)

	found := devicemgmtstate.FindChangeByMgmtMessageID(s.st, "msg1")
	c.Assert(found, NotNil)
	c.Check(found.ID(), Equals, chg.ID())

	notFound := devicemgmtstate.FindChangeByMgmtMessageID(s.st, "other-msg")
	c.Check(notFound, IsNil)
}

func changesOfKind(changes []*state.Change, kind string) []*state.Change {
	var result []*state.Change
	for _, chg := range changes {
		if chg.Kind() == kind {
			result = append(result, chg)
		}
	}

	sort.Slice(result, func(i, j int) bool {
		idI, _ := strconv.Atoi(result[i].ID())
		idJ, _ := strconv.Atoi(result[j].ID())
		return idI < idJ
	})

	return result
}

type taskIndex struct {
	validate map[string]*state.Task
	apply    map[string]*state.Task
	queue    map[string]*state.Task
}

func buildTaskIndex(chg *state.Change) *taskIndex {
	ti := &taskIndex{
		validate: make(map[string]*state.Task),
		apply:    make(map[string]*state.Task),
		queue:    make(map[string]*state.Task),
	}
	for _, t := range chg.Tasks() {
		var id string
		err := t.Get("message-id", &id)
		if err != nil {
			continue
		}

		switch t.Kind() {
		case "validate-mgmt-message":
			ti.validate[id] = t
		case "apply-mgmt-message":
			ti.apply[id] = t
		case "queue-mgmt-response":
			ti.queue[id] = t
		}
	}

	return ti
}

func assertMessagesDispatched(c *C, ti *taskIndex, msgIDs []string, testName string) {
	for _, id := range msgIDs {
		cmt := Commentf("%s: expected %s to be dispatched", testName, id)
		c.Assert(ti.validate[id], NotNil, cmt)
		c.Assert(ti.apply[id], NotNil, cmt)
		c.Assert(ti.queue[id], NotNil, cmt)
	}
}

func assertMessagesNotDispatched(c *C, ti *taskIndex, msgIDs []string, testName string) {
	for _, id := range msgIDs {
		cmt := Commentf("%s: expected %s to not be dispatched", testName, id)
		c.Assert(ti.validate[id], IsNil, cmt)
		c.Assert(ti.apply[id], IsNil, cmt)
		c.Assert(ti.queue[id], IsNil, cmt)
	}
}

func assertMessagesWaitOn(c *C, ti *taskIndex, waitOn map[string]string, testName string) {
	for msgID, prevID := range waitOn {
		cmt := Commentf("%s: invalid wait chain for %s", testName, msgID)

		validate := ti.validate[msgID]
		c.Assert(validate, NotNil, cmt)

		waitTasks := validate.WaitTasks()
		c.Assert(waitTasks, HasLen, 1, cmt)

		if prevID == "<dispatch>" {
			c.Assert(waitTasks[0].Kind(), Equals, "dispatch-mgmt-messages", cmt)
		} else {
			prevQueue := ti.queue[prevID]
			c.Assert(prevQueue, NotNil, cmt)
			c.Assert(waitTasks[0].ID(), Equals, prevQueue.ID(), cmt)
		}
	}
}
