// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
)

// An Authorizer can authorize a request using credentials directly or indirectly available.
type Authorizer interface {
	// AuthAvailable returns true if there are actual authorization
	// credentials available.
	AuthAvailable(dauthCtx DeviceAndAuthContext, user *auth.UserState) (bool, error)

	// Authorize authorizes the given request.
	// If implementing multiple kind of authorization at the same
	// time all they should be performed separately ignoring
	// errors, as the higher-level code might as well treat Authorize
	// as best-effort and only log any returned error.
	Authorize(r *http.Request, dauthCtx DeviceAndAuthContext, user *auth.UserState, opts *AuthorizeOptions) error
}

type AuthorizeOptions struct {
	// DeviceAuth is set if device authorization should be
	// provided if available.
	DeviceAuth bool

	// DeviceAutHeader is set to the header name to use for device
	// authorization.
	DeviceAuthHeader string
}

type DeviceSessionAuthorizer interface {
	Authorizer

	// Ensure a device session using available device credentials.
	EnsureDeviceSession(dauthCtx DeviceAndAuthContext) error
}

type RefreshingAuthorizer interface {
	Authorizer

	// Refresh transient authorization data.
	RefreshAuth(resp http.Response, dauthCtx DeviceAndAuthContext, user *auth.UserState) error
}
