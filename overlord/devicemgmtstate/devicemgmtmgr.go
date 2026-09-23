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

// Package devicemgmtstate implements the manager and state aspects responsible
// for remote device management.
package devicemgmtstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicemgmtstate/handlers"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/store"
	"gopkg.in/tomb.v2"
)

const (
	deviceMgmtStateKey = "device-mgmt"
	taskMessageKey     = "message-key"

	defaultExchangeLimit    = 10
	defaultExchangeInterval = 6 * time.Hour
)

var (
	timeNow                    = time.Now
	assertstateFetchAccountKey = assertstate.FetchAccountKey

	maxSequences                  = 256
	maxBlockedMessagesPerSequence = 8

	awaitSubsystemRetryInterval = 30 * time.Second

	deviceMgmtExchangeChangeKind = swfeats.RegisterChangeKind("device-management-exchange")
)

// deviceBackend provides device identity and response message signing.
type deviceBackend interface {
	Serial() (*asserts.Serial, error)
	SignResponseMessage(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error)
}

// sequenceState holds the messages and progress for a single sequence within an account,
// covering both sequenced & unsequenced messages.
type sequenceState struct {
	// Messages holds request messages from receipt until their response is queued.
	Messages []*handlers.RequestMessage `json:"messages"`

	// Applied is the highest sequence number successfully applied. A sequenced
	// message can only be applied once its predecessor has been applied.
	Applied int `json:"applied"`
}

// deviceMgmtState holds the persistent state for device management operations.
type deviceMgmtState struct {
	// Sequences maps sequence keys to the sequence's state.
	Sequences map[string]*sequenceState `json:"sequences"`

	// SequenceLRU tracks sequence keys in least-recently-used order for eviction.
	SequenceLRU []string `json:"sequence-lru"`

	// LastReceivedToken is the token of the last message successfully stored locally,
	// sent in the "after" field of the next exchange to acknowledge receipt
	// up to this point.
	LastReceivedToken string `json:"last-received-token"`

	// ReadyResponses are response messages ready to send in the next exchange.
	// Cleared after successful transmission.
	ReadyResponses map[string]store.Message `json:"ready-responses"`

	// LastExchangeTime is the timestamp of the last message exchange.
	LastExchangeTime time.Time `json:"last-exchange-time"`
}

// getRequestMessage retrieves a request message from the state.
func (ms *deviceMgmtState) getRequestMessage(key string) (*handlers.RequestMessage, error) {
	accountID, id, ok := strings.Cut(key, "/")
	if !ok {
		return nil, fmt.Errorf("invalid message key %q", key)
	}

	baseID, seqStr, hasSeq := strings.Cut(id, "-")
	seqNum := 0
	if hasSeq {
		seqNum, _ = strconv.Atoi(seqStr)
	}

	seqKey := fmt.Sprintf("%s/%s", accountID, baseID)
	seq := ms.Sequences[seqKey]
	if seq == nil {
		return nil, fmt.Errorf("cannot find sequence %q", seqKey)
	}

	// TODO:GOVERSION:1.21: replace with slices.BinarySearchFunc
	i := sort.Search(len(seq.Messages), func(i int) bool {
		return seq.Messages[i].SeqNum >= seqNum
	})
	if i < len(seq.Messages) && seq.Messages[i].SeqNum == seqNum {
		return seq.Messages[i], nil
	}

	return nil, fmt.Errorf("cannot find message %q", key)
}

// removeRequestMessage removes a processed request message from its sequence.
// For a sequenced message, the sequence entry is left in place so its Applied
// progress is preserved for later messages in the same sequence.
func (ms *deviceMgmtState) removeRequestMessage(msg *handlers.RequestMessage) {
	seqKey := msg.SeqKey()
	seq := ms.Sequences[seqKey]
	if seq == nil {
		return
	}

	for i, m := range seq.Messages {
		if m.SeqNum == msg.SeqNum {
			seq.Messages = append(seq.Messages[:i], seq.Messages[i+1:]...)

			// Unsequenced messages have no Applied progress to carry forward.
			if msg.SeqNum == 0 && len(seq.Messages) == 0 {
				delete(ms.Sequences, seqKey)
			}

			return
		}
	}
}

