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
	"fmt"
)

// Assert tries to add an assertion to the system assertion
// database. To succeed the assertion must be valid, its signature
// verified with a known public key and the assertion consistent with
// and its prerequisite in the database.
func (client *Client) Assert(b []byte) error {
	var rsp interface{}
	if err := client.doSync("POST", "/2.0/assertions", bytes.NewReader(b), &rsp); err != nil {
		return fmt.Errorf("cannot assert: %v", err)
	}

	return nil
}
