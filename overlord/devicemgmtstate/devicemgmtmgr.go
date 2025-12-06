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
// for message-based remote device management. It receives signed request-message
// assertions from the store via periodic message exchanges, validates them against
// SD187 requirements, dispatches them to subsystem-specific handlers (like confdb),
// and sends back response-message assertions with processing results.
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

	defaultExchangeLimit    = 10
	defaultExchangeInterval = 5 * time.Minute

	awaitSubsystemRetryInterval = 30 * time.Second
)

var (
	timeNow = time.Now

	deviceMgmtCycleChangeKind = swfeats.RegisterChangeKind("device-management-cycle")
)

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
	BaseID      string    `json:"base-id"`
	SeqNum      int       `json:"seq-num"`
	Kind        string    `json:"kind"`
	AccountID   string    `json:"account-id"`
	AuthorityID string    `json:"authority-id"`
	Source      string    `json:"source"`
	Received    time.Time `json:"received"`

	Devices    []string  `json:"devices"`
	ValidSince time.Time `json:"valid-since"`
	ValidUntil time.Time `json:"valid-until"`

	Body string `json:"body"`

	ChangeID        string `json:"change-id,omitempty"`        // Set when subsystem change is created
	ValidationError string `json:"validation-error,omitempty"` // Set when validation fails
	ApplyError      string `json:"apply-error,omitempty"`      // Set when apply fails
}

func (msg *PendingMessage) ID() string {
	if msg.SeqNum != 0 {
		return fmt.Sprintf("%s-%d", msg.BaseID, msg.SeqNum)
	}

	return msg.BaseID
}

// exchangeConfig holds parameters for the message exchange task.
type exchangeConfig struct {
	// Limit is the maximum number of request messages to fetch.
	Limit int
}

// DeviceMgmtState holds the persistent state for device management operations.
type DeviceMgmtState struct {
	// PendingMessages maps message IDs to messages being processed. A message
	// stays here from receipt until its response is queued. Messages with ChangeID
	// set are actively being processed by the subsystem's change.
	// TODO: add cap to PendingMessages queue to prevent unbounded growth.
	PendingMessages map[string]*PendingMessage `json:"pending-messages"`

	// PendingAckToken is the token of the last message we successfully stored,
	// sent in the "after" field of the next exchange to acknowledge receipt
	// up to this point.
	PendingAckToken string `json:"pending-ack-token"`

	// ReadyResponses are response-message assertions ready to send in the next exchange.
	// Cleared after successful transmission.
	ReadyResponses map[string]store.Message `json:"ready-responses"`

	// LastExchange is the timestamp of the last message exchange.
	LastExchange time.Time `json:"last-exchange"`
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

	m.handlers["confdb"] = &ConfdbMessageHandler{}

	runner.AddHandler("exchange-messages", m.doExchangeMessages, nil)
	runner.AddHandler("dispatch-messages", m.doDispatchMessages, nil)
	runner.AddHandler("validate-message", m.doValidateMessage, nil)
	runner.AddHandler("apply-message", m.doApplyMessage, nil)
	runner.AddHandler("queue-response", m.doQueueResponse, nil)

	return m
}

