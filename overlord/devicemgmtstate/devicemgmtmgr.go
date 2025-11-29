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

// Package devicemgmtstate implements the manager and state aspects responsible
// for polling-based remote device management. It receives signed request-message
// assertions from the store via periodic polling, validates them against SD187
// requirements, dispatches them to subsystem-specific handlers (like confdb), and
// sends back response-message assertions with processing results.
package devicemgmtstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	// TODO: dynamically change this based on # of pending messages & other factors
	pollingLimit    = 10
	pollingInterval = 5 * time.Minute
)

var deviceMgmtCycleChangeKind = swfeats.RegisterChangeKind("device-management-cycle")

// MessageHandler processes request-message messages of a specific kind.
// Caller must hold state lock when using this interface.
type MessageHandler interface {
	// Validate checks subsystem-specific constraints (authorization, payload schema, etc).
	// Returns nil if valid, error describing the validation failure otherwise.
	Validate(st *state.State, msg *PendingMessage) error

	// Apply processes a request-message and returns a change ID.
	Apply(st *state.State, reqAs *PendingMessage) (changeID string, err error)

	// BuildResponse converts a completed change into a response body and status.
	BuildResponse(chg *state.Change) (body map[string]any, status asserts.MessageStatus)
}

// ResponseMessageSigner can sign response-message assertions.
type ResponseMessageSigner interface {
	SignResponseMessage(accountID, messageID string, status asserts.MessageStatus, body []byte) (*asserts.ResponseMessage, error)
}

// PendingMessage represents a request-message being processed.
// Messages remain pending until their associated change completes,
// at which point a response is queued and the message is removed.
type PendingMessage struct {
	BaseID      string `json:"base-id"`
	SeqNum      int    `json:"seq-num"`
	Kind        string `json:"kind"`
	AccountID   string `json:"account-id"`
	AuthorityID string `json:"authority-id"`
	Source      string `json:"source"`

	Devices    []string  `json:"devices"`
	ValidSince time.Time `json:"valid-since"`
	ValidUntil time.Time `json:"valid-until"`

	Body string `json:"body"`

	Received time.Time `json:"received"`
	ChangeID string    `json:"change-id,omitempty"` // Set when subsystem change is created
}

func (msg *PendingMessage) ID() string {
	if msg.SeqNum != 0 {
		return fmt.Sprintf("%s-%d", msg.BaseID, msg.SeqNum)
	}

	return msg.BaseID
}

// Sequence tracks the last received and applied sequence numbers for message ordering.
// TODO: implement sequencing and LRU eviction
type Sequence struct {
	// The <random-id> portion of message-id
	ID string `json:"id"`
	// The highest <N> we've stored
	LastStored int `json:"last-received"`
	// The highest <N> we've successfully applied
	LastApplied int `json:"last-applied"`
}

// DeviceMgmtState holds the persistent state for device management operations.
type DeviceMgmtState struct {
	// PendingMessages maps message IDs to messages being processed. A message
	// stays here from receipt until its response is queued. Messages with ChangeID
	// set are actively being processed by the subsystem's change.
	// TODO: add cap to PendingMessages queue to prevent unbounded growth.
	PendingMessages map[string]*PendingMessage `json:"pending-messages"`

	// Sequence tracking (LRU cache, max 256 entries)
	Sequences map[string]*Sequence `json:"sequences"`

	// PendingAckToken is the token of the last message we successfully stored,
	// sent in the "after" field of the next poll request to acknowledge receipt
	// up to this point.
	PendingAckToken string `json:"pending-ack-token"`

	// ReadyResponses are response-message assertions ready to send in the next poll.
	// Cleared after successful transmission.
	ReadyResponses map[string]store.Message `json:"ready-responses"`

	// Timestamp of last poll
	LastPolled time.Time `json:"last-polled"`
}

