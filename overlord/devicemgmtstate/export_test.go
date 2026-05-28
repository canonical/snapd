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

package devicemgmtstate

import (
	"context"
	"time"

	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
	"gopkg.in/tomb.v2"
)

var (
	DeviceMgmtStateKey = deviceMgmtStateKey

	DefaultExchangeLimit    = defaultExchangeLimit
	DefaultExchangeInterval = defaultExchangeInterval
)

func MockMaxSequences(n int) func() {
	return testutil.Mock(&maxSequences, n)
}

func MockMaxBlockedMessagesPerSequence(n int) func() {
	return testutil.Mock(&maxBlockedMessagesPerSequence, n)
}

type MessageResult = messageResult
type MessageHandler = messageHandler
type RequestMessage = requestMessage

type SequenceState = sequenceState
type DeviceMgmtState = deviceMgmtState

func (m *DeviceMgmtManager) GetState() (*DeviceMgmtState, error) {
	ms, err := m.getState()
	return ms, err
}

func (m *DeviceMgmtManager) SetState(ms *DeviceMgmtState) {
	m.setState(ms)
}

func (m *DeviceMgmtManager) MockHandler(kind string, handler messageHandler) {
	m.handlers[kind] = handler
}

func (m *DeviceMgmtManager) MockSigner(signer responseMessageSigner) {
	m.signer = signer
}

func (m *DeviceMgmtManager) ShouldExchangeMessages(ms *DeviceMgmtState) bool {
	return m.shouldExchangeMessages(ms)
}

func (m *DeviceMgmtManager) DoExchangeMessages(t *state.Task, tomb *tomb.Tomb) error {
	return m.doExchangeMessages(t, tomb)
}

func (m *DeviceMgmtManager) DoDispatchMessages(t *state.Task, tomb *tomb.Tomb) error {
	return m.doDispatchMessages(t, tomb)
}

func (m *DeviceMgmtManager) DoValidateMessage(t *state.Task, tomb *tomb.Tomb) error {
	return m.doValidateMessage(t, tomb)
}

func (m *DeviceMgmtManager) DoApplyMessage(t *state.Task, tomb *tomb.Tomb) error {
	return m.doApplyMessage(t, tomb)
}

func (m *DeviceMgmtManager) DoQueueResponse(t *state.Task, tomb *tomb.Tomb) error {
	return m.doQueueResponse(t, tomb)
}

func ParseRequestMessage(msg store.Message) (*RequestMessage, error) {
	return parseRequestMessage(msg)
}

type ConfdbMessageHandler = confdbMessageHandler

func MockConfdbstateGetView(f func(*state.State, string, string, string) (*confdb.View, error)) func() {
	return testutil.Mock(&confdbstateGetView, f)
}

func MockConfdbstateReadConfdb(f func(context.Context, *state.State, *confdb.View, []string, map[string]any, confdb.Access) (string, error)) func() {
	return testutil.Mock(&confdbstateReadConfdb, f)
}

func MockConfdbstateWriteConfdb(f func(context.Context, *state.State, *confdb.View, map[string]any) (string, error)) func() {
	return testutil.Mock(&confdbstateWriteConfdb, f)
}

func MockTimeNow(t time.Time) func() {
	f := func() time.Time {
		return t
	}

	return testutil.Mock(&timeNow, f)
}
