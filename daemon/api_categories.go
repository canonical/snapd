// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"net/http"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/store"
)

var categoriesCmd = &Command{
	Path:       "/v2/categories",
	GET:        getCategories,
	ReadAccess: openAccess{},
}

func getCategories(c *Command, r *http.Request, user *auth.UserState) Response {
	route := c.d.router.Get(snapCmd.Path)
	if route == nil {
		return InternalError("cannot find route for categories")
	}

	theStore := storeFrom(c.d)

	categories := mylog.Check2(theStore.Categories(r.Context(), user))
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

	return SyncResponse(categories)
}