// enqueueRequestMessages queues incoming request messages for processing
// and updates polling state accordingly.
func (ms *deviceMgmtState) enqueueRequestMessages(pollResp *store.MessageExchangeResponse) {
	for _, msg := range pollResp.Messages {
		reqMsg, err := parseRequestMessage(msg.Message)
		if err != nil {
			// Malformed messages are acknowledged but not processed.
			// There's no point retrying since if parsing fails once, it will fail again.
			logger.Noticef("cannot parse request-message with token %s: %v", msg.Token, err)
			continue
		}

		seqKey := reqMsg.SeqKey()
		seq := ms.Sequences[seqKey]
		if seq == nil {
			seq = &sequenceState{}
			ms.Sequences[seqKey] = seq
		}

		// Drop any sequenced message that has already been applied.
		if reqMsg.SeqNum > 0 && reqMsg.SeqNum <= seq.Applied {
			continue
		}

		// TODO:GOVERSION:1.21: replace with slices.BinarySearchFunc
		i := sort.Search(len(seq.Messages), func(i int) bool {
			return seq.Messages[i].SeqNum >= reqMsg.SeqNum
		})
		if i < len(seq.Messages) && seq.Messages[i].SeqNum == reqMsg.SeqNum {
			continue // duplicate
		}
		// TODO:GOVERSION:1.21: replace with slices.Insert(seq.Messages, i, reqMsg)
		seq.Messages = append(seq.Messages, nil)
		copy(seq.Messages[i+1:], seq.Messages[i:])
		seq.Messages[i] = reqMsg

		if reqMsg.SeqNum > 0 {
			// Move to end of LRU to mark as recently used.
			ms.removeSequenceFromLRU(seqKey)
			ms.SequenceLRU = append(ms.SequenceLRU, seqKey)
		}
	}

	if len(pollResp.Messages) > 0 {
		// Record the token of the last message as the ack position. It will be
		// sent on the next exchange to advance the store's queue pointer.
		token := pollResp.Messages[len(pollResp.Messages)-1].Token
		ms.LastReceivedToken = token
	} else {
		// No messages were returned: the store has already advanced the queue
		// pointer from the previous exchange. Clear the token so the next
		// exchange sends no ack, since there is nothing new to acknowledge.
		ms.LastReceivedToken = ""
	}

	// The responses in this batch were sent during the exchange, so clear them.
	ms.ReadyResponses = make(map[string]store.Message)
}

// removeSequenceFromLRU removes a sequence from the LRU list, if present.
func (ms *deviceMgmtState) removeSequenceFromLRU(seqKey string) {
	for i, key := range ms.SequenceLRU {
		if key == seqKey {
			ms.SequenceLRU = append(ms.SequenceLRU[:i], ms.SequenceLRU[i+1:]...)
			return
		}
	}
}

// evictSequence deletes a sequence and removes it from the LRU.
func (ms *deviceMgmtState) evictSequence(seqKey string) {
	delete(ms.Sequences, seqKey)
	ms.removeSequenceFromLRU(seqKey)
}

