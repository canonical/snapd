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
	"strconv"
	"strings"
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

	mgmtMessageIDKey = "mgmt-message-id"
)

var (
	timeNow = time.Now

	maxSequences                  = 256
	maxBlockedMessagesPerSequence = 8

	deviceMgmtExchangeChangeKind = swfeats.RegisterChangeKind("device-management-exchange")
)

// deviceIdentityProvider returns the device's serial assertion.
type deviceIdentityProvider interface {
	Serial() (*asserts.Serial, error)
}

// MessageHandler processes request messages of a specific kind.
// Caller must hold state lock when using this interface.
type MessageHandler interface {
	// Validate checks subsystem-specific constraints.
	Validate(st *state.State, msg *RequestMessage) error

	// Apply creates a change to process the message and returns its ID.
	// Implementations must call MarkChangeForMessage on the created change before
	// releasing the state lock.
	Apply(st *state.State, msg *RequestMessage) (changeID string, err error)

	// ResultFromChange reads the completed change and returns the full result.
	ResultFromChange(chg *state.Change) (body map[string]any, err error)
}

// UnauthorizedError is returned by MessageHandler.Validate when the operator
// does not have permission to perform the requested operation.
type UnauthorizedError struct {
	Operator string
}

func (e *UnauthorizedError) Error() string {
	return fmt.Sprintf("operator %q is not authorized to perform this operation", e.Operator)
}

// MarkChangeForMessage records the message ID on the change created by an Apply
// implementation. It must be called after change creation and before releasing
// the state lock, so that doApplyMessage can recover the change ID on retry
// and not call the handler's Apply again.
func MarkChangeForMessage(chg *state.Change, msg *RequestMessage) {
	chg.Set(mgmtMessageIDKey, msg.ID())
}

// responseMessageSigner can sign response-message assertions.
type responseMessageSigner interface {
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
	Assumes     []string  `json:"assumes,omitempty"`
	Body        string    `json:"body"`

	ReceiveTime time.Time `json:"receive-time"`
	Dispatched  bool      `json:"dispatched"`

	// ApplyChangeID is set when Apply schedules async work.
	ApplyChangeID string `json:"apply-change-id,omitempty"`

	// ResponseStatus and ResponseBody hold the final processing outcome.
	// A non-empty ResponseStatus means the message has been fully processed.
	ResponseStatus asserts.MessageStatus `json:"response-status,omitempty"`
	ResponseBody   map[string]any        `json:"response-body,omitempty"`
}

// ID returns the full message identifier `BaseID[-SeqNum]`.
func (msg *RequestMessage) ID() string {
	if msg.SeqNum != 0 {
		return fmt.Sprintf("%s-%d", msg.BaseID, msg.SeqNum)
	}

	return msg.BaseID
}

// ValidAt returns whether the request-message is valid at 'when' time.
func (msg *RequestMessage) ValidAt(when time.Time) bool {
	return (when.Equal(msg.ValidSince) || when.After(msg.ValidSince)) && when.Before(msg.ValidUntil)
}

// Targets returns whether the given device is listed in the message's devices header.
func (msg *RequestMessage) Targets(devID asserts.DeviceID) bool {
	target := devID.String()
	for _, d := range msg.Devices {
		if d == target {
			return true
		}
	}

	return false
}

// sequenceState holds the messages and progress for a single base ID,
// covering both sequenced & unsequenced messages.
type sequenceState struct {
	// Messages holds request messages from receipt until their response is queued.
	Messages []*RequestMessage `json:"messages"`

	// Applied is the highest sequence number successfully applied. A sequenced
	// message can only be applied once its predecessor has been applied.
	Applied int `json:"applied"`
}

