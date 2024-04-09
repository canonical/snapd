// Copyright (c) 2024 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package client

import (
	"bytes"
	"encoding/json"
)

type NotifyOptions struct {
	// Type is the notice's type. Currently only notices of type CustomNotice
	// can be added.
	Type NoticeType

	// Key is the notice's key. For "custom" notices, this must be in
	// "domain.com/key" format.
	Key string
}

// Notify records an occurrence of a notice with the specified options,
// returning the notice ID.
func (client *Client) Notify(opts *NotifyOptions) (string, error) {
	var payload = struct {
		Action string `json:"action"`
		Type   string `json:"type"`
		Key    string `json:"key"`
	}{
		Action: "add",
		Type:   string(opts.Type),
		Key:    opts.Key,
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(&payload); err != nil {
		return "", err
	}

	result := struct {
		ID string `json:"id"`
	}{}
	_, err := client.doSync("POST", "/v2/notices", nil, nil, &body, &result)
	if err != nil {
		return "", err
	}
	return result.ID, err
}

type NoticeType string

const (
	// SnapRunInhibitNotice is recorded when "snap run" is inhibited due refresh.
	SnapRunInhibitNotice NoticeType = "snap-run-inhibit"
)
