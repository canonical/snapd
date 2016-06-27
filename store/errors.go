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

// ErrBuyParameterMissing is returned when a required parameter is omitted
type ErrBuyParameterMissing struct {
	Name      string
	SnapID    string
	Parameter string
}

func (e *ErrBuyParameterMissing) Error() string {
	identifier := ""
	if e.Name != "" {
		identifier = fmt.Sprintf(" %q", e.Name)
	} else if e.SnapID != "" {
		identifier = fmt.Sprintf(" %q", e.SnapID)
	}

	return fmt.Sprintf("cannot buy snap%s: no %s specified", identifier, e.Parameter)
}

// ErrBadBuyRequest is returned when a bad request (400) response is received from the purchase server
type ErrBadBuyRequest struct {
	Name    string
	Message string
}

func (e *ErrBadBuyRequest) Error() string {
	return fmt.Sprintf("cannot buy snap %q: bad request: %s", e.Name, e.Message)
}

// ErrBuySnapNotFound is returned when a not found (404) response is received from the purchase server
type ErrBuySnapNotFound struct {
	Name string
}

func (e *ErrBuySnapNotFound) Error() string {
	return fmt.Sprintf("cannot buy snap %q: server says not found (snap got removed?)", e.Name)
}

// ErrBuyUnexpectedCode is returned when an unexpected HTTP response code is received from the purchase server
type ErrBuyUnexpectedCode struct {
	Name       string
	StatusCode int
	Message    string
}

func (e *ErrBuyUnexpectedCode) Error() string {
	details := ""
	if e.Message != "" {
		details = ": " + e.Message
	}
	return fmt.Sprintf("cannot buy snap %q: unexpected HTTP code %d%s", e.Name, e.StatusCode, details)
}