// deviceMgmtState holds the persistent state for device management operations.
type deviceMgmtState struct {
	// Sequences maps base IDs to their per-base-ID state.
	Sequences map[string]*sequenceState `json:"sequences"`

	// SequenceLRU tracks sequenced base IDs in least-recently-used order for eviction.
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
func (ms *deviceMgmtState) getRequestMessage(id string) (*RequestMessage, error) {
	baseID, seqStr, hasSeq := strings.Cut(id, "-")
	seqNum := 0
	if hasSeq {
		seqNum, _ = strconv.Atoi(seqStr)
	}

	seq := ms.Sequences[baseID]
	if seq == nil {
		return nil, fmt.Errorf("cannot find sequence %q", baseID)
	}

	// TODO:GOVERSION:1.21: replace with slices.BinarySearchFunc
	i := sort.Search(len(seq.Messages), func(i int) bool {
		return seq.Messages[i].SeqNum >= seqNum
	})
	if i < len(seq.Messages) && seq.Messages[i].SeqNum == seqNum {
		return seq.Messages[i], nil
	}

	return nil, fmt.Errorf("cannot find message %q", id)
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

		seq := ms.Sequences[reqMsg.BaseID]
		if seq == nil {
			seq = &sequenceState{}
			ms.Sequences[reqMsg.BaseID] = seq
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
			ms.removeSequenceFromLRU(reqMsg.BaseID)
			ms.SequenceLRU = append(ms.SequenceLRU, reqMsg.BaseID)
		}
	}

	if len(pollResp.Messages) > 0 {
		token := pollResp.Messages[len(pollResp.Messages)-1].Token
		ms.LastReceivedToken = token
	} else {
		ms.LastReceivedToken = ""
	}

	ms.ReadyResponses = make(map[string]store.Message)
}

// removeSequenceFromLRU removes a sequence from the LRU list, if present.
func (ms *deviceMgmtState) removeSequenceFromLRU(baseID string) {
	for i, id := range ms.SequenceLRU {
		if id == baseID {
			ms.SequenceLRU = append(ms.SequenceLRU[:i], ms.SequenceLRU[i+1:]...)
			return
		}
	}
}

// DeviceMgmtManager handles device management operations.
type DeviceMgmtManager struct {
	state    *state.State
	identity deviceIdentityProvider
	signer   responseMessageSigner
	handlers map[string]MessageHandler
}

