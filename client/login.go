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
	"os"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/osutil"
)

// AuthenticatedUser holds logged in user details.
type AuthenticatedUser struct {
	Username   string   `json:"username,omitempty"`
	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
}

// Login logs user in.
func (client *Client) Login(username, password, otp string) (*AuthenticatedUser, error) {
	var user AuthenticatedUser
	body := strings.NewReader(fmt.Sprintf(`{"username":%q,"password":%q,"otp":%q}`, username, password, otp))
	err := client.doSync("POST", "/v2/login", nil, body, &user)
	if err != nil {
		return nil, err
	}
	if err := writeAuthData(user); err != nil {
		return nil, fmt.Errorf("cannot store login information: %v", err)
	}
	return &user, err
}

func storeAuthDataFilename() string {
	homeDir, _ := osutil.CurrentHomeDir()
	return filepath.Join(homeDir, ".snap", "auth.json")
}

func writeAuthData(userData AuthenticatedUser) error {
	targetFile := storeAuthDataFilename()
	if err := os.MkdirAll(filepath.Dir(targetFile), 0750); err != nil {
		return err
	}
	outStr, err := json.Marshal(userData)
	if err != nil {
		return nil
	}

	return osutil.AtomicWriteFile(targetFile, []byte(outStr), 0600, 0)
}
