// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025-2026 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/store"
	"gopkg.in/tomb.v2"
)

const (
	deviceMgmtStateKey = "device-mgmt"

	defaultExchangeLimit    = 10
	defaultExchangeInterval = 5 * time.Minute
)

var (
	timeNow = time.Now

	deviceMgmtCycleChangeKind = swfeats.RegisterChangeKind("device-management-cycle")
)

// MessageHandler processes request-message messages of a specific kind.
// Caller must hold state lock when using this interface.
type MessageHandler interface {
	// Validate checks subsystem-specific constraints (authorization, payload schema, etc).
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
	Received    time.Time `json:"received"`

	Devices    []string  `json:"devices"`
	ValidSince time.Time `json:"valid-since"`
	ValidUntil time.Time `json:"valid-until"`

	Body string `json:"body"`
}

// exchangeConfig holds parameters for the message exchange task.
type exchangeConfig struct {
	// Limit is the maximum number of request messages to fetch.
	Limit int
}

// deviceMgmtState holds the persistent state for device management operations.
type deviceMgmtState struct {
	// PendingMessages maps message IDs to messages being processed. A message
	// stays here from receipt until its response is queued.
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
				PendingMessages: make(map[string]*PendingMessage),
				ReadyResponses:  make(map[string]store.Message),
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

	exchg := m.state.NewTask("exchange-mgmt-messages", "Exchange messages with the Store")
	exchg.Set("config", exchgCfg)
	chg.AddTask(exchg)

	dispatch := m.state.NewTask("dispatch-mgmt-messages", "Dispatch message(s) to subsystems")
	dispatch.WaitFor(exchg)
	chg.AddTask(dispatch)

	return nil
}

// shouldExchangeMessages checks whether a message exchange should happen now.
// Caller must hold state lock.
func (m *DeviceMgmtManager) shouldExchangeMessages(ms *deviceMgmtState) (bool, exchangeConfig) {
	nextExchange := ms.LastExchange.Add(defaultExchangeInterval)
	if timeNow().Before(nextExchange) {
		return false, exchangeConfig{}
	}

	tr := config.NewTransaction(m.state)
	enabled, err := features.Flag(tr, features.RemoteDeviceManagement)
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("cannot check remote-device-management feature flag: %v", err)
		// Assume flag is unset but still send responses if there are any
		enabled = false
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
	// TODO: implement this task, no-op for now.
	return nil
}

// doDispatchMessages selects pending messages for processing & queues tasks for them.
func (m *DeviceMgmtManager) doDispatchMessages(t *state.Task, _ *tomb.Tomb) error {
	// TODO: implement this task, no-op for now.
	return nil
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
// Retries until subsystem change completes.
func (m *DeviceMgmtManager) doQueueResponse(t *state.Task, _ *tomb.Tomb) error {
	// TODO: implement this task, no-op for now.
	return nil
}
