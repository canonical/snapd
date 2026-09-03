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

// Package handlers defines the message and handler contract shared between
// devicemgmtstate and the subsystem packages (e.g. confdbstate) that implement
// and register handlers for specific message kinds.
package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/state"
)

const MgmtMessageIDKey = "mgmt-message-id"

var registeredHandlers = map[string]MessageHandler{}

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

	// RawAssertion holds the original encoded assertion bytes.
	RawAssertion []byte `json:"raw-assertion"`
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

// MessageHandler processes request messages of a specific kind.
// Caller must hold state lock when using this interface.
type MessageHandler interface {
	// Validate checks subsystem-specific constraints.
	Validate(ctx context.Context, st *state.State, msg *RequestMessage) error

	// Apply creates a change to process the message and returns its ID.
	// Implementations must call MarkChangeForMessage on the created change before
	// releasing the state lock.
	Apply(ctx context.Context, st *state.State, msg *RequestMessage) (changeID string, err error)

	// ResultFromChange reads the completed change and returns the full result.
	ResultFromChange(ctx context.Context, chg *state.Change) (body map[string]any, err error)
}

// UnauthorizedError is returned by MessageHandler.Validate when the operator
// does not have permission to perform the requested action.
type UnauthorizedError struct {
	Operator string
}

func (e *UnauthorizedError) Error() string {
	return fmt.Sprintf("cannot perform action: operator %q is not authorized", e.Operator)
}

// MarkChangeForMessage records the message ID on the change created by an Apply
// implementation. It must be called after change creation and before releasing
// the state lock, so that doApplyMessage can recover the change ID on retry
// and not call the handler's Apply again.
func MarkChangeForMessage(chg *state.Change, msg *RequestMessage) {
	chg.Set(MgmtMessageIDKey, msg.ID())
}

// Register registers a MessageHandler for the given message kind.
func Register(kind string, h MessageHandler) {
	registeredHandlers[kind] = h
}

// Get returns the MessageHandler registered for the given message kind, if any.
func Get(kind string) (MessageHandler, bool) {
	h, ok := registeredHandlers[kind]
	return h, ok
}
