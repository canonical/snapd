// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package store

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

var (
	// ErrBadQuery is returned from Find when the query has special characters in strange places.
	ErrBadQuery = errors.New("bad query")

	// ErrSnapNotFound is returned when a snap can not be found
	ErrSnapNotFound = errors.New("snap not found")

	// ErrSnapNotFoundInGivenContext is returned when the snap exists
	// but not in the given context (e.g. in a different channel/track)
	ErrSnapNotFoundInGivenContext = errors.New("snap not found in given context")

	// ErrUnauthenticated is returned when authentication is needed to complete the query
	ErrUnauthenticated = errors.New("you need to log in first")

	// ErrAuthenticationNeeds2fa is returned if the authentication needs 2factor
	ErrAuthenticationNeeds2fa = errors.New("two factor authentication required")

	// Err2faFailed is returned when 2fa failed (e.g., a bad token was given)
	Err2faFailed = errors.New("two factor authentication failed")

	// ErrInvalidCredentials is returned on login error
	// It can also be returned when refreshing the discharge
	// macaroon if the user has changed their password.
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrTOSNotAccepted is returned when the user has not accepted the store's terms of service.
	ErrTOSNotAccepted = errors.New("terms of service not accepted")

	// ErrNoPaymentMethods is returned when the user has no valid payment methods associated with their account.
	ErrNoPaymentMethods = errors.New("no payment methods")

	// ErrPaymentDeclined is returned when the user's payment method was declined by the upstream payment provider.
	ErrPaymentDeclined = errors.New("payment declined")

	// ErrLocalSnap is returned when an operation that only applies to snaps that come from a store was attempted on a local snap.
	ErrLocalSnap = errors.New("cannot perform operation on local snap")

	// ErrNoUpdateAvailable is returned when an update is attempetd for a snap that has no update available.
	ErrNoUpdateAvailable = errors.New("snap has no updates available")
)

// DownloadError represents a download error
type DownloadError struct {
	Code int
	URL  *url.URL
}

func (e *DownloadError) Error() string {
	return fmt.Sprintf("received an unexpected http response code (%v) when trying to download %s", e.Code, e.URL)
}

// PasswordPolicyError is returned in a few corner cases, most notably
// when the password has been force-reset.
type PasswordPolicyError map[string]stringList

func (e PasswordPolicyError) Error() string {
	var msg string

	if reason, ok := e["reason"]; ok && len(reason) == 1 {
		msg = reason[0]
		if location, ok := e["location"]; ok && len(location) == 1 {
			msg += "\nTo address this, go to: " + location[0] + "\n"
		}
	} else {
		for k, vs := range e {
			msg += fmt.Sprintf("%s: %s\n", k, strings.Join(vs, "  "))
		}
	}

	return msg
}

// InvalidAuthDataError signals that the authentication data didn't pass validation.
type InvalidAuthDataError map[string]stringList

func (e InvalidAuthDataError) Error() string {
	var es []string
	for _, v := range e {
		es = append(es, v...)
	}
	// XXX: confirm with server people that extra args are all
	//      full sentences (with periods and capitalization)
	//      (empirically this checks out)
	return strings.Join(es, "  ")
}
