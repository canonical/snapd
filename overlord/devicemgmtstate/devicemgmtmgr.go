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
// for message-based remote device management. It receives signed request-message
// assertions from the store via periodic message exchanges, validates them against
// SD187 requirements, dispatches them to subsystem-specific handlers (like confdb),
// and sends back response-message assertions with processing results.
package devicemgmtstate

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/store"
	"gopkg.in/tomb.v2"
)

const (
	deviceMgmtStateKey = "device-mgmt"

	defaultExchangeLimit    = 10
	defaultExchangeInterval = 6 * time.Hour

	maxSequences = 256
)

var (
	timeNow = time.Now

	deviceMgmtExchangeChangeKind = swfeats.RegisterChangeKind("device-management-exchange")
)

// MessageHandler processes request messages of a specific kind.
// Caller must hold state lock when using this interface.
type MessageHandler interface {
	// Validate checks subsystem-specific constraints (authorization, payload schema, etc).
	Validate(st *state.State, msg *RequestMessage) error

	// Apply processes a request-message and returns a change ID.
	Apply(st *state.State, reqAs *RequestMessage) (changeID string, err error)

	// BuildResponse converts a completed change into a response body and status.
	BuildResponse(chg *state.Change) (body map[string]any, status asserts.MessageStatus)
}

// ResponseMessageSigner can sign response-message assertions.
type ResponseMessageSigner interface {
	SignResponseMessage(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error)
}

// RequestMessage represents a request-message being processed.
// Messages remain pending until their associated change completes,
// at which point a response is queued and the message is removed.
type RequestMessage struct {
	AccountID   string    `json:"account-id"`
	AuthorityID string    `json:"authority-id"`
	BaseID      string    `json:"base-id"`
	SeqNum      int       `json:"seq-num"`
	Kind        string    `json:"kind"`
	Devices     []string  `json:"devices"`
	ValidSince  time.Time `json:"valid-since"`
	ValidUntil  time.Time `json:"valid-until"`
	Body        string    `json:"body"`

	ReceiveTime time.Time             `json:"receive-time"`
	ChangeID    string                `json:"change-id,omitempty"` // Subsystem change applying this message
	Status      asserts.MessageStatus `json:"status,omitempty"`    // Response status
	Error       string                `json:"error,omitempty"`     // Error/rejection reason
}

// ID returns the full message identifier `BaseID[-SeqNum]`.
func (msg *RequestMessage) ID() string {
	if msg.SeqNum != 0 {
		return fmt.Sprintf("%s-%d", msg.BaseID, msg.SeqNum)
	}

	return msg.BaseID
}

// sequenceCache is the LRU-bounded cache of tracked message sequences.
type sequenceCache struct {
	// Applied tracks how far each sequence has progressed. A sequenced
	// message can only be applied once its predecessor has been applied.
	Applied map[string]int `json:"applied"`

	// LRU determines eviction order when the cache is full.
	LRU []string `json:"lru"`
}

