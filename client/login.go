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
	"os"
	"path/filepath"
	"strconv"

	"github.com/snapcore/snapd/osutil"
)

// User holds logged in user information.
type User struct {
	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
}

type loginData struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Otp      string `json:"otp,omitempty"`
}

// Login logs user in.
func (client *Client) Login(username, password, otp string) (*User, error) {
	postData := loginData{
		Username: username,
		Password: password,
		Otp:      otp,
	}
	var body bytes.Buffer
	if err := json.NewEncoder(&body).Encode(postData); err != nil {
		return nil, err
	}

	var user User
	if _, err := client.doSync("POST", "/v2/login", nil, nil, &body, &user); err != nil {
		return nil, err
	}

	if err := writeAuthData(user); err != nil {
		return nil, fmt.Errorf("cannot persist login information: %v", err)
	}
	return &user, nil
}

// Logout logs the user out.
func (client *Client) Logout() error {
	_, err := client.doSync("POST", "/v2/logout", nil, nil, nil, nil)
	if err != nil {
		return err
	}
	return removeAuthData()
}

// LoggedIn returns whether the client has authentication data available.
func (client *Client) LoggedIn() bool {
	return osutil.FileExists(storeAuthDataFilename())
}

func storeAuthDataFilename() string {
	real, err := osutil.RealUser()
	if err != nil {
		panic(err)
	}

	return storeAuthDataFilenameFromHome(real.HomeDir)
}

const authFileEnvKey = "SNAPPY_STORE_AUTH_DATA_FILENAME"

func storeAuthDataFilenameFromHome(homeDir string) string {
	if fn := os.Getenv(authFileEnvKey); fn != "" {
		return fn
	}

	return filepath.Join(homeDir, ".snap", "auth.json")
}

// writeAuthData saves authentication details for later reuse through ReadAuthData
func writeAuthData(user User) error {
	real, err := osutil.RealUser()
	if err != nil {
		return err
	}

	uid, err := strconv.Atoi(real.Uid)
	if err != nil {
		return err
	}

	gid, err := strconv.Atoi(real.Gid)
	if err != nil {
		return err
	}

	targetFile := storeAuthDataFilenameFromHome(real.HomeDir)

	if err := osutil.MkdirAllChown(filepath.Dir(targetFile), 0700, uid, gid); err != nil {
		return err
	}

	outStr, err := json.Marshal(user)
	if err != nil {
		return nil
	}

	return osutil.AtomicWriteFileChown(targetFile, []byte(outStr), 0600, 0, uid, gid)
}

// readAuthData reads previously written authentication details
func readAuthData() (*User, error) {
	sourceFile := storeAuthDataFilename()
	f, err := os.Open(sourceFile)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var user User
	dec := json.NewDecoder(f)
	if err := dec.Decode(&user); err != nil {
		return nil, err
	}

	return &user, nil
}

// removeAuthData removes any previously written authentication details.
func removeAuthData() error {
	filename := storeAuthDataFilename()
	return os.Remove(filename)
}
