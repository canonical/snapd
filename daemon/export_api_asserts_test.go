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

package daemon

import (
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
)

func GetAssertTypeNames(c *Command, r *http.Request, user *auth.UserState) *resp {
	return getAssertTypeNames(c, r, user).(*resp)
}

func DoAssert(c *Command, r *http.Request, user *auth.UserState) *resp {
	return doAssert(c, r, user).(*resp)
}

var (
	AssertsCmd         = assertsCmd
	AssertsFindManyCmd = assertsFindManyCmd
)
