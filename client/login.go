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
	"os/user"
	"path/filepath"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
)

// User holds logged in user information.
type User struct {
	ID       int    `json:"id,omitempty"`
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`

	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
}

type loginData struct {
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`
	Otp      string `json:"otp,omitempty"`
}

// Login logs user in.
func (client *Client) Login(email, password, otp string) (*User, error) {
	postData := loginData{
		Email:    email,
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

// LoggedInUser returns the logged in User or nil
func (client *Client) LoggedInUser() *User {
	u, err := readAuthData()
	if err != nil {
		return nil
	}
	return u
}

const authFileEnvKey = "SNAPD_AUTH_DATA_FILENAME"

func storeAuthDataFilename(homeDir string) string {
	if fn := os.Getenv(authFileEnvKey); fn != "" {
		return fn
	}

	if homeDir == "" {
		real, err := osutil.UserMaybeSudoUser()
		if err != nil {
			panic(err)
		}
		homeDir = real.HomeDir
	}

	return filepath.Join(homeDir, ".snap", "auth.json")
}

// realUidGid finds the real user when the command is run
// via sudo. It returns the users record and uid,gid.
func realUidGid() (*user.User, sys.UserID, sys.GroupID, error) {
	real, err := osutil.UserMaybeSudoUser()
	if err != nil {
		return nil, 0, 0, err
	}

	uid, gid, err := osutil.UidGid(real)
	if err != nil {
		return nil, 0, 0, err
	}
	return real, uid, gid, err
}

// writeAuthData saves authentication details for later reuse through ReadAuthData
func writeAuthData(user User) error {
	real, uid, gid, err := realUidGid()
	if err != nil {
		return err
	}

	targetFile := storeAuthDataFilename(real.HomeDir)

	out, err := json.Marshal(user)
	if err != nil {
		return err
	}

	return sys.RunAsUidGid(uid, gid, func() error {
		if err := os.MkdirAll(filepath.Dir(targetFile), 0700); err != nil {
			return err
		}

		return osutil.AtomicWriteFile(targetFile, out, 0600, 0)
	})
}

// readAuthData reads previously written authentication details
func readAuthData() (*User, error) {
	_, uid, _, err := realUidGid()
	if err != nil {
		return nil, err
	}

	var user User
	sourceFile := storeAuthDataFilename("")

	if err := sys.RunAsUidGid(uid, sys.FlagID, func() error {
		f, err := os.Open(sourceFile)
		if err != nil {
			return err
		}
		defer f.Close()

		dec := json.NewDecoder(f)

		return dec.Decode(&user)
	}); err != nil {
		return nil, err
	}

	return &user, nil
}

// removeAuthData removes any previously written authentication details.
func removeAuthData() error {
	_, uid, _, err := realUidGid()
	if err != nil {
		return err
	}

	filename := storeAuthDataFilename("")

	return sys.RunAsUidGid(uid, sys.FlagID, func() error {
		return os.Remove(filename)
	})
}
