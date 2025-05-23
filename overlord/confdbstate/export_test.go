// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2024 Canonical Ltd
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

package confdbstate

import (
	"time"

	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	ReadDatabag             = readDatabag
	WriteDatabag            = writeDatabag
	GetPlugsAffectedByPaths = getPlugsAffectedByPaths
	CreateChangeConfdbTasks = createChangeConfdbTasks
	CreateLoadConfdbTasks   = createLoadConfdbTasks
	SetWriteTransaction     = setWriteTransaction
	AddReadTransaction      = addReadTransaction
	UnsetOngoingTransaction = unsetOngoingTransaction
)

type (
	ConfdbTransactions = confdbTransactions
)

const (
	CommitEdge  = commitEdge
	ClearTxEdge = clearTxEdge
)

func ChangeViewHandlerGenerator(ctx *hookstate.Context) hookstate.Handler {
	return &changeViewHandler{ctx: ctx}
}

func SaveViewHandlerGenerator(ctx *hookstate.Context) hookstate.Handler {
	return &saveViewHandler{ctx: ctx}
}

func MockReadDatabag(f func(st *state.State, account, confdbName string) (confdb.JSONDatabag, error)) func() {
	old := readDatabag
	readDatabag = f
	return func() {
		readDatabag = old
	}
}

func MockWriteDatabag(f func(st *state.State, databag confdb.JSONDatabag, account, confdbName string) error) func() {
	old := writeDatabag
	writeDatabag = f
	return func() {
		writeDatabag = old
	}
}

func MockEnsureNow(f func(*state.State)) func() {
	old := ensureNow
	ensureNow = f
	return func() {
		ensureNow = old
	}
}

func MockTransactionTimeout(dur time.Duration) func() {
	old := transactionTimeout
	transactionTimeout = dur
	return func() {
		transactionTimeout = old
	}
}
