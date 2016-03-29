// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"encoding/json"
	"fmt"
)

// An Operation provides information about an asynchronous daemon operation
type Operation interface {
	Running() bool
	Err() error
}

// Operation fetches information about an operation given its UUID
func (client *Client) Operation(uuid string) (Operation, error) {
	var v operation
	err := client.doSync("GET", "/v2/operations/"+uuid, nil, nil, &v)

	return &v, err
}

type operation struct {
	Status string          `json:"status"`
	Output json.RawMessage `json:"output"`
}

func (op *operation) Err() error {
	if op.Status != "failed" {
		return nil
	}

	var res errorResult
	if json.Unmarshal(op.Output, &res) != nil {
		return fmt.Errorf("unexpected error format: %q", op.Output)
	}

	return &res
}

func (op *operation) Running() bool {
	return op.Status == "running"
}
