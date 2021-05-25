// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"runtime"
	"strings"

	"github.com/snapcore/snapd/overlord/auth"
)

func getDebugStacktrace(c *Command, r *http.Request, user *auth.UserState) Response {
	stack := make([]byte, 65535)
	all := true
	n := runtime.Stack(stack, all)
	data := strings.Split(string(stack[:n]), "\n")
	return SyncResponse(data, nil)
}