// DeviceMgmtManager handles device management operations.
type DeviceMgmtManager struct {
	state    *state.State
	signer   ResponseMessageSigner
	handlers map[string]MessageHandler
}

// Manager creates a new DeviceMgmtManager.
func Manager(state *state.State, runner *state.TaskRunner, signer ResponseMessageSigner) (*DeviceMgmtManager, error) {
	m := &DeviceMgmtManager{
		state:    state,
		signer:   signer,
		handlers: make(map[string]MessageHandler),
	}

	m.handlers["confdb"] = &ConfdbMessageHandler{}

	runner.AddHandler("poll-messages", m.doPollMessages, nil)
	runner.AddHandler("dispatch-messages", m.doDispatchMessages, nil)
	runner.AddHandler("validate-message", m.doValidateMessage, nil)
	runner.AddHandler("apply-message", m.doApplyMessage, nil)
	runner.AddHandler("await-subsystem-change", m.doAwaitSubsystemChange, nil)
	runner.AddHandler("queue-response", m.doQueueResponse, nil)

	return m, nil
}

// getState retrieves the current device management state, initializing if not present.
func (m *DeviceMgmtManager) getState() (*DeviceMgmtState, error) {
	var ms DeviceMgmtState
	err := m.state.Get(deviceMgmtStateKey, &ms)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return &DeviceMgmtState{
				PendingMessages: make(map[string]*PendingMessage),
				Sequences:       make(map[string]*Sequence),
				ReadyResponses:  make(map[string]store.Message),
			}, nil
		}

		return nil, err
	}

	return &ms, nil
}

// setState persists the device management state.
func (m *DeviceMgmtManager) setState(ms *DeviceMgmtState) {
	m.state.Set(deviceMgmtStateKey, ms)
}

// Ensure implements StateManager.Ensure.
func (m *DeviceMgmtManager) Ensure() error { // TODO: add manager-level feature flag?
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	for _, chg := range m.state.Changes() {
		if chg.Kind() == deviceMgmtCycleChangeKind && !chg.Status().Ready() {
			return nil
		}
	}

	ts := state.NewTaskSet()
	var dispatch *state.Task
	for _, msg := range ms.PendingMessages {
		if msg.ChangeID == "" {
			dispatch = m.state.NewTask("dispatch-messages", "Dispatch message(s) to subsystems")
			ts.AddTask(dispatch)
			break
		}
	}

	if m.canPoll(ms.LastPolled) {
		poll := m.state.NewTask("poll-messages", "Poll store for device management messages")
		ts.WaitFor(poll)
		ts.AddTask(poll)
	}

	if len(ts.Tasks()) > 0 {
		chg := m.state.NewChange(deviceMgmtCycleChangeKind, "Process device management messages")
		chg.AddAll(ts)
		chg.Set("dispatch-queued", dispatch != nil)
	}

	return nil
}

// canPoll checks whether polling should happen now based on feature flags and timing.
func (m *DeviceMgmtManager) canPoll(lastPolled time.Time) bool {
	tr := config.NewTransaction(m.state)
	enabled, err := features.Flag(tr, features.MessagePolling)
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("cannot check message-polling feature flag: %v", err)
		return false
	}
	if !enabled {
		return false
	}

	if time.Since(lastPolled) < pollingInterval {
		return false
	}

	return true
}