/* The life of a request message.

   This diagram follows message N of a sequence. An unsequenced message
   follows much the same path on its own: it waits on no earlier message,
   and no later message waits on it.

   The names on the right are the tasks that handle each step. The names
   in brackets are the asserts.MessageStatus the message ends up with.

   Received                                     <- exchange-mgmt-messages
     |  \
     |   \ Malformed, duplicate, or message N
     |     already applied --> discarded, no response
     V
   Queued in sequence
     |
     V
   Too many sequences, and this one LRU?        <- dispatch-mgmt-messages
     |  \
     |   \ Yes ------------------------------------\
     |                                             |
     | No                                          |
     V                                             |
   Message N-1 applied or dispatched?              |
     |  \                                          |
     |   \ No --> Not dispatched                   |
     |               |                             |
     | Yes           | Too many messages blocked   |
     |               | in sequence?                |
     |               |  \                          |
     |               |   \ No --> Retried on the   |
     |               |            next dispatch    |
     |               | Yes                         |
     |               |                             |
     |               V                             |
     |        Sequence rejected <------------------/
     |               |
     |               V
     |        Message N first still pending?
     |               |  \
     |               |   \ No --> discarded, no response
     |               |
     |               | Yes
     |               V
     |            [rejected] ------------->\
     V                                     |
   Dispatched                              |
     |                                     |
     V                                     |
   Valid and authorized?                   |    <- validate-mgmt-message
     |  \                                  |
     |   \ No --> [rejected] or            |
     |            [unauthorized] --------->|
     | Yes                                 |
     V                                     |
   Subsystem change created?               |    <- apply-mgmt-message
     |  \                                  |
     |   \ No --> [error] ---------------->|
     |                                     |
     | Yes                                 |
     V                                     |
   Subsystem change successful?            |    <- queue-mgmt-response
     |  \                                  |
     |   \ No --> [error] ---------------->|
     |                                     |
     | Yes                                 |
     V                                     |
   [success] ----------------------------->|
                                           |
     /-------------------------------------/
     |
     V
   Response signed and queued
     |
     V
   [success]?
     |  \
     |   \ No --> Sequence evicted,
     |            tasks for messages N+1 onward aborted
     | Yes                       |
     V                           |
   Message removed from state <--/
     |
     V
   Response sent on the next exchange           <- exchange-mgmt-messages

   Eviction aborts the tasks of later messages already dispatched from the same
   sequence, which is what stops message N+1 from applying once N has failed.
*/

// DeviceMgmtManager handles device management operations.
type DeviceMgmtManager struct {
	state  *state.State
	device deviceBackend
}

// Manager creates a new DeviceMgmtManager.
func Manager(state *state.State, runner *state.TaskRunner, backend deviceBackend) *DeviceMgmtManager {
	m := &DeviceMgmtManager{state: state, device: backend}

	runner.AddHandler("exchange-mgmt-messages", m.doExchangeMessages, nil)
	runner.AddHandler("dispatch-mgmt-messages", m.doDispatchMessages, nil)
	runner.AddHandler("validate-mgmt-message", m.doValidateMessage, nil)
	runner.AddHandler("apply-mgmt-message", m.doApplyMessage, nil)
	runner.AddHandler("queue-mgmt-response", m.doQueueResponse, nil)

	return m
}

// getState retrieves the current device management state, initializing if not present.
func (m *DeviceMgmtManager) getState() (*deviceMgmtState, error) {
	var ms deviceMgmtState
	err := m.state.Get(deviceMgmtStateKey, &ms)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return &deviceMgmtState{
				Sequences:      make(map[string]*sequenceState),
				ReadyResponses: make(map[string]store.Message),
			}, nil
		}

		return nil, err
	}

	if ms.Sequences == nil {
		ms.Sequences = make(map[string]*sequenceState)
	}

	if ms.ReadyResponses == nil {
		ms.ReadyResponses = make(map[string]store.Message)
	}

	return &ms, nil
}

// getMessageAndState retrieves the current state along with the given message.
func (m *DeviceMgmtManager) getMessageAndState(msgKey string) (*deviceMgmtState, *handlers.RequestMessage, error) {
	ms, err := m.getState()
	if err != nil {
		return nil, nil, err
	}

	msg, err := ms.getRequestMessage(msgKey)
	if err != nil {
		return nil, nil, err
	}

	return ms, msg, nil
}

// setState persists the device management state.
func (m *DeviceMgmtManager) setState(ms *deviceMgmtState) {
	m.state.Set(deviceMgmtStateKey, ms)
}

