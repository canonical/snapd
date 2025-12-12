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

package devicemgmtstate

import (
	"time"

	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/hookstate"
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

type ExchangeConfig exchangeConfig

func (m *DeviceMgmtManager) GetState() (*DeviceMgmtState, error) {
	return m.getState()
}

func (m *DeviceMgmtManager) SetState(ms *DeviceMgmtState) {
	m.setState(ms)
}

func (m *DeviceMgmtManager) MockHandler(kind string, handler MessageHandler) {
	m.handlers[kind] = handler
}

func (m *DeviceMgmtManager) MockSigner(signer ResponseMessageSigner) {
	m.signer = signer
}

func (m *DeviceMgmtManager) ShouldExchangeMessages(ms *DeviceMgmtState) (bool, exchangeConfig) {
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

func ParsePendingMessage(msg store.Message) (*PendingMessage, error) {
	return parsePendingMessage(msg)
}

func MockTimeNow(t time.Time) func() {
	f := func() time.Time {
		return t
	}

	return testutil.Mock(&timeNow, f)
}

func MockConfdbstateGetView(f func(*state.State, string, string, string) (*confdb.View, error)) func() {
	return testutil.Mock(&confdbstateGetView, f)
}

func MockConfdbstateLoadConfdbAsync(f func(*state.State, *confdb.View, []string) (string, error)) func() {
	return testutil.Mock(&confdbstateLoadConfdbAsync, f)
}

func MockConfdbstateGetTransactionToSet(f func(*hookstate.Context, *state.State, *confdb.View) (*confdbstate.Transaction, confdbstate.CommitTxFunc, error)) func() {
	return testutil.Mock(&confdbstateGetTransactionToSet, f)
}

func MockConfdbstateSetViaView(f func(confdb.Databag, *confdb.View, map[string]any) error) func() {
	return testutil.Mock(&confdbstateSetViaView, f)
}
