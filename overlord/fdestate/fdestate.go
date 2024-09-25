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
package fdestate

import (
	"errors"

	"github.com/snapcore/snapd/overlord/state"
)

var errNotImplemented = errors.New("not implemented")

type EFIKeyManagerIdentity struct {
	SnapInstanceName string
}

// EFISecureBootDBManagerStartup indicates that the local EFI key database
// manager has started.
func EFISecureBootDBManagerStartup(st *state.State, id EFIKeyManagerIdentity) error {
	return errNotImplemented
}

type EFISecurebooKeystDB int

const (
	EFISecurebootPK EFISecurebooKeystDB = iota
	EFISecurebootKEK
	EFISecurebootDB
	EFISecurebootDBX
)

// EFISecureBootDBUpdatePrepare notifies notifies that the local EFI key
// database manager is about to update the database.
func EFISecureBootDBUpdatePrepare(st *state.State, id EFIKeyManagerIdentity, db EFISecurebooKeystDB, payload []byte) error {
	return errNotImplemented
}

// EFISecureBootDBUpdateCleanup notifies that the local EFI key database manager
// has reached a cleanup stage of the update process.
func EFISecureBootDBUpdateCleanup(st *state.State, id EFIKeyManagerIdentity) error {
	return errNotImplemented
}