// Ensure implements StateManager.Ensure.
func (m *DeviceMgmtManager) Ensure() error {
	m.state.Lock()
	defer m.state.Unlock()
	seeded, err := snapstate.SystemSeeded(m.state)
	if err != nil || !seeded {
		return err
	}

	ms, err := m.getState()
	if err != nil {
		return err
	}

	if !m.shouldExchangeMessages(ms) {
		return nil
	}

	// For now, only one device management change can be in flight at any given time.
	for _, chg := range m.state.Changes() {
		if chg.Kind() == deviceMgmtExchangeChangeKind && !chg.Status().Ready() {
			return nil
		}
	}

	chg := m.state.NewChange(deviceMgmtExchangeChangeKind, "Process device management messages")

	exchg := m.state.NewTask("exchange-mgmt-messages", "Exchange messages with the Store")
	chg.AddTask(exchg)

	dispatch := m.state.NewTask("dispatch-mgmt-messages", "Dispatch message(s) to subsystems")
	dispatch.WaitFor(exchg)
	chg.AddTask(dispatch)

	m.state.EnsureBefore(0)

	return nil
}

// isRemoteDeviceManagementEnabled checks whether the remote device management feature is enabled.
func (m *DeviceMgmtManager) isRemoteDeviceManagementEnabled() bool {
	tr := config.NewTransaction(m.state)
	enabled, err := features.Flag(tr, features.RemoteDeviceManagement)
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("cannot check remote-device-management feature flag: %v", err)

		// If the flag cannot be checked, assume disabled.
		return false
	}

	return enabled
}

// shouldExchangeMessages checks whether a message exchange should happen now.
func (m *DeviceMgmtManager) shouldExchangeMessages(ms *deviceMgmtState) bool {
	nextExchange := ms.LastExchangeTime.Add(defaultExchangeInterval)
	if timeNow().Before(nextExchange) {
		return false
	}

	// If disabled, still exchange to deliver responses for already-processed messages.
	return m.isRemoteDeviceManagementEnabled() || len(ms.ReadyResponses) > 0
}

// doExchangeMessages exchanges messages with the store: sends queued response messages,
// acknowledges receipt of persisted request messages, and fetches new request messages.
//
// The store maintains a queue of request messages for the device. In each
// exchange, the after field carries the token of the last message the device
// received, acknowledging every message up to and including it. A message stays
// queued until a later exchange acks it, so one lost to a crash is re-sent.
//
// For instance:
//
//	Exchange 1  -->  {after "", limit 10, no response messages}
//	            <--  Two request messages with tokenA and tokenB.
//	                 Both persisted locally, awaiting acknowledgement.
//
//	Exchange 2  -->  {after tokenB, limit 10, response messages}
//	                 Messages with tokenA and tokenB are dropped from the queue.
//	            <--  No new request messages.
//	                 Nothing new arrived, so there is nothing left to ack.
//
//	Exchange 3  -->  {after "", limit 10, ...}
func (m *DeviceMgmtManager) doExchangeMessages(t *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	defer func() {
		ms.LastExchangeTime = timeNow()
		m.setState(ms)
	}()

	deviceCtx, err := snapstate.DevicePastSeeding(m.state, nil)
	if err != nil {
		return err
	}
	sto := snapstate.Store(m.state, deviceCtx)

	limit := 0
	if m.isRemoteDeviceManagementEnabled() {
		limit = defaultExchangeLimit
	}

	messages := make([]store.Message, 0, len(ms.ReadyResponses))
	for _, msg := range ms.ReadyResponses {
		messages = append(messages, msg)
	}

	m.state.Unlock()
	pollResp, err := sto.ExchangeMessages(tomb.Context(nil), &store.MessageExchangeRequest{
		After:    ms.LastReceivedToken,
		Limit:    limit,
		Messages: messages,
	})
	m.state.Lock()
	if err != nil {
		return err
	}

	ms.enqueueRequestMessages(pollResp)

	return nil
}

