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

var fixedTestTime = time.Date(2025, 6, 14, 12, 0, 0, 0, time.UTC)

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

func (s *deviceMgmtMgrSuite) makeStoreMessage(c *C, messageID, token string) store.MessageWithToken {
	oneHourAgo := fixedTestTime.Add(-time.Hour)
	tomorrow := oneHourAgo.Add(24 * time.Hour)
	body := []byte(`{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`)
	as, err := s.storeStack.Sign(
		asserts.RequestMessageType,
		map[string]any{
			"authority-id": "my-brand",
			"account-id":   "my-brand",
			"message-id":   messageID,
			"message-kind": "confdb",
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
	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime))

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

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()
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

	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime))

	setRemoteMgmtFeatureFlag(c, s.st, true)

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

	s.settle(c)

	changes := changesOfKind(s.st.Changes(), "device-management-exchange")
	c.Assert(changes, HasLen, 0)
}

func (s *deviceMgmtMgrSuite) TestEnsureFeatureDisabledWithReadyResponses(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.mockModel()
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

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Check(req.After, Equals, "")
		c.Check(req.Limit, Equals, devicemgmtstate.DefaultExchangeLimit)
		c.Check(req.Messages, HasLen, 0)

		return &store.MessageExchangeResponse{
			Messages:             []store.MessageWithToken{s.makeStoreMessage(c, "someId", "token-123")},
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
	c.Check(msg.Kind, Equals, "confdb")
	c.Check(msg.Devices, DeepEquals, []string{"serial-1.my-model.my-brand"})
	c.Check(msg.Body, Equals, `{"action": "get", "account": "my-brand", "view": "network/wifi-state"}`)
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

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreMessage(c, "seqA-1", "token-seqA-1"),
				s.makeStoreMessage(c, "seqB-1", "token-seqB-1"),
				s.makeStoreMessage(c, "uns7", "token-uns1"),
				s.makeStoreMessage(c, "seqB-2", "token-seqB-2"),
				s.makeStoreMessage(c, "seqC-1", "token-seqC-1"),
				s.makeStoreMessage(c, "seqA-2", "token-seqA-2"),
				s.makeStoreMessage(c, "uns8", "token-uns2"),
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

	setRemoteMgmtFeatureFlag(c, s.st, true)

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

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		msg := s.makeStoreMessage(c, "someId", "token-1")
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

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.AddCleanup(snapstatetest.MockDeviceContext(nil))
	s.st.Set("seeded", false)

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		c.Log("call not expected")
		c.Fail()

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

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()
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

	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime))

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				// deliver the same message twice
				s.makeStoreMessage(c, "someId", "token-1"),
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

	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime))

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()

	// Exchange 1: receive msg1 only so it gets dispatched.
	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreMessage(c, "msg1", "token-1"),
			},
		}, nil
	})

	s.settle(c)

	// Exchange 2: msg1 is dedup'd by exchange; msg2 and msg3 are new.
	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime.Add(2 * devicemgmtstate.DefaultExchangeInterval)))

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{
			Messages: []store.MessageWithToken{
				s.makeStoreMessage(c, "msg1", "token-1"),
				s.makeStoreMessage(c, "msg2", "token-2"),
				s.makeStoreMessage(c, "msg3", "token-3"),
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
				makeRequestMessage("seqA-1", "confdb", false),
				makeRequestMessage("seqA-2", "confdb", false),
				makeRequestMessage("seqA-3", "confdb", false),
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
				makeRequestMessage("seqA-1", "confdb", false),
				makeRequestMessage("seqA-2", "confdb", false),
				makeRequestMessage("seqA-4", "confdb", false), // 3 is missing
				makeRequestMessage("seqA-5", "confdb", false),
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
				makeRequestMessage("seqA-3", "confdb", false),
				makeRequestMessage("seqA-4", "confdb", false),
			},
			expectedChain: map[string]string{
				"seqA-3": "<dispatch>",
				"seqA-4": "seqA-3",
			},
		},
		{
			name: "no dispatchable messages",
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("seqA-5", "confdb", false), // can't start here
			},
		},
		{
			name:      "already dispatched skipped",
			sequences: map[string]int{"seqA": 1},
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("seqA-1", "confdb", true), // already dispatched
				makeRequestMessage("seqA-2", "confdb", false),
				makeRequestMessage("seqA-3", "confdb", false),
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
					msg := makeRequestMessage("seqA-1", "confdb", false)
					msg.Status = asserts.MessageStatusRejected
					return msg
				}(),
				makeRequestMessage("seqA-2", "confdb", false),
			},
			expectedChain: map[string]string{},
		},
		{
			name: "mixed sequenced and unsequenced",
			pendingRequests: []*devicemgmtstate.RequestMessage{
				makeRequestMessage("uns1", "confdb", false),
				makeRequestMessage("uns2", "confdb", false),
				makeRequestMessage("seqA-1", "confdb", false),
				makeRequestMessage("seqA-2", "confdb", false),
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
				makeRequestMessage("seqA-1", "confdb", false),
				makeRequestMessage("seqA-2", "confdb", false),
				makeRequestMessage("seqB-1", "confdb", false),
				makeRequestMessage("seqB-2", "confdb", false),
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

	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime))

	const maxSequences = 4
	s.AddCleanup(devicemgmtstate.MockMaxSequences(maxSequences))

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()

	// seqA and seqB are the 2 oldest in LRU and will be evicted; each gets 2
	// messages to verify the 2nd is dropped on eviction.
	messages := []store.MessageWithToken{
		s.makeStoreMessage(c, "seqA-1", "token-seqA-1"),
		s.makeStoreMessage(c, "seqA-2", "token-seqA-2"),
		s.makeStoreMessage(c, "seqB-1", "token-seqB-1"),
		s.makeStoreMessage(c, "seqB-2", "token-seqB-2"),
	}
	for i := 3; i <= maxSequences+2; i++ {
		baseID := fmt.Sprintf("seq%c", rune('A'+i-1))
		messages = append(messages,
			s.makeStoreMessage(c, fmt.Sprintf("%s-1", baseID), fmt.Sprintf("token-%s-1", baseID)),
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
	c.Check(seqA.Messages[0].Status, Equals, asserts.MessageStatusRejected)
	c.Check(seqA.Messages[0].Error, Equals, "cannot process message: sequence evicted due to capacity limits")

	ti := buildTaskIndex(changes[0])
	c.Check(ti.validate["seqA-1"], IsNil)
	c.Check(ti.apply["seqA-1"], IsNil)
	c.Check(ti.queue["seqA-1"], NotNil)

	// seqB also evicted.
	seqB := ms.Sequences["seqB"]
	c.Assert(seqB.Messages, HasLen, 1, Commentf("the 2nd message in seqB should have been deleted"))
	c.Check(seqB.Messages[0].Status, Equals, asserts.MessageStatusRejected)

	c.Check(ms.SequenceLRU, DeepEquals, []string{"seqC", "seqD", "seqE", "seqF"})
}

func (s *deviceMgmtMgrSuite) TestDoDispatchMessagesBlockedSequenceRejected(c *C) {
	s.st.Lock()
	defer s.st.Unlock()

	s.AddCleanup(devicemgmtstate.MockTimeNow(fixedTestTime))

	const maxBlockedMessagesPerSequence = 4
	s.AddCleanup(devicemgmtstate.MockMaxBlockedMessagesPerSequence(maxBlockedMessagesPerSequence))

	setRemoteMgmtFeatureFlag(c, s.st, true)

	s.mockModel()

	// Build a sequence stuck at a gap: seqNum 1 is missing, messages start at 2.
	messages := make([]store.MessageWithToken, maxBlockedMessagesPerSequence+1)
	for i := range messages {
		seqNum := i + 2
		messages[i] = s.makeStoreMessage(c, fmt.Sprintf("seqA-%d", seqNum), fmt.Sprintf("token-seqA-%d", seqNum))
	}

	s.mockStore(func(ctx context.Context, req *store.MessageExchangeRequest) (*store.MessageExchangeResponse, error) {
		return &store.MessageExchangeResponse{Messages: messages}, nil
	})

	s.settle(c)

	ms, err := s.mgr.GetState()
	c.Assert(err, IsNil)

	seqA := ms.Sequences["seqA"]
	c.Assert(seqA.Messages, HasLen, 1, Commentf("remaining messages should have been deleted"))
	c.Check(seqA.Messages[0].Status, Equals, asserts.MessageStatusRejected)
	c.Check(seqA.Messages[0].Error, Equals, "cannot process message: too many messages waiting on missing predecessors in sequence")

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

	ms := &devicemgmtstate.DeviceMgmtState{
		Sequences: map[string]*devicemgmtstate.SequenceState{
			"msg1": {
				Messages: []*devicemgmtstate.RequestMessage{
					{
						AccountID:   "my-brand",
						AuthorityID: "my-brand",
						BaseID:      "msg1",
						Kind:        "confdb",
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
						Kind:        "confdb",
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