// doPollMessages polls the store for new request messages, acknowledges receipt
// of persisted messages, and sends queued response messages.
func (m *DeviceMgmtManager) doPollMessages(t *state.Task, tomb *tomb.Tomb) error {
	m.state.Lock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	deviceCtx, err := snapstate.DevicePastSeeding(m.state, nil)
	if err != nil {
		m.state.Unlock()
		return err
	}

	sto := snapstate.Store(m.state, deviceCtx)
	m.state.Unlock()

	messages := make([]store.Message, 0, len(ms.ReadyResponses))
	for _, msg := range ms.ReadyResponses {
		messages = append(messages, msg)
	}

	resp, err := sto.PollMessages(
		tomb.Context(context.Background()),
		&store.PollMessagesRequest{
			After:    ms.PendingAckToken,
			Limit:    pollingLimit,
			Messages: messages,
		},
	)
	if err != nil {
		return err
	}

	for _, msg := range resp.Messages {
		pendingMsg, err := parsePendingMessage(msg.Message)
		if err != nil {
			logger.Noticef("cannot parse message with token %s: %v", msg.Token, err)
			continue
		}

		ms.PendingMessages[pendingMsg.ID()] = pendingMsg
	}

	m.state.Lock()
	defer m.state.Unlock()

	ms.ReadyResponses = make(map[string]store.Message)
	ms.PendingAckToken = ""
	ms.LastPolled = time.Now()

	if len(resp.Messages) > 0 {
		ms.PendingAckToken = resp.Messages[len(resp.Messages)-1].Token

		chg := t.Change()
		var dispatchQueued bool
		chg.Get("dispatch-queued", &dispatchQueued)
		if !dispatchQueued {
			dispatch := m.state.NewTask("dispatch-messages", "Dispatch message(s) to subsystems")
			dispatch.WaitFor(t)
			chg.AddTask(dispatch)
			chg.Set("dispatch-queued", true)
		}
	}

	m.setState(ms)

	return nil
}

// doDispatchMessages selects pending messages for processing & creates validate-message
// tasks for them.
// TODO: handle sequencing - pick messages from sequences that have predecessors applied.
func (m *DeviceMgmtManager) doDispatchMessages(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	chg := t.Change()
	for _, msg := range ms.PendingMessages {
		if msg.ChangeID != "" {
			continue
		}

		validate := m.state.NewTask("validate-message", fmt.Sprintf("Validate message %s", msg.ID()))
		validate.Set("id", msg.ID())
		validate.WaitFor(t)
		chg.AddTask(validate)
	}

	if len(ms.PendingMessages) > 0 {
		m.state.EnsureBefore(0)
	}

	return nil
}

// getMessageHandler retrieves a pending message and its corresponding handler.
// Caller must hold state lock.
func (m *DeviceMgmtManager) getMessageAndHandler(t *state.Task, ms *DeviceMgmtState) (*PendingMessage, MessageHandler, error) {
	var id string
	err := t.Get("id", &id)
	if err != nil {
		return nil, nil, err
	}

	msg, ok := ms.PendingMessages[id]
	if !ok {
		return nil, nil, fmt.Errorf("message %s not found in pending messages", id)
	}

	handler, ok := m.handlers[msg.Kind]
	if !ok {
		return nil, nil, fmt.Errorf("no handler registered for message kind %q", msg.Kind)
	}

	return msg, handler, nil
}

// doValidateMessage performs snapd-level and subsystem-level validation on a message.
// Creates apply-message task if validation succeeds, or queue-response for rejected messages.
// TODO: implement device targeting check
// TODO: implement time constraint checks
// TODO: implement assumes validation
func (m *DeviceMgmtManager) doValidateMessage(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	msg, handler, err := m.getMessageAndHandler(t, ms)
	if err != nil {
		return err
	}

	var next *state.Task
	err = handler.Validate(m.state, msg)
	if err != nil {
		next = m.state.NewTask("queue-response", fmt.Sprintf("Queue response for message %s", msg.ID()))
		next.Set("status", asserts.MessageStatusRejected)
		next.Set("body", map[string]any{"message": err.Error()})
	} else {
		next = m.state.NewTask("apply-message", fmt.Sprintf("Apply message %s", msg.ID()))
	}

	next.Set("id", msg.ID())
	next.WaitFor(t)
	t.Change().AddTask(next)

	m.setState(ms)

	return nil
}