// deviceMgmtState holds the persistent state for device management operations.
type deviceMgmtState struct {
	// PendingRequests maps message IDs to request messages being processed.
	// A message stays here from receipt until its response is queued.
	PendingRequests map[string]*RequestMessage `json:"pending-requests"`

	// Sequences is the LRU-bounded cache of tracked message sequences.
	Sequences *sequenceCache `json:"sequences"`

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

// enqueueRequests queues incoming request messages for processing
// and updates polling state accordingly.
func (ms *deviceMgmtState) enqueueRequests(pollResp *store.MessageExchangeResponse) {
	for _, msg := range pollResp.Messages {
		reqMsg, err := parseRequestMessage(msg.Message)
		if err != nil {
			// Malformed messages are acknowledged but not processed.
			// There's no point retrying since if parsing fails once, it will fail again.
			logger.Noticef("cannot parse request-message with token %s: %v", msg.Token, err)
			continue
		}

		_, exists := ms.PendingRequests[reqMsg.ID()]
		if !exists {
			ms.PendingRequests[reqMsg.ID()] = reqMsg
		}
	}

	if len(pollResp.Messages) > 0 {
		token := pollResp.Messages[len(pollResp.Messages)-1].Token
		ms.LastReceivedToken = token
	} else {
		ms.LastReceivedToken = ""
	}

	ms.ReadyResponses = make(map[string]store.Message)
	ms.LastExchangeTime = timeNow()
}

// touchSequence marks a sequence as recently used, adding it if new.
func (ms *deviceMgmtState) touchSequence(baseID string) {
	_, exists := ms.Sequences.Applied[baseID]
	if !exists {
		ms.Sequences.Applied[baseID] = 0
	}

	// Move sequence to end (most recently used).
	for i, id := range ms.Sequences.LRU {
		if id == baseID {
			ms.Sequences.LRU = append(ms.Sequences.LRU[:i], ms.Sequences.LRU[i+1:]...)
			break
		}
	}

	ms.Sequences.LRU = append(ms.Sequences.LRU, baseID)
}

// evictLRUSequence evicts the least recently used sequence and returns its earliest
// pending message for rejection. Remaining messages in the sequence are deleted.
// The returned message is cleaned up by queue-mgmt-response after its response is queued.
func (ms *deviceMgmtState) evictLRUSequence() *RequestMessage {
	if len(ms.Sequences.LRU) == 0 {
		return nil
	}

	baseID := ms.Sequences.LRU[0]
	delete(ms.Sequences.Applied, baseID)

	ms.Sequences.LRU = ms.Sequences.LRU[1:]

	var msgs []*RequestMessage
	var earliest *RequestMessage
	for _, msg := range ms.PendingRequests {
		if msg.BaseID == baseID && msg.SeqNum > 0 {
			msgs = append(msgs, msg)

			if earliest == nil || msg.SeqNum < earliest.SeqNum {
				earliest = msg
			}
		}
	}

	for _, msg := range msgs {
		if msg != earliest {
			delete(ms.PendingRequests, msg.ID())
		}
	}

	return earliest
}

// DeviceMgmtManager handles device management operations.
type DeviceMgmtManager struct {
	state    *state.State
	signer   ResponseMessageSigner
	handlers map[string]MessageHandler
}

// Manager creates a new DeviceMgmtManager.
func Manager(state *state.State, runner *state.TaskRunner, signer ResponseMessageSigner) *DeviceMgmtManager {
	m := &DeviceMgmtManager{
		state:    state,
		signer:   signer,
		handlers: make(map[string]MessageHandler),
	}

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
				PendingRequests: make(map[string]*RequestMessage),
				Sequences: &sequenceCache{
					Applied: make(map[string]int),
					LRU:     make([]string, 0),
				},
				ReadyResponses: make(map[string]store.Message),
			}, nil
		}

		return nil, err
	}

	return &ms, nil
}

// setState persists the device management state.
func (m *DeviceMgmtManager) setState(ms *deviceMgmtState) {
	m.state.Set(deviceMgmtStateKey, ms)
}

