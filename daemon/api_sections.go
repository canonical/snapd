// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

package daemon

import (
	"context"
	"net/http"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

var sectionsCmd = &Command{
	Path:       "/v2/sections",
	GET:        getSections,
	ReadAccess: openAccess{},
}

func getSections(c *Command, r *http.Request, user *auth.UserState) Response {
	// TODO: test this
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for snaps")
	}

	theStore := storeFrom(c.d)

	// TODO: use a per-request context
	sections := mylog.Check2(theStore.Sections(context.TODO(), user))
	switch err {
	case nil:
		// pass
	case store.ErrBadQuery:
		return BadQuery()
	case store.ErrUnauthenticated, store.ErrInvalidCredentials:
		return Unauthorized("%v", err)
	default:
		return InternalError("%v", err)
	}

	return SyncResponse(sections)
}
