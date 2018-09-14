// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"bytes"
	"encoding/json"
	"net/url"
	"time"
)

type Warning struct {
	Message     string     `json:"message"`
	FirstAdded  time.Time  `json:"first-added"`
	LastAdded   time.Time  `json:"last-added"`
	LastShown   *time.Time `json:"last-shown,omitempty"`
	ExpireAfter string     `json:"expire-after,omitempty"`
	RepeatAfter string     `json:"repeat-after,omitempty"`
}

type WarningsOptions struct {
	All bool
}

// Warnings are short messages that are meant to alert the user of system events
func (client *Client) Warnings(opts WarningsOptions) ([]*Warning, error) {
	var warnings []*Warning
	q := make(url.Values)
	if opts.All {
		q.Add("select", "all")
	}
	_, err := client.doSync("GET", "/v2/warnings", q, nil, nil, &warnings)
	return warnings, err
}

// Okay asks snapd to chill about the warnings
func (client *Client) Okay(t time.Time) error {
	var body bytes.Buffer
	var op = struct {
		Action    string    `json:"action"`
		Timestamp time.Time `json:"timestamp"`
	}{Action: "okay", Timestamp: t}
	if err := json.NewEncoder(&body).Encode(op); err != nil {
		return err
	}
	_, err := client.doSync("POST", "/v2/warnings", nil, nil, &body, nil)
	return err
}