// getState retrieves the current device management state, initializing if not present.
func (m *DeviceMgmtManager) getState() (*DeviceMgmtState, error) {
	var ms DeviceMgmtState
	err := m.state.Get(deviceMgmtStateKey, &ms)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return &DeviceMgmtState{
				PendingMessages: make(map[string]*PendingMessage),
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
func (m *DeviceMgmtManager) Ensure() error {
	m.state.Lock()
	defer m.state.Unlock()

	ms, err := m.getState()
	if err != nil {
		return err
	}

	shouldExchg, exchgCfg := m.shouldExchangeMessages(ms)
	if !shouldExchg {
		return nil
	}

	// For now, only one device management change can be in flight at any given time.
	for _, chg := range m.state.Changes() {
		if chg.Kind() == deviceMgmtCycleChangeKind && !chg.Status().Ready() {
			return nil
		}
	}

	chg := m.state.NewChange(deviceMgmtCycleChangeKind, "Process device management messages")

	exchg := m.state.NewTask("exchange-messages", "Exchange messages with the Store")
	exchg.Set("config", exchgCfg)
	chg.AddTask(exchg)

	dispatch := m.state.NewTask("dispatch-messages", "Dispatch message(s) to subsystems")
	dispatch.WaitFor(exchg)
	chg.AddTask(dispatch)

	return nil
}

// shouldExchangeMessages checks whether a message exchange should happen now.
// Caller must hold state lock.
func (m *DeviceMgmtManager) shouldExchangeMessages(ms *DeviceMgmtState) (bool, exchangeConfig) {
	tr := config.NewTransaction(m.state)
	enabled, err := features.Flag(tr, features.RemoteDeviceManagement)
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("cannot check remote-device-management feature flag: %v", err)
		return false, exchangeConfig{}
	}

	nextExchange := ms.LastExchange.Add(defaultExchangeInterval)
	if timeNow().Before(nextExchange) {
		return false, exchangeConfig{}
	}

	shouldExchange := enabled || len(ms.ReadyResponses) > 0
	limit := 0
	if enabled {
		limit = defaultExchangeLimit
	}

	return shouldExchange, exchangeConfig{Limit: limit}
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

	var cfg exchangeConfig
	err = t.Get("config", &cfg)
	if err != nil {
		return err
	}

	messages := make([]store.Message, 0, len(ms.ReadyResponses))
	for _, msg := range ms.ReadyResponses {
		messages = append(messages, msg)
	}

	m.state.Unlock()
	resp, err := sto.ExchangeMessages(
		tomb.Context(context.Background()),
		&store.MessageExchangeRequest{
			After:    ms.PendingAckToken,
			Limit:    cfg.Limit,
			Messages: messages,
		},
	)
	m.state.Lock()
	if err != nil {
		return err
	}

	m.processExchangeResponse(ms, resp)
	m.setState(ms)

	return nil
}

// processExchangeResponse updates local state based on the message exchange response.
// Caller must hold state lock.
func (m *DeviceMgmtManager) processExchangeResponse(ms *DeviceMgmtState, resp *store.MessageExchangeResponse) {
	for _, msg := range resp.Messages {
		pendingMsg, err := parsePendingMessage(msg.Message)
		if err != nil {
			logger.Noticef("cannot parse message with token %s: %v", msg.Token, err)
			continue
		}

		_, exists := ms.PendingMessages[pendingMsg.ID()]
		if !exists {
			ms.PendingMessages[pendingMsg.ID()] = pendingMsg
		}
	}

	if len(resp.Messages) > 0 {
		ms.PendingAckToken = resp.Messages[len(resp.Messages)-1].Token
	} else {
		ms.PendingAckToken = ""
	}

	ms.ReadyResponses = make(map[string]store.Message)
	ms.LastExchange = timeNow()
}

// doDispatchMessages selects pending messages for processing & queues tasks for them.
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

		lane := m.state.NewLane()

		validate := m.state.NewTask("validate-message", fmt.Sprintf("Validate message %s", msg.ID()))
		validate.Set("id", msg.ID())
		validate.WaitFor(t)
		validate.JoinLane(lane)
		chg.AddTask(validate)

		apply := m.state.NewTask("apply-message", fmt.Sprintf("Apply message %s", msg.ID()))
		apply.Set("id", msg.ID())
		apply.WaitFor(validate)
		apply.JoinLane(lane)
		chg.AddTask(apply)

		queue := m.state.NewTask("queue-response", fmt.Sprintf("Queue response for message %s", msg.ID()))
		queue.Set("id", msg.ID())
		queue.WaitFor(apply)
		queue.JoinLane(lane)
		chg.AddTask(queue)
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

	err = handler.Validate(m.state, msg)
	if err != nil {
		msg.ValidationError = err.Error()
	}

	m.setState(ms)

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

	msg, handler, err := m.getMessageAndHandler(t, ms)
	if err != nil {
		return err
	}

	if msg.ValidationError != "" {
		return nil // No-op if validation failed
	}

	subSysChangeID, err := handler.Apply(m.state, msg)
	if err != nil {
		msg.ApplyError = fmt.Sprintf("cannot apply message: %v", err)
	} else {
		msg.ChangeID = subSysChangeID
	}

	m.setState(ms)

	return nil
}

// doQueueResponse builds a response, signs it, and queues it for transmission on the next exchange.
// Retries until subsystem change completes.
func (m *DeviceMgmtManager) doQueueResponse(t *state.Task, _ *tomb.Tomb) error {
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

	body, status, err := m.buildResponseBody(msg, handler)
	if err != nil {
		return err
	}

	resAs, err := m.signer.SignResponseMessage(msg.AccountID, msg.ID(), status, body)
	if err != nil {
		return fmt.Errorf("cannot sign response: %w", err)
	}

	ms.ReadyResponses[msg.ID()] = store.Message{
		Format: "assertion",
		Data:   string(asserts.Encode(resAs)),
	}
	delete(ms.PendingMessages, msg.ID())

	m.setState(ms)

	return nil
}

// buildResponseBody creates a response body for a message.
// Caller must hold state lock.
func (m *DeviceMgmtManager) buildResponseBody(msg *PendingMessage, handler MessageHandler) ([]byte, asserts.MessageStatus, error) {
	var body map[string]any
	var status asserts.MessageStatus

	if msg.ValidationError != "" {
		status = asserts.MessageStatusRejected
		body = map[string]any{"message": msg.ValidationError}
	} else if msg.ApplyError != "" {
		status = asserts.MessageStatusError
		body = map[string]any{"message": msg.ApplyError}
	} else {
		subSysChange := m.state.Change(msg.ChangeID)
		if subSysChange == nil {
			return nil, "", fmt.Errorf("subsystem change %s not found", msg.ChangeID)
		}

		if !subSysChange.Status().Ready() {
			return nil, "", &state.Retry{After: awaitSubsystemRetryInterval}
		}

		body, status = handler.BuildResponse(subSysChange)
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("cannot marshal response body: %w", err)
	}

	return bodyBytes, status, nil
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
		Received:    timeNow(),
	}, nil
}
