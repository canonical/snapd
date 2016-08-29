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
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/snapcore/snapd/overlord/hookstate"
)

type snapctlOutput struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
}

// RunSnapctl requests a snapctl run for the given options.
func (client *Client) RunSnapctl(options hookstate.SnapCtlRequest) (stdout, stderr []byte, err error) {
	b, err := json.Marshal(options)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot marshal options: %s", err)
	}

	var output snapctlOutput
	_, err = client.doSync("POST", "/v2/snapctl", nil, nil, bytes.NewReader(b), &output)
	if err != nil {
		return nil, nil, fmt.Errorf("cannot run snapctl: %s", err)
	}

	return []byte(output.Stdout), []byte(output.Stderr), nil
}
