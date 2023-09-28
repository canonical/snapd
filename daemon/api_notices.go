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
	"net/http"
	"time"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/notices"
)

var (
	noticesCmd = &Command{
		Path:       "/v2/notices",
		GET:        getNotices,
		ReadAccess: openAccess{},
	}
)

func getNotices(c *Command, r *http.Request, user *auth.UserState) Response {
	query := r.URL.Query()
	var after time.Time
	if s := query.Get("after"); s != "" {
		f, err := time.Parse("2006-01-02T15:04:05", s)
		if err != nil {
			return BadRequest(`invalid value for after: %q: %v`, s, err)
		}
		after = f
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	ns, err := notices.WaitForNotices(st, r.Context(), notices.NoticeFilter{
		After: after,
	})
	if err != nil {
		return InternalError("cannot wait for notices: %v", err)
	}
	return SyncResponse(ns)
}