// doApplyMessage dispatches the message to its subsystem handler for processsing
// and creates an await-subsystem-change task to monitor completion.
func (m *DeviceMgmtManager) doApplyMessage(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	msg, handler, err := m.getMessageAndHandler(t, ms)
	if err != nil {
		return err
	}

	subSysChangeID, err := handler.Apply(m.state, msg)
	if err != nil {
		return fmt.Errorf("cannot apply message: %w", err)
	}
	msg.ChangeID = subSysChangeID

	await := m.state.NewTask("await-subsystem-change", fmt.Sprintf("Await %s subsystem change for message %s", msg.Kind, msg.ID()))
	await.Set("id", msg.ID())
	await.WaitFor(t)
	t.Change().AddTask(await)

	m.setState(ms)

	return nil
}

// doAwaitSubsystemChange monitors a subsystem change until completion and creates
// a queue-response task with the processing results.
func (m *DeviceMgmtManager) doAwaitSubsystemChange(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	msg, handler, err := m.getMessageAndHandler(t, ms)
	if err != nil {
		return err
	}

	subSysChange := m.state.Change(msg.ChangeID)
	if subSysChange == nil {
		return fmt.Errorf("subsystem change %s not found", msg.ChangeID)
	}

	if !subSysChange.Status().Ready() {
		return &state.Retry{After: time.Minute}
	}

	respBody, status := handler.BuildResponse(subSysChange)

	queue := m.state.NewTask("queue-response", fmt.Sprintf("Queue response for message %s", msg.ID()))
	queue.Set("id", msg.ID())
	queue.Set("status", status)
	queue.Set("body", respBody)
	queue.WaitFor(t)
	t.Change().AddTask(queue)

	return nil
}

// doQueueResponse signs a response message and queues it for transmission on the next poll.
func (m *DeviceMgmtManager) doQueueResponse(t *state.Task, _ *tomb.Tomb) error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	msg, _, err := m.getMessageAndHandler(t, ms)
	if err != nil {
		return err
	}

	var body map[string]any
	err = t.Get("body", &body)
	if err != nil {
		return err
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("cannot marshal response body: %w", err)
	}

	var status asserts.MessageStatus
	err = t.Get("status", &status)
	if err != nil {
		return err
	}

	resAs, err := m.signer.SignResponseMessage(msg.AccountID, msg.ID(), status, bodyBytes)
	if err != nil {
		return fmt.Errorf("cannot sign response: %w", err)
	}

	ms.ReadyResponses[msg.ID()] = store.Message{Format: "assertion", Data: string(asserts.Encode(resAs))}
	delete(ms.PendingMessages, msg.ID())

	m.setState(ms)

	return nil
}

// parsePendingMessage decodes a store message body into a PendingMessage.
func parsePendingMessage(msg store.Message) (*PendingMessage, error) {
	if msg.Format != "assertion" {
		return nil, fmt.Errorf("unsupported format %s", msg.Format)
	}

	as, err := asserts.Decode([]byte(msg.Data))
	if err != nil {
		return nil, fmt.Errorf("cannot decode assertion: %w", err)
	}

	reqAs, ok := as.(*asserts.RequestMessage)
	if !ok {
		return nil, fmt.Errorf(`assertion is %q, expected "request-message"`, as.Type().Name)
	}

	devices := reqAs.Devices()
	deviceIDs := make([]string, len(devices))
	for i, dev := range devices {
		deviceIDs[i] = dev.String()
	}

	return &PendingMessage{
		Source:      "store",
		BaseID:      reqAs.ID(),
		SeqNum:      reqAs.SeqNum(),
		Kind:        reqAs.Kind(),
		AccountID:   reqAs.AccountID(),
		AuthorityID: reqAs.AuthorityID(),
		Devices:     deviceIDs,
		ValidSince:  reqAs.ValidSince(),
		ValidUntil:  reqAs.ValidUntil(),
		Body:        string(reqAs.Body()),
		Received:    time.Now(),
	}, nil
}
