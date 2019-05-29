// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"context"
)

type userAgentContextKey struct{}

// ClientUserAgentContext carries the client user agent that talks to snapd
func WithClientUserAgent(parent context.Context, ua string) context.Context {
	return context.WithValue(parent, userAgentContextKey{}, ua)
}

// ClientUserAgent returns the user agent of the client that talks to snapd
func ClientUserAgent(ctx context.Context) string {
	ua, ok := ctx.Value(userAgentContextKey{}).(string)
	if ok {
		return ua
	}
	return ""
}
