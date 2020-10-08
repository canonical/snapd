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

import "time"

// ConsoleConfStartResponse is the response for starting the console-conf
// routine.
type ConsoleConfStartResponse struct {
	ActiveSnapAutoRefreshChanges []string `json:"active-snap-auto-refreshes,omitempty"`
}

// ConsoleConfStart starts the console-conf start routine.
func (client *Client) ConsoleConfStart() ([]string, error) {
	resp := &ConsoleConfStartResponse{}
	// do the post with a short timeout so that if snapd is not available due to
	// maintenance we will return very quickly so the caller can handle that
	opts := &doOptions{
		Timeout: 2 * time.Second,
	}
	_, err := client.doSyncWithOpts("POST", "/v2/internal/console-conf-start", nil, nil, nil, resp, opts)
	return resp.ActiveSnapAutoRefreshChanges, err
}
