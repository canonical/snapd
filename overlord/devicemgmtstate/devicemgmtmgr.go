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
	"github.com/snapcore/snapd/store"
)

const (
	deviceMgmtStateKey = "device-mgmt"

	// TODO: Dynamically change this based on # of pending messages & other factors
	pollingLimit    = 10
	pollingInterval = 5 * time.Minute

	// Messages older than this are discarded (prevents unbounded growth)
	maxMessageAge = 24 * time.Hour
)

// MessageHandler processes request-message messages of a specific kind.
// Caller must hold state lock when using this interface.
type MessageHandler interface {
	// Validate checks subsystem-specific constraints (authorization, payload schema, etc).
	// Returns nil if valid, error describing the validation failure otherwise.
	Validate(st *state.State, reqAs *asserts.RequestMessage) error

	// Apply processes a request-message and returns a change ID.
	Apply(st *state.State, reqAs *asserts.RequestMessage) (changeID string, err error)

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
	store.Message

	Received time.Time `json:"received"`
	ChangeID string    `json:"change-id,omitempty"`

	assertion *asserts.RequestMessage
}

// decode decodes and caches the request-message assertion from the message data.
func (m *PendingMessage) decode() error {
	if m.assertion != nil {
		return nil // already decoded
	}

	if m.Format != "assertion" {
		return fmt.Errorf("cannot decode message: unsupported format %s", m.Format)
	}

	as, err := asserts.Decode([]byte(m.Data))
	if err != nil {
		return fmt.Errorf("cannot decode assertion: %w", err)
	}

	reqAs, ok := as.(*asserts.RequestMessage)
	if !ok {
		return fmt.Errorf(`cannot decode message: assertion is %q, expected "request-message"`, as.Type().Name)
	}

	m.assertion = reqAs
	return nil
}

// Sequence tracks the last received and applied sequence numbers for message ordering.
// TODO: implement sequencing and LRU eviction
type Sequence struct {
	// The <random-id> portion of message-id
	BaseID string `json:"base-id"`
	// The highest <N> we've stored
	LastStored int `json:"last-received"`
	// The highest <N> we've successfully processed
	LastApplied int `json:"last-applied"`
}