// doDispatchMessages selects pending requests for processing and queues tasks for them.
//
// One chain is built per sequence, so sequences run independently, while the
// messages inside a sequence run in order. Each message's three tasks get
// their own lane, so a failing task aborts that message and the ones after it.
//
// For instance:
//
//	dispatch-mgmt-messages
//	   |
//	   |--> validate --> apply --> queue-response ---\  Message N          [lane 1]
//	   |                                             |
//	   |    /----------------------------------------/
//	   |    V
//	   |    validate --> apply --> queue-response ---\  Message N+1        [lane 2]
//	   |                                             |
//	   |    /----------------------------------------/
//	   |    V
//	   |    validate --> apply --> queue-response       Message N+2        [lane 3]
//	   |
//	   |--> validate --> apply --> queue-response       Other sequence     [lane 4]
//	   |
//	   |--> validate --> apply --> queue-response       Unsequenced        [lane 5]
//	   |
//	   \--> queue-response                              Rejected sequence  [lane 6]
func (m *DeviceMgmtManager) doDispatchMessages(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	// We may have already scheduled rejection tasks and made changes
	// to the state in rejectSequence, so persist even on error.
	defer m.setState(ms)

	// Reject oldest sequences when the LRU exceeds capacity.
	rejected := make(map[string]bool)
	excess := len(ms.SequenceLRU) - maxSequences
	if excess > 0 {
		seqKeys := append([]string(nil), ms.SequenceLRU[:excess]...)
		for _, seqKey := range seqKeys {
			err = m.rejectSequence(ms, t, seqKey, "cannot process message: sequence evicted due to capacity limits")
			if err != nil {
				return err
			}

			rejected[seqKey] = true
		}
	}

	for seqKey, seq := range ms.Sequences {
		if rejected[seqKey] {
			continue
		}

		dispatched := m.dispatchSequence(t, seq)
		// If nothing was dispatched, the sequence is stuck at a gap (one or more missing predecessors).
		// Reject if too many messages have accumulated waiting on it.
		if dispatched == 0 && len(seq.Messages) > maxBlockedMessagesPerSequence {
			err = m.rejectSequence(ms, t, seqKey, "cannot process message: too many messages waiting on missing predecessors in sequence")
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// dispatchSequence dispatches pending messages in a sequence starting from where
// it left off, chaining consecutive messages. Gaps in the sequence stop the chain.
// Messages are assumed to be sorted by SeqNum. Returns the number of messages dispatched.
func (m *DeviceMgmtManager) dispatchSequence(dispatchTask *state.Task, seq *sequenceState) int {
	// Unsequenced messages have SeqNum 0.
	expectedSeqNum := 0
	// Sequenced messages resume from where the sequence left off.
	if len(seq.Messages) > 0 && seq.Messages[0].SeqNum != 0 {
		expectedSeqNum = seq.Applied + 1
	}

	dispatched := 0
	awaitTask := dispatchTask
	for _, msg := range seq.Messages {
		// Skip messages already dispatched or that have a final result.
		if msg.Dispatched || msg.ResponseStatus != "" {
			continue
		}

		if msg.SeqNum != expectedSeqNum {
			// Gap in sequence, stop chaining.
			break
		}

		awaitTask = m.dispatchMessage(awaitTask, msg)
		expectedSeqNum++
		dispatched++
	}

	return dispatched
}

// dispatchMessage creates the task chain for a single message and returns
// the final task so callers can chain subsequent messages after it.
func (m *DeviceMgmtManager) dispatchMessage(prevTask *state.Task, msg *handlers.RequestMessage) *state.Task {
	chg := prevTask.Change()
	lane := m.state.NewLane()

	addTask := func(kind, summary string) {
		t := m.state.NewTask(kind, summary)
		t.Set(taskMessageKey, msg.Key())
		t.WaitFor(prevTask)
		t.JoinLane(lane)
		chg.AddTask(t)

		prevTask = t
	}

	addTask("validate-mgmt-message", fmt.Sprintf("Validate message %q", msg.Key()))
	addTask("apply-mgmt-message", fmt.Sprintf("Apply message %q", msg.Key()))
	addTask("queue-mgmt-response", fmt.Sprintf("Queue response for message %q", msg.Key()))

	msg.Dispatched = true

	return prevTask
}

// rejectSequence queues a rejection response for the earliest pending message
// in a sequence and discards the rest. An empty sequence is evicted.
func (m *DeviceMgmtManager) rejectSequence(ms *deviceMgmtState, dispatchTask *state.Task, seqKey, reason string) error {
	seq := ms.Sequences[seqKey]
	if seq == nil {
		return fmt.Errorf("internal error: rejectSequence called for unknown sequence %q", seqKey)
	}

	if len(seq.Messages) == 0 {
		// When rejecting the least recently used sequence, it might be empty if
		// all its messages have already been processed in prior changes.
		// There's no message to reject so it's simply evicted.
		ms.evictSequence(seqKey)
		return nil
	}

	earliest := seq.Messages[0]
	if earliest.ResponseStatus != "" {
		// The sequence was already rejected by a previous call and is pending
		// eviction once its queued response is processed.
		return nil
	}

	earliest.ResponseStatus = asserts.MessageStatusRejected
	earliest.ResponseBody = map[string]any{"message": reason}
	seq.Messages = []*handlers.RequestMessage{earliest}

	lane := m.state.NewLane()
	queue := m.state.NewTask("queue-mgmt-response", fmt.Sprintf("Queue response for message %q", earliest.Key()))
	queue.Set(taskMessageKey, earliest.Key())
	queue.JoinLane(lane)
	queue.WaitFor(dispatchTask)
	dispatchTask.Change().AddTask(queue)

	return nil
}

// doValidateMessage performs snapd-level and subsystem-level validation on a message.
func (m *DeviceMgmtManager) doValidateMessage(t *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	var msgKey string
	err := t.Get(taskMessageKey, &msgKey)
	if err != nil {
		return err
	}

	ms, msg, err := m.getMessageAndState(msgKey)
	if err != nil {
		return err
	}

	if msg.ResponseStatus != "" {
		return nil
	}

	setMsgResponse := func(status asserts.MessageStatus, message string) {
		msg.ResponseStatus = status
		msg.ResponseBody = map[string]any{"message": message}
		m.setState(ms)
	}
	rejectMsg := func(reason string) {
		setMsgResponse(asserts.MessageStatusRejected, reason)
	}

	a, err := asserts.Decode(msg.RawAssertion)
	if err != nil {
		rejectMsg(fmt.Sprintf("cannot decode message: %v", err))
		return nil
	}

	fetched, err := m.ensureAccountKey(a.SignKeyID())
	if err != nil {
		// TODO: need to distinguish between:
		//  - transient errors (like store unreachable) - retry
		//  - permanent errors - determine whether to fail the task or reject the message.
		return err
	}
	if fetched {
		// The state lock was dropped during the store fetch. Concurrent tasks in
		// other lanes may have mutated state in that window, so re-read before mutating.
		ms, msg, err = m.getMessageAndState(msgKey)
		if err != nil {
			return err
		}
	}

	err = assertstate.DB(m.state).Check(a)
	if err != nil {
		rejectMsg(fmt.Sprintf("cannot verify message signature: %v", err))
		return nil
	}

	serial, err := m.device.Serial()
	if err != nil {
		return err
	}
	devID := serial.DeviceID()
	if !msg.Targets(devID) {
		rejectMsg(fmt.Sprintf("cannot process message: not intended for device %s", devID))
		return nil
	}

	now := timeNow()
	if !msg.ValidAt(now) {
		rejectMsg(fmt.Sprintf("cannot process message: not valid at %s", now.UTC().Format(time.RFC3339)))
		return nil
	}

	// TODO: implement assumes checks (SD187, SD251). The design is somewhat in
	// flux: SD251 "extends" messages to non-run-mode contexts (e.g. first boot,
	// before the device has an identity), which will require assumes entries
	// like "seeding". For now, only the confdb subsystem is supported and
	// no specific features need to be declared.

	handler := handlers.Get(msg.Kind)
	if handler == nil {
		rejectMsg(fmt.Sprintf("cannot find handler for message kind %q", msg.Kind))
		return nil
	}

	err = handler.Validate(tomb.Context(nil), m.state, msg)
	if err != nil {
		var unauthorizedErr *handlers.UnauthorizedError
		status := asserts.MessageStatusRejected
		if errors.As(err, &unauthorizedErr) {
			status = asserts.MessageStatusUnauthorized
		}
		reason := err.Error()

		// handler.Validate may drop the state lock internally. Concurrent tasks
		// in other lanes may have mutated the state in that window, so re-read before mutating.
		ms, msg, err = m.getMessageAndState(msgKey)
		if err != nil {
			return err
		}

		setMsgResponse(status, reason)
		return nil
	}

	return nil
}

// ensureAccountKey fetches the account-key assertion for signKeyID from the
// store if it is not already in the local database.
func (m *DeviceMgmtManager) ensureAccountKey(signKeyID string) (fetched bool, err error) {
	_, err = assertstate.AccountKey(m.state, signKeyID)
	if err == nil {
		return false, nil
	}
	if !errors.Is(err, &asserts.NotFoundError{}) {
		return false, err
	}

	err = assertstateFetchAccountKey(m.state, 0, signKeyID)
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
		return true, err
	}

	return true, nil
}

// doApplyMessage dispatches the message to its subsystem handler for processing.
func (m *DeviceMgmtManager) doApplyMessage(t *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	var msgKey string
	err := t.Get(taskMessageKey, &msgKey)
	if err != nil {
		return err
	}

	ms, msg, err := m.getMessageAndState(msgKey)
	if err != nil {
		return err
	}

	if msg.ResponseStatus != "" || msg.ApplyChangeID != "" {
		// No-op if the message failed earlier in the pipeline or was already applied.
		return nil
	}

	// Check if a change was already created for this message before persisting its ApplyChangeID.
	chg := findChangeByMgmtMessageKey(m.state, msgKey)
	if chg != nil {
		msg.ApplyChangeID = chg.ID()
		m.setState(ms)
		return nil
	}

	handler := handlers.Get(msg.Kind)
	if handler == nil {
		msg.ResponseStatus = asserts.MessageStatusError
		msg.ResponseBody = map[string]any{"message": fmt.Sprintf("cannot find handler for message kind %q", msg.Kind)}
		m.setState(ms)
		return nil
	}

	// TODO: If a shutdown terminates this context while we're waiting for
	// another op to complete, we'll error out of this call and mark the
	// message as failed. It would make sense to retry this task instead.
	chgID, applyErr := handler.Apply(tomb.Context(nil), m.state, msg)

	// handler.Apply may drop the state lock internally. Concurrent tasks in
	// other lanes may have mutated the state in that window, so re-read before mutating.
	ms, msg, err = m.getMessageAndState(msgKey)
	if err != nil {
		return err
	}

	if applyErr != nil {
		msg.ResponseStatus = asserts.MessageStatusError
		msg.ResponseBody = map[string]any{"message": applyErr.Error()}
	} else {
		msg.ApplyChangeID = chgID
	}
	m.setState(ms)

	return nil
}

// doQueueResponse builds a response, signs it, and queues it for transmission on the next exchange.
// Retries until the subsystem change (if any) completes.
func (m *DeviceMgmtManager) doQueueResponse(t *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	var msgKey string
	err = t.Get(taskMessageKey, &msgKey)
	if err != nil {
		return err
	}

	msg, err := ms.getRequestMessage(msgKey)
	if err != nil {
		// Message already processed on a prior run.
		return nil
	}

	err = m.setMessageResponseFromChange(tomb.Context(nil), msg)
	if err != nil {
		return err
	}

	// handler.ResultFromChange may drop the state lock internally. Concurrent tasks
	// in other lanes may have mutated the state in that window, so re-read before mutating.
	responseStatus := msg.ResponseStatus
	responseBody := msg.ResponseBody

	ms, msg, err = m.getMessageAndState(msgKey)
	if err != nil {
		return err
	}

	msg.ResponseStatus = responseStatus
	msg.ResponseBody = responseBody

	bodyBytes, err := json.Marshal(msg.ResponseBody)
	if err != nil {
		return fmt.Errorf("cannot marshal response body: %w", err)
	}

	// TODO: determine reasonable behavior for internal errors (e.g., signing or marshal failures).
	// Since tasks are idempotent, a failed message will not be re-dispatched or re-applied on the
	// next change, but the request message remains in state until doQueueResponse completes,
	// so such failures leave it hanging indefinitely.

	resAs, err := m.device.SignResponseMessage(msg.AccountID, msg.ID(), msg.ResponseStatus, bodyBytes)
	if err != nil {
		return fmt.Errorf("cannot sign response message: %w", err)
	}

	ms.ReadyResponses[msg.Key()] = store.Message{
		Format: "assertion",
		Data:   string(asserts.Encode(resAs)),
	}

	if msg.SeqNum > 0 {
		seqKey := msg.SeqKey()
		if msg.ResponseStatus == asserts.MessageStatusSuccess {
			ms.Sequences[seqKey].Applied = msg.SeqNum
			ms.removeRequestMessage(msg)
		} else {
			ms.evictSequence(seqKey)
			// Abort all pending tasks in the sequence from message N+1 onwards.
			chg := t.Change()
			for _, ht := range t.HaltTasks() {
				chg.AbortLanes(ht.Lanes())
			}
		}
	} else {
		ms.removeRequestMessage(msg)
	}

	m.setState(ms)

	return nil
}

// setMessageResponseFromChange populates msg's response fields from the completed apply change.
func (m *DeviceMgmtManager) setMessageResponseFromChange(ctx context.Context, msg *handlers.RequestMessage) error {
	if msg.ResponseStatus != "" {
		return nil
	}

	handler := handlers.Get(msg.Kind)
	if handler == nil {
		msg.ResponseStatus = asserts.MessageStatusError
		msg.ResponseBody = map[string]any{"message": fmt.Sprintf("cannot find handler for message kind %q", msg.Kind)}
		return nil
	}

	chg := m.state.Change(msg.ApplyChangeID)
	if chg == nil {
		return fmt.Errorf("internal error: cannot find subsystem change %q", msg.ApplyChangeID)
	}
	if !chg.Status().Ready() {
		return &state.Retry{After: awaitSubsystemRetryInterval}
	}
	if chg.Status() != state.DoneStatus {
		msg.ResponseStatus = asserts.MessageStatusError
		err := chg.Err()
		if err == nil {
			err = fmt.Errorf("cannot process message: change is in unexpected status %q", chg.Status())
		}
		msg.ResponseBody = map[string]any{"message": err.Error()}
		return nil
	}

	body, err := handler.ResultFromChange(ctx, chg)
	if err != nil {
		msg.ResponseStatus = asserts.MessageStatusError
		msg.ResponseBody = map[string]any{"message": err.Error()}
	} else {
		msg.ResponseStatus = asserts.MessageStatusSuccess
		msg.ResponseBody = body
	}

	return nil
}

// parseRequestMessage decodes a store message body into a RequestMessage.
func parseRequestMessage(msg store.Message) (*handlers.RequestMessage, error) {
	if msg.Format != "assertion" {
		return nil, fmt.Errorf("cannot process assertion: unsupported format %q", msg.Format)
	}

	as, err := asserts.Decode([]byte(msg.Data))
	if err != nil {
		return nil, fmt.Errorf("cannot decode assertion: %w", err)
	}

	reqAs, ok := as.(*asserts.RequestMessage)
	if !ok {
		return nil, fmt.Errorf(`cannot process assertion: expected "request-message" but got %q`, as.Type().Name)
	}

	devices := reqAs.Devices()
	deviceIDs := make([]string, len(devices))
	for i, devID := range devices {
		deviceIDs[i] = devID.String()
	}

	return &handlers.RequestMessage{
		AccountID:    reqAs.AccountID(),
		AuthorityID:  reqAs.AuthorityID(),
		BaseID:       reqAs.ID(),
		SeqNum:       reqAs.SeqNum(),
		Kind:         reqAs.Kind(),
		Devices:      deviceIDs,
		ValidSince:   reqAs.ValidSince(),
		ValidUntil:   reqAs.ValidUntil(),
		Assumes:      reqAs.Assumes(),
		Body:         string(reqAs.Body()),
		ReceiveTime:  timeNow(),
		RawAssertion: []byte(msg.Data),
	}, nil
}

// findChangeByMgmtMessageKey scans all changes for one marked with the given
// message key via MarkChangeForMessage.
func findChangeByMgmtMessageKey(st *state.State, msgKey string) *state.Change {
	for _, chg := range st.Changes() {
		key, ok := handlers.ChangeMessageKey(chg)
		if !ok {
			continue
		}

		if key == msgKey {
			return chg
		}
	}

	return nil
}