// Manager creates a new DeviceMgmtManager.
func Manager(state *state.State, runner *state.TaskRunner, identity deviceIdentityProvider, signer responseMessageSigner) *DeviceMgmtManager {
	m := &DeviceMgmtManager{
		state:    state,
		identity: identity,
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

// RegisterHandler registers a MessageHandler for the given message kind.
func (m *DeviceMgmtManager) RegisterHandler(kind string, h MessageHandler) {
	m.handlers[kind] = h
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
		ms.ReadyResponses = map[string]store.Message{}
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
func (m *DeviceMgmtManager) doDispatchMessages(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	chg := t.Change()
	// Evict oldest sequences when the LRU exceeds capacity.
	for len(ms.SequenceLRU) > maxSequences {
		baseID := ms.SequenceLRU[0]
		ms.SequenceLRU = ms.SequenceLRU[1:]
		err = m.rejectSequence(ms, chg, baseID, "cannot process message: sequence evicted due to capacity limits")
		if err != nil {
			return err
		}
	}

	for baseID, seq := range ms.Sequences {
		dispatched := m.dispatchSequence(t, seq)
		// If nothing was dispatched, the sequence is stuck at a gap (one or more missing predecessors).
		// Reject if too many messages have accumulated waiting on it.
		if dispatched == 0 && len(seq.Messages) > maxBlockedMessagesPerSequence {
			err = m.rejectSequence(ms, chg, baseID, "cannot process message: too many messages waiting on missing predecessors in sequence")
			if err != nil {
				return err
			}
		}
	}

	m.setState(ms)

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
func (m *DeviceMgmtManager) dispatchMessage(prevTask *state.Task, msg *RequestMessage) *state.Task {
	chg := prevTask.Change()
	// TODO: add tests verifying that a failure in one message's task chain does not
	// affect other messages (lanes provide this isolation, but it needs test coverage).
	lane := m.state.NewLane()

	addTask := func(kind, summary string) {
		t := m.state.NewTask(kind, summary)
		t.Set("message-id", msg.ID())
		t.WaitFor(prevTask)
		t.JoinLane(lane)
		chg.AddTask(t)

		prevTask = t
	}

	addTask("validate-mgmt-message", fmt.Sprintf("Validate message with id %q", msg.ID()))
	addTask("apply-mgmt-message", fmt.Sprintf("Apply message with id %q", msg.ID()))
	addTask("queue-mgmt-response", fmt.Sprintf("Queue response for message with id %q", msg.ID()))

	msg.Dispatched = true

	return prevTask
}

// rejectSequence rejects the earliest pending message in a sequence and discards
// the rest. It removes the sequence from the LRU and queues a rejection response.
func (m *DeviceMgmtManager) rejectSequence(ms *deviceMgmtState, chg *state.Change, baseID, reason string) error {
	seq := ms.Sequences[baseID]
	if seq == nil || len(seq.Messages) == 0 {
		return fmt.Errorf("internal error: rejectSequence called for baseID %q with no pending messages", baseID)
	}

	earliest := seq.Messages[0]
	earliest.ResponseStatus = asserts.MessageStatusRejected
	earliest.ResponseBody = map[string]any{"message": reason}
	seq.Messages = []*RequestMessage{earliest}

	ms.removeSequenceFromLRU(baseID)

	lane := m.state.NewLane()
	queue := m.state.NewTask("queue-mgmt-response", fmt.Sprintf("Queue response for message with id %q", earliest.ID()))
	queue.Set("message-id", earliest.ID())
	queue.JoinLane(lane)
	chg.AddTask(queue)

	return nil
}

// doValidateMessage performs snapd-level and subsystem-level validation on a message.
func (m *DeviceMgmtManager) doValidateMessage(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	var msgID string
	err = t.Get("message-id", &msgID)
	if err != nil {
		return err
	}

	msg, err := ms.getRequestMessage(msgID)
	if err != nil {
		return err
	}

	if msg.ResponseStatus != "" {
		return nil
	}

	defer m.setState(ms)

	reject := func(reason string) error {
		msg.ResponseStatus = asserts.MessageStatusRejected
		msg.ResponseBody = map[string]any{"message": reason}
		return nil
	}

	serial, err := m.identity.Serial()
	if err != nil {
		return err
	}
	devID := serial.DeviceID()
	if !msg.Targets(devID) {
		return reject(fmt.Sprintf("message not intended for device %s", devID))
	}

	now := timeNow()
	if !msg.ValidAt(now) {
		return reject(fmt.Sprintf("message not valid at %s", now.UTC().Format(time.RFC3339)))
	}

	// TODO: implement assumes checks.

	handler, ok := m.handlers[msg.Kind]
	if !ok {
		return reject(fmt.Sprintf("cannot find handler for message kind %q", msg.Kind))
	}

	err = handler.Validate(m.state, msg)
	if err != nil {
		var unauthorizedErr *UnauthorizedError
		if errors.As(err, &unauthorizedErr) {
			msg.ResponseStatus = asserts.MessageStatusUnauthorized
			msg.ResponseBody = map[string]any{"message": err.Error()}
			return nil
		}

		return reject(err.Error())
	}

	return nil
}

// doApplyMessage dispatches the message to its subsystem handler for processing.
func (m *DeviceMgmtManager) doApplyMessage(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	var msgID string
	err = t.Get("message-id", &msgID)
	if err != nil {
		return err
	}

	msg, err := ms.getRequestMessage(msgID)
	if err != nil {
		return err
	}

	if msg.ResponseStatus != "" || msg.ApplyChangeID != "" {
		// No-op if the message failed earlier in the pipeline or was already applied.
		return nil
	}

	defer m.setState(ms)

	// Check if a change was already created for this message before persisting its ApplyChangeID.
	chg := findChangeByMgmtMessageID(m.state, msgID)
	if chg != nil {
		msg.ApplyChangeID = chg.ID()
		return nil
	}

	handler, ok := m.handlers[msg.Kind]
	if !ok {
		msg.ResponseStatus = asserts.MessageStatusError
		msg.ResponseBody = map[string]any{"message": fmt.Sprintf("cannot find handler for message kind %q", msg.Kind)}
		return nil
	}

	chgID, err := handler.Apply(m.state, msg)
	if err != nil {
		msg.ResponseStatus = asserts.MessageStatusError
		msg.ResponseBody = map[string]any{"message": err.Error()}
	} else {
		msg.ApplyChangeID = chgID
	}

	return nil
}

// doQueueResponse builds a response, signs it, and queues it for transmission on the next exchange.
// Retries until subsystem change completes.
func (m *DeviceMgmtManager) doQueueResponse(t *state.Task, _ *tomb.Tomb) error {
	// TODO: implement this task, no-op for now.
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
		Assumes:     reqAs.Assumes(),
		Body:        string(reqAs.Body()),
		ReceiveTime: timeNow(),
	}, nil
}

// findChangeByMgmtMessageID scans all changes for one marked with the given
// message ID via MarkChangeForMessage.
func findChangeByMgmtMessageID(st *state.State, msgID string) *state.Change {
	for _, chg := range st.Changes() {
		var id string
		err := chg.Get(mgmtMessageIDKey, &id)
		if err != nil {
			continue
		}

		if id == msgID {
			return chg
		}
	}

	return nil
}
