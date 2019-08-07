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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type remodelData struct {
	NewModel string `json:"new-model"`
}

// Remodel tries to remodel the system with the given assertion data
func (client *Client) Remodel(b []byte) (changeID string, err error) {
	data, err := json.Marshal(&remodelData{
		NewModel: string(b),
	})
	if err != nil {
		return "", fmt.Errorf("cannot marshal remodel data: %v", err)
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", "/v2/model", nil, headers, bytes.NewReader(data))
}
