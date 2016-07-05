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
)

var (
	// ErrSnapNotFound is returned when a snap can not be found
	ErrSnapNotFound = errors.New("snap not found")

	// ErrAssertionNotFound is returned when an assertion can not be found
	ErrAssertionNotFound = errors.New("assertion not found")

	// ErrAuthenticationNeeds2fa is returned if the authentication needs 2factor
	ErrAuthenticationNeeds2fa = errors.New("two factor authentication required")

	// Err2faFailed is returned when 2fa failed (e.g., a bad token was given)
	Err2faFailed = errors.New("two factor authentication failed")

	// ErrInvalidCredentials is returned on login error
	ErrInvalidCredentials = errors.New("invalid credentials")
)

// ErrDownload represents a download error
type ErrDownload struct {
	Code int
	URL  *url.URL
}

func (e *ErrDownload) Error() string {
	return fmt.Sprintf("received an unexpected http response code (%v) when trying to download %s", e.Code, e.URL)
}
