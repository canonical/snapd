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

// ErrorKind distinguishes kind of errors.
type ErrorKind string

// error kind const value doc comments here have a non-default,
// specialized style (to help docs/error-kind.go):
//
// // ErrorKind...: DESCRIPTION .
//
// Note the mandatory dot at the end.
// `code-like` quoting should be used when meaningful.

// Error kinds. Keep https://forum.snapcraft.io/t/using-the-rest-api/18603#heading--errors in sync using doc/error-kinds.go.
const (
	// ErrorKindTwoFactorRequired: the client needs to retry the
	// `login` command including an OTP.
	ErrorKindTwoFactorRequired ErrorKind = "two-factor-required"
	// ErrorKindTwoFactorFailed: the OTP provided wasn't recognised.
	ErrorKindTwoFactorFailed ErrorKind = "two-factor-failed"
	// ErrorKindLoginRequired: the requested operation cannot be
	// performed without an authenticated user. This is the kind
	// of any other 401 Unauthorized response.
	ErrorKindLoginRequired ErrorKind = "login-required"
	// ErrorKindInvalidAuthData: the authentication data provided
	// failed to validate (e.g. a malformed email address). The
	// `value` of the error is an object with a key per failed field
	// and a list of the failures on each field.
	ErrorKindInvalidAuthData ErrorKind = "invalid-auth-data"
	// ErrorKindPasswordPolicy: provided password doesn't meet
	// system policy.
	ErrorKindPasswordPolicy ErrorKind = "password-policy"
	// ErrorKindAuthCancelled: authentication was cancelled by the user.
	ErrorKindAuthCancelled ErrorKind = "auth-cancelled"

	// ErrorKindTermsNotAccepted: deprecated, do not document.
	ErrorKindTermsNotAccepted ErrorKind = "terms-not-accepted"
	// ErrorKindNoPaymentMethods: deprecated, do not document.
	ErrorKindNoPaymentMethods ErrorKind = "no-payment-methods"
	// ErrorKindPaymentDeclined: deprecated, do not document.
	ErrorKindPaymentDeclined ErrorKind = "payment-declined"

	// ErrorKindSnapAlreadyInstalled: the requested snap is
	// already installed.
	ErrorKindSnapAlreadyInstalled ErrorKind = "snap-already-installed"
	// ErrorKindSnapNotInstalled: the requested snap is not installed.
	ErrorKindSnapNotInstalled ErrorKind = "snap-not-installed"
	// ErrorKindSnapNotFound: the requested snap couldn't be found.
	ErrorKindSnapNotFound ErrorKind = "snap-not-found"
	// ErrorKindAppNotFound: the requested app couldn't be found.
	ErrorKindAppNotFound ErrorKind = "app-not-found"
	// ErrorKindSnapLocal: cannot perform operation on local snap.
	ErrorKindSnapLocal ErrorKind = "snap-local"
	// ErrorKindSnapNeedsDevMode: the requested snap needs devmode
	// to be installed.
	ErrorKindSnapNeedsDevMode ErrorKind = "snap-needs-devmode"
	// ErrorKindSnapNeedsClassic: the requested snap needs classic
	// confinement to be installed.
	ErrorKindSnapNeedsClassic ErrorKind = "snap-needs-classic"
	// ErrorKindSnapNeedsClassicSystem: the requested snap can't
	// be installed on the current non-classic system.
	ErrorKindSnapNeedsClassicSystem ErrorKind = "snap-needs-classic-system"
	// ErrorKindSnapNotClassic: snap not compatible with classic mode.
	ErrorKindSnapNotClassic ErrorKind = "snap-not-classic"
	// ErrorKindSnapNoUpdateAvailable: the requested snap does not
	// have an update available.
	ErrorKindSnapNoUpdateAvailable ErrorKind = "snap-no-update-available"
	// ErrorKindSnapRevisionNotAvailable: no snap revision available
	// as specified.
	ErrorKindSnapRevisionNotAvailable ErrorKind = "snap-revision-not-available"
	// ErrorKindSnapChannelNotAvailable: no snap revision on specified
	// channel. The `value` of the error is a rich object with
	// requested `snap-name`, `action`, `channel`, `architecture`, and
	// actually available `releases` as list of
	// `{"architecture":... , "channel": ...}` objects.
	ErrorKindSnapChannelNotAvailable ErrorKind = "snap-channel-not-available"
	// ErrorKindSnapArchitectureNotAvailable: no snap revision on
	// specified architecture. Value has the same format as for
	// `snap-channel-not-available`.
	ErrorKindSnapArchitectureNotAvailable ErrorKind = "snap-architecture-not-available"

	// ErrorKindSnapChangeConflict: the requested operation would
	// conflict with currently ongoing change. This is a temporary
	// error. The error `value` is an object with optional fields
	// `snap-name`, `change-kind` of the ongoing change.
	ErrorKindSnapChangeConflict ErrorKind = "snap-change-conflict"

	// ErrorKindQuotaChangeConflict: the requested operation would
	// conflict with a currently ongoing change affecting the quota
	// group. This is a temporary error. The error `value` is an
	// object with optional fields `quota-name`, `change-kind` of the
	// ongoing change.
	ErrorKindQuotaChangeConflict ErrorKind = "quota-change-conflict"

	// ErrorKindNotSnap: the given snap or directory does not
	// look like a snap.
	ErrorKindNotSnap ErrorKind = "snap-not-a-snap"

	// ErrorKindInterfacesUnchanged: the requested interfaces'
	// operation would have no effect.
	ErrorKindInterfacesUnchanged ErrorKind = "interfaces-unchanged"

	// ErrorKindBadQuery: a bad query was provided.
	ErrorKindBadQuery ErrorKind = "bad-query"
	// ErrorKindConfigNoSuchOption: the given configuration option
	// does not exist.
	ErrorKindConfigNoSuchOption ErrorKind = "option-not-found"

	// ErrorKindAssertionNotFound: assertion can not be found.
	ErrorKindAssertionNotFound ErrorKind = "assertion-not-found"

	// ErrorKindUnsuccessful: snapctl command was unsuccessful.
	ErrorKindUnsuccessful ErrorKind = "unsuccessful"

	// ErrorKindNetworkTimeout: a timeout occurred during the request.
	ErrorKindNetworkTimeout ErrorKind = "network-timeout"

	// ErrorKindDNSFailure: DNS not responding.
	ErrorKindDNSFailure ErrorKind = "dns-failure"

	// ErrorKindInsufficientDiskSpace: not enough disk space to perform the request.
	ErrorKindInsufficientDiskSpace ErrorKind = "insufficient-disk-space"

	// ErrorKindValidationSetNotFound: validation set cannot be found.
	ErrorKindValidationSetNotFound ErrorKind = "validation-set-not-found"

	// ErrorKindBusySnap: cannot do action as snap is currently busy.
	ErrorKindBusySnap ErrorKind = "busy"
)

// Maintenance error kinds.
// These are used only inside the maintenance field of responses.
// Keep https://forum.snapcraft.io/t/using-the-rest-api/18603#heading--maint-errors in sync using doc/error-kinds.go.
const (
	// ErrorKindDaemonRestart: daemon is restarting.
	ErrorKindDaemonRestart ErrorKind = "daemon-restart"
	// ErrorKindSystemRestart: system is restarting.
	ErrorKindSystemRestart ErrorKind = "system-restart"
)
