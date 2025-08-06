// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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

package optee

import (
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

var (
	newClient = newFDETAClient
)

// FDETAClient represents an interface to our FDE trusted application.
type FDETAClient interface {
	// Present returns true if the FDE TA is present.
	Present() bool

	// DecryptKey requests that the FDE TA decrypt the given key, using the
	// handle as supplimentary information. The decrypted key is returned.
	DecryptKey(input []byte, handle []byte) ([]byte, error)

	// EncryptKey requests that the FDE TA encrypt the given key. A handle and
	// the encrypted key are returned.
	EncryptKey(input []byte) (handle []byte, sealed []byte, err error)

	// Lock requests that the FDE TA be locked. This will prevent it from being
	// used further.
	Lock() error

	// Version returns the version of the FDE TA.
	Version() (int, error)
}

// NewFDETAClient returns a new [FDETAClient].
func NewFDETAClient() FDETAClient {
	return newClient()
}

// MockNewFDETAClient mocks the function called by [NewFDETAClient]. Should only
// be used in tests.
func MockNewFDETAClient(c FDETAClient) (restore func()) {
	osutil.MustBeTestBinary("can only mock optee client in tests")
	return testutil.Mock(&newClient, func() FDETAClient {
		return c
	})
}