// Ensure implements StateManager.Ensure.
func (m *DeviceMgmtManager) Ensure() error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	exchange := m.shouldExchangeMessages(ms)
	if !exchange {
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
func (m *DeviceMgmtManager) doExchangeMessages(t *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

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

	ms.enqueueRequests(pollResp)
	m.setState(ms)

	return nil
}

// doDispatchMessages selects pending requests for processing and queues tasks for them.
func (m *DeviceMgmtManager) doDispatchMessages(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	chg := t.Change()
	m.pruneSequences(chg, ms)

	// Dispatch unsequenced messages.
	sequences := make(map[string][]*RequestMessage)
	for _, msg := range ms.PendingRequests {
		// No explicit "dispatched" marker is needed; the single-change-in-flight
		// guard in Ensure() prevents concurrent dispatch.
		if msg.ChangeID != "" || msg.Status != "" {
			continue // Already dispatched
		}

		if msg.SeqNum == 0 {
			m.dispatchMessage(chg, t, msg)
			continue
		}

		sequences[msg.BaseID] = append(sequences[msg.BaseID], msg)
	}

	// Dispatch sequenced messages.
	for _, msgs := range sequences {
		m.dispatchSequence(chg, ms, t, msgs)
	}

	m.setState(ms)
	m.state.EnsureBefore(0)

	return nil
}

// pruneSequences evicts tracked sequences that exceed the capacity limit,
// queuing a rejection response for each evicted sequence.
func (m *DeviceMgmtManager) pruneSequences(chg *state.Change, ms *deviceMgmtState) {
	latestReceiveTime := make(map[string]time.Time)
	for _, msg := range ms.PendingRequests {
		if msg.SeqNum <= 0 {
			continue
		}

		t, ok := latestReceiveTime[msg.BaseID]
		if !ok || msg.ReceiveTime.After(t) {
			latestReceiveTime[msg.BaseID] = msg.ReceiveTime
		}
	}

	baseIDs := make([]string, 0, len(latestReceiveTime))
	for baseID := range latestReceiveTime {
		baseIDs = append(baseIDs, baseID)
	}
	sort.Slice(baseIDs, func(i, j int) bool {
		return latestReceiveTime[baseIDs[i]].Before(latestReceiveTime[baseIDs[j]])
	})

	for _, baseID := range baseIDs {
		ms.touchSequence(baseID)
	}

	for len(ms.Sequences.Applied) > maxSequences {
		earliest := ms.evictLRUSequence()
		if earliest != nil {
			earliest.Status = asserts.MessageStatusRejected
			earliest.Error = "sequence evicted from cache due to capacity limits"

			lane := m.state.NewLane()
			queue := m.state.NewTask("queue-mgmt-response", fmt.Sprintf("Queue response for message with id %q", earliest.ID()))
			queue.Set("id", earliest.ID())
			queue.JoinLane(lane)
			chg.AddTask(queue)
		}
	}
}

// dispatchSequence dispatches sequenced messages starting from where the sequence left off,
// chaining consecutive messages. Gaps in the sequence stop the chain.
// All messages must belong to the same sequence.
func (m *DeviceMgmtManager) dispatchSequence(chg *state.Change, ms *deviceMgmtState, dispatchTask *state.Task, msgs []*RequestMessage) {
	sort.Slice(msgs, func(i, j int) bool {
		return msgs[i].SeqNum < msgs[j].SeqNum
	})

	// Find the first message that can be dispatched.
	startIdx := -1
	applied := ms.Sequences.Applied[msgs[0].BaseID]
	for i, msg := range msgs {
		if msg.SeqNum == applied+1 {
			startIdx = i
			break
		}
	}

	if startIdx == -1 {
		return
	}

	// Chain consecutive messages from the start point.
	awaitTask := dispatchTask
	expectedSeqNum := msgs[startIdx].SeqNum
	for i := startIdx; i < len(msgs); i++ {
		if msgs[i].SeqNum != expectedSeqNum {
			// Gap in sequence, stop chaining.
			break
		}

		awaitTask = m.dispatchMessage(chg, awaitTask, msgs[i])
		expectedSeqNum++
	}
}

// dispatchMessage creates the task chain for a single message and returns
// the final task so callers can chain subsequent messages after it.
func (m *DeviceMgmtManager) dispatchMessage(chg *state.Change, awaitTask *state.Task, msg *RequestMessage) *state.Task {
	lane := m.state.NewLane()

	validate := m.state.NewTask("validate-mgmt-message", fmt.Sprintf("Validate message with id %q", msg.ID()))
	validate.Set("id", msg.ID())
	validate.WaitFor(awaitTask)
	validate.JoinLane(lane)
	chg.AddTask(validate)

	apply := m.state.NewTask("apply-mgmt-message", fmt.Sprintf("Apply message with id %q", msg.ID()))
	apply.Set("id", msg.ID())
	apply.WaitFor(validate)
	apply.JoinLane(lane)
	chg.AddTask(apply)

	queue := m.state.NewTask("queue-mgmt-response", fmt.Sprintf("Queue response for message with id %q", msg.ID()))
	queue.Set("id", msg.ID())
	queue.WaitFor(apply)
	queue.JoinLane(lane)
	chg.AddTask(queue)

	return queue
}

// doValidateMessage performs snapd-level and subsystem-level validation on a message.
func (m *DeviceMgmtManager) doValidateMessage(t *state.Task, _ *tomb.Tomb) error {
	// TODO: implement this task, no-op for now.
	return nil
}

// doApplyMessage dispatches the message to its subsystem handler for processing.
func (m *DeviceMgmtManager) doApplyMessage(t *state.Task, _ *tomb.Tomb) error {
	// TODO: implement this task, no-op for now.
	return nil
}

// doQueueResponse builds a response, signs it, and queues it for transmission on the next exchange.
// For messages with a subsystem change, the task retries until the change completes.
func (m *DeviceMgmtManager) doQueueResponse(t *state.Task, _ *tomb.Tomb) error {
	// TODO: implement this task, no-op for now.
	// TODO: on success for sequenced messages, update seq.Applied = msg.SeqNum.
	return nil
}

// parseRequestMessage decodes a store message body into a RequestMessage.
func parseRequestMessage(msg store.Message) (*RequestMessage, error) {
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

	return &RequestMessage{
		AccountID:   reqAs.AccountID(),
		AuthorityID: reqAs.AuthorityID(),
		BaseID:      reqAs.ID(),
		SeqNum:      reqAs.SeqNum(),
		Kind:        reqAs.Kind(),
		Devices:     deviceIDs,
		ValidSince:  reqAs.ValidSince(),
		ValidUntil:  reqAs.ValidUntil(),
		Body:        string(reqAs.Body()),
		ReceiveTime: timeNow(),
	}, nil
}