// DeviceMgmtState holds the persistent state for device management operations.
type DeviceMgmtState struct {
	// PendingMessages maps message tokens to messages being processed. A message
	// stays here from receipt until its response is queued. Messages with ChangeID
	// set are actively being processed by the subsystem's change.
	// TODO: Add cap to PendingMessages queue to prevent unbounded growth.
	PendingMessages map[string]*PendingMessage `json:"pending-messages,omitempty"`

	// Sequence tracking (LRU cache, max 256 entries)
	Sequences map[string]*Sequence `json:"sequences,omitempty"`

	// LastStoredToken is the token of the last message we successfully stored,
	// sent in the "after" field of the next poll request to acknowledge receipt
	// up to this point.
	LastStoredToken string `json:"last-stored-token,omitempty"`

	// ReadyResponses are response-message assertions ready to send in the next poll.
	// After successful transmission, this is cleared.
	ReadyResponses []store.Message `json:"ready-responses,omitempty"`

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
				ReadyResponses:  []store.Message{},
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

	tr := config.NewTransaction(m.state)
	enabled, err := features.Flag(tr, features.MessagePolling)
	if err != nil && !config.IsNoOption(err) {
		return fmt.Errorf("cannot check message-polling feature flag: %s", err)
	}
	if !enabled {
		return nil
	}

	ms, err := m.getState()
	if err != nil {
		return err
	}
	defer m.setState(ms)

	// Clean up stale messages to prevent unbounded growth
	for token, msg := range ms.PendingMessages {
		if time.Since(msg.Received) > maxMessageAge {
			logger.Noticef("discarding stale message (token %s, age %v)", token, time.Since(msg.Received))
			delete(ms.PendingMessages, token)
		}
	}

	var errs []error
	err = m.processCompletedChanges(ms)
	if err != nil {
		logger.Noticef("cannot process completed changes: %v", err)
		errs = append(errs, fmt.Errorf("cannot process completed changes: %w", err))
	}

	if time.Since(ms.LastPolled) > pollingInterval {
		err := m.poll(ms)
		if err != nil {
			logger.Noticef(err.Error())
			errs = append(errs, fmt.Errorf("poll: %w", err))
		}
	}

	err = m.processPendingMessages(ms)
	if err != nil {
		logger.Noticef("cannot process pending messages: %v", err)
		errs = append(errs, fmt.Errorf("process pending: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("device-mgmt: %v", errs)
	}

	return nil
}

// poll fetches pending request-message messages from the store and sends queued response-message messages,
// acknowledging the last received token.
func (m *DeviceMgmtManager) poll(ms *DeviceMgmtState) error {
	deviceCtx, err := snapstate.DevicePastSeeding(m.state, nil)
	if err != nil {
		return err
	}

	req := &store.PollMessagesRequest{
		After:    ms.LastStoredToken,
		Limit:    pollingLimit,
		Messages: ms.ReadyResponses,
	}
	sto := snapstate.Store(m.state, deviceCtx)

	m.state.Unlock()
	resp, err := sto.PollMessages(context.TODO(), req)
	m.state.Lock()
	if err != nil {
		return err
	}

	ms.LastPolled = time.Now()
	ms.LastStoredToken = ""
	ms.ReadyResponses = []store.Message{}

	now := time.Now()
	for _, msg := range resp.Messages {
		_, exists := ms.PendingMessages[msg.Token]
		if exists {
			continue
		}

		ms.PendingMessages[msg.Token] = &PendingMessage{
			Message: store.Message{
				Format: msg.Format,
				Data:   msg.Data,
			},
			Received: now,
		}
	}

	if len(resp.Messages) > 0 {
		lastMsg := resp.Messages[len(resp.Messages)-1]
		ms.LastStoredToken = lastMsg.Token
	}

	return nil
}

func (m *DeviceMgmtManager) getMessageHandler(msg *PendingMessage) (MessageHandler, error) {
	kind := msg.assertion.Kind()
	handler, ok := m.handlers[kind]
	if !ok {
		return nil, fmt.Errorf("cannot get handler: unsupported message kind %q", kind)
	}

	return handler, nil
}

// validateMessage checks SD187 validation requirements.
func (m *DeviceMgmtManager) validateMessage(handler MessageHandler, msg *PendingMessage) error {
	err := m.validateTargeting(msg)
	if err != nil {
		return err
	}

	err = m.validateTimeConstraints(msg)
	if err != nil {
		return err
	}

	err = m.validateAssumes(msg)
	if err != nil {
		return err
	}

	return handler.Validate(m.state, msg.assertion)
}

// validateTargeting checks if this device is targeted by the message.
func (m *DeviceMgmtManager) validateTargeting(_ *PendingMessage) error {
	// TODO: implement device targeting checks
	return nil
}

// validateTimeConstraints checks if the message should be processed at the current time.
func (m *DeviceMgmtManager) validateTimeConstraints(_ *PendingMessage) error {
	// TODO: implement time constraint checks
	return nil
}

// validateAssumes checks if this device meets the message's "assumes" header requirements
func (m *DeviceMgmtManager) validateAssumes(_ *PendingMessage) error {
	// TODO: implement assumes validation
	return nil
}

// processPendingMessages decodes received request-message assertions and initiates their processing.
func (m *DeviceMgmtManager) processPendingMessages(ms *DeviceMgmtState) error {
	for token, msg := range ms.PendingMessages {
		if msg.ChangeID != "" {
			continue
		}

		err := msg.decode()
		if err != nil {
			logger.Noticef("cannot decode message (token %s): %v - deleting", token, err)
			delete(ms.PendingMessages, token)
			continue
		}

		handler, err := m.getMessageHandler(msg)
		if err != nil {
			logger.Noticef("no handler for message (token %s, kind %s): keeping for retry", token, msg.assertion.Kind())
			continue // keep message until maxMessageAge
		}

		err = m.validateMessage(handler, msg)
		if err != nil {
			errorBody := map[string]any{
				"error": map[string]any{"message": err.Error()}, // TODO: pick error code/kind
			}

			err := m.queueResponse(ms, msg, errorBody, asserts.MessageStatusRejected)
			if err != nil {
				logger.Noticef("cannot queue rejection response for message (token %s): %v", token, err)
			}

			delete(ms.PendingMessages, token)
			continue
		}

		changeID, err := handler.Apply(m.state, msg.assertion)
		if err != nil {
			logger.Noticef("error processing message: %v", err)

			errorBody := map[string]any{
				"error": map[string]any{"message": err.Error()}, // TODO: pick error code/kind
			}

			err := m.queueResponse(ms, msg, errorBody, asserts.MessageStatusError)
			if err != nil {
				logger.Noticef("cannot queue error response for message (token %s): %v", token, err)
			}

			delete(ms.PendingMessages, token)
			continue
		}

		msg.ChangeID = changeID

		logger.Noticef("started processing message %s as change %s", token, changeID)
	}

	return nil
}

// processCompletedChanges converts finished message processing into response-message assertions
// ready for transmission on the next poll.
func (m *DeviceMgmtManager) processCompletedChanges(ms *DeviceMgmtState) error {
	for token, msg := range ms.PendingMessages {
		if msg.ChangeID == "" {
			continue
		}

		err := msg.decode()
		if err != nil {
			logger.Noticef("cannot decode message (token %s): %v - deleting", token, err)
			delete(ms.PendingMessages, token)
			continue
		}

		chg := m.state.Change(msg.ChangeID)
		if chg == nil {
			logger.Noticef("change %s disappeared for message (token %s)", msg.ChangeID, token)
			errorBody := map[string]any{
				"error": map[string]any{"message": "processing state lost"},
			}
			if err := m.queueResponse(ms, msg, errorBody, asserts.MessageStatusError); err != nil {
				logger.Noticef("cannot queue error response: %v", err)
			}
			delete(ms.PendingMessages, token)
			continue
		}

		if !chg.Status().Ready() {
			continue // still in progress
		}

		handler, err := m.getMessageHandler(msg)
		if err != nil {
			logger.Noticef("no handler for completed message (token %s, kind %s): keeping for retry", token, msg.assertion.Kind())
			continue // keep message until maxMessageAge
		}

		respBody, status := handler.BuildResponse(chg)

		err = m.queueResponse(ms, msg, respBody, status)
		if err != nil {
			logger.Noticef("cannot queue response for message (token %s): %v", token, err)
		}

		delete(ms.PendingMessages, token)
	}

	return nil
}

// queueResponse marshals a response body, signs it, and adds it to the ready responses queue.
func (m *DeviceMgmtManager) queueResponse(ms *DeviceMgmtState, msg *PendingMessage, respBody map[string]any, status asserts.MessageStatus) error {
	bodyBytes, err := json.Marshal(respBody)
	if err != nil {
		return fmt.Errorf("cannot marshal response body: %w", err)
	}

	resAs, err := m.signer.SignResponseMessage(
		msg.assertion.AccountID(),
		msg.assertion.HeaderString("message-id"),
		status,
		bodyBytes,
	)
	if err != nil {
		return fmt.Errorf("cannot sign response: %w", err)
	}

	ms.ReadyResponses = append(ms.ReadyResponses, store.Message{
		Format: "assertion",
		Data:   string(asserts.Encode(resAs)),
	})

	return nil
}
