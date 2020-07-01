// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package client

// XXX: sync with the error kinds in daemon and ensure we define them
//      only in a single place

// Keep in sync with: https://forum.snapcraft.io/t/17954
const (
	// ErrorKindTwoFactorRequired the client needs to retry the
	// login command including an OTP
	ErrorKindTwoFactorRequired = "two-factor-required"
	// ErrorKindTwoFactorFailed: the OTP provided wasn’t recognised
	ErrorKindTwoFactorFailed = "two-factor-failed"
	// ErrorKindLoginRequired: the requested operation cannot be
	// performed without an authenticated user. This is the kind
	// of any other 401 Unauthorized response.
	ErrorKindLoginRequired = "login-required"
	// ErrorKindInvalidAuthData: the authentication data provided
	// failed to validate (e.g. a malformed email address). The
	// value of the error is an object with a key per failed field
	// and a list of the failures on each field.
	ErrorKindInvalidAuthData = "invalid-auth-data"
	// ErrorKindTermsNotAccepted: deprecated, do not document
	ErrorKindTermsNotAccepted = "terms-not-accepted"
	// ErrorKindNoPaymentMethods: deprecated, do not document
	ErrorKindNoPaymentMethods = "no-payment-methods"
	// ErrorKindPaymentDeclined: deprecated, do not document
	ErrorKindPaymentDeclined = "payment-declined"
	// ErrorKindPasswordPolicy: provided password doesn’t meet
	// system policy.
	ErrorKindPasswordPolicy = "password-policy"

	// ErrorKindSnapAlreadyInstalled: the requested snap is
	// already installed.
	ErrorKindSnapAlreadyInstalled = "snap-already-installed"
	// ErrorKindSnapNotInstalled:  the requested snap is not installed.
	ErrorKindSnapNotInstalled = "snap-not-installed"
	// ErrorKindSnapNotFound: the requested snap couldn’t be found.
	ErrorKindSnapNotFound = "snap-not-found"
	// ErrorKindAppNotFound: the requested app couldn’t be found.
	ErrorKindAppNotFound = "app-not-found"
	// ErrorKindSnapLocal: the requested snap couldn’t be found in
	// the store.
	ErrorKindSnapLocal = "snap-local"
	// ErrorKindSnapNeedsDevMode: the requested snap needs devmode
	// to be installed.
	ErrorKindSnapNeedsDevMode = "snap-needs-devmode"
	// ErrorKindSnapNeedsClassic: the requested snap needs classic
	// confinement to be installed.
	ErrorKindSnapNeedsClassic = "snap-needs-classic"
	// ErrorKindSnapNeedsClassicSystem: the requested snap can’t
	// be installed on the current system.
	ErrorKindSnapNeedsClassicSystem = "snap-needs-classic-system"
	// ErrorKindSnapNotClassic: snap not compatible with classic mode.
	ErrorKindSnapNotClassic = "snap-not-classic"
	// ErrorKindNoUpdateAvailable: the requested snap does not
	// have an update available.
	ErrorKindNoUpdateAvailable = "snap-no-update-available"

	// ErrorKindRevisionNotAvailable: no snap revision available
	// as specified.
	ErrorKindRevisionNotAvailable = "snap-revision-not-available"
	// ErrorKindChannelNotAvailable: no snap revision on specified
	// channel. The value of the error is a rich object with
	// requested snap-name, action, channel, architecture, and
	// actually available releases as list of
	// {"architecture":... , "channel": ...} objects.
	ErrorKindChannelNotAvailable = "snap-channel-not-available"
	// ErrorKindArchitectureNotAvailable: no snap revision on
	// specified architecture. Value has the same format as for
	// snap-channel-not-available:
	ErrorKindArchitectureNotAvailable = "snap-architecture-not-available"

	// ErrorKindChangeConflict: the requested operation would
	// conflict with currently ongoing change. This is a temporary
	// error. The error value is an object with optional fields
	// snap-name, change-kind of the ongoing change.
	ErrorKindChangeConflict = "snap-change-conflict"

	// ErrorKindNotSnap : the given snap or directory does not
	// look like a snap.
	ErrorKindNotSnap = "snap-not-a-snap"

	// ErrorKindNetworkTimeout: a timeout occurred during the request.
	ErrorKindNetworkTimeout = "network-timeout"

	// ErrorKindDNSFailure:
	ErrorKindDNSFailure = "dns-failure"

	// ErrorKindInterfacesUnchanged: the requested interfaces’
	// operation would have no effect.
	ErrorKindInterfacesUnchanged = "interfaces-unchanged"

	// ErrorKindBadQuery: a bad query was provided.
	ErrorKindBadQuery = "bad-query"
	// ErrorKindConfigNoSuchOption: the given configuration option
	// does not exist.
	ErrorKindConfigNoSuchOption = "option-not-found"

	// ErrorKindSystemRestart:
	ErrorKindSystemRestart = "system-restart"
	// ErrorKindDaemonRestart:
	ErrorKindDaemonRestart = "daemon-restart"

	// ErrorKindAssertionNotFound:
	ErrorKindAssertionNotFound = "assertion-not-found"

	// ErrorKindUnsuccessful:
	ErrorKindUnsuccessful = "unsuccessful"

	// XXX: auth-cancelled needs error kind and documentation
)
