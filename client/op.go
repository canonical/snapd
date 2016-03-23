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
	Progress() float64
}

// Operation fetches information about an operation given its UUID
func (client *Client) Operation(uuid string) (Operation, error) {
	var v operation
	err := client.doSync("GET", "/2.0/operations/"+uuid, nil, nil, &v)

	return &v, err
}

type operation struct {
	Status string          `json:"status"`
	Output json.RawMessage `json:"output"`

	ProgressCurrent int `json:"progress_current"`
	ProgressTotal   int `json:"progress_total"`
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

func (op *operation) Progress() float64 {
	if op.ProgressTotal == 0 {
		return 0.0
	}
	return float64(op.ProgressCurrent) / float64(op.ProgressTotal)
}
