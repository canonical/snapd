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
)

type snaptoolOutput struct {
	Stdout string
	Stderr string
}

// RunSnaptool requests a snaptool run for the given context and arguments.
func (client *Client) RunSnaptool(context string, args []string) (stdout string, stderr string, err error) {
	parameters := map[string]interface{}{
		"context": context,
		"args":    args,
	}

	b, err := json.Marshal(parameters)
	if err != nil {
		return "", "", err
	}

	var output snaptoolOutput
	_, err = client.doSync("POST", "/v2/snaptool", nil, nil, bytes.NewReader(b), &output)
	if err != nil {
		return "", "", err
	}

	return output.Stdout, output.Stderr, nil
}
