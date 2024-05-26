// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package client

import (
	"time"

	"github.com/ddkwork/golibrary/mylog"
)

// InternalConsoleConfStartResponse is the response from console-conf start
// support
type InternalConsoleConfStartResponse struct {
	ActiveAutoRefreshChanges []string `json:"active-auto-refreshes,omitempty"`
	ActiveAutoRefreshSnaps   []string `json:"active-auto-refresh-snaps,omitempty"`
}

// InternalConsoleConfStart invokes the dedicated console-conf start support
// to handle intervening auto-refreshes.
// Not for general use.
func (client *Client) InternalConsoleConfStart() ([]string, []string, error) {
	resp := &InternalConsoleConfStartResponse{}
	// do the post with a short timeout so that if snapd is not available due to
	// maintenance we will return very quickly so the caller can handle that
	opts := &doOptions{
		Timeout: 2 * time.Second,
		Retry:   1 * time.Hour,
	}
	_ := mylog.Check2(client.doSyncWithOpts("POST", "/v2/internal/console-conf-start", nil, nil, nil, resp, opts))
	return resp.ActiveAutoRefreshChanges, resp.ActiveAutoRefreshSnaps, err
}
