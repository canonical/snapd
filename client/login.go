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

	"github.com/ddkwork/golibrary/mylog"
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
	mylog.Check(json.NewEncoder(&body).Encode(postData))

	var user User
	mylog.Check2(client.doSync("POST", "/v2/login", nil, nil, &body, &user))
	mylog.Check(writeAuthData(user))

	return &user, nil
}

// Logout logs the user out.
func (client *Client) Logout() error {
	_ := mylog.Check2(client.doSync("POST", "/v2/logout", nil, nil, nil, nil))

	return removeAuthData()
}

// LoggedInUser returns the logged in User or nil
func (client *Client) LoggedInUser() *User {
	u := mylog.Check2(readAuthData())

	return u
}

const authFileEnvKey = "SNAPD_AUTH_DATA_FILENAME"

func storeAuthDataFilename(homeDir string) string {
	if fn := os.Getenv(authFileEnvKey); fn != "" {
		return fn
	}

	if homeDir == "" {
		real := mylog.Check2(osutil.UserMaybeSudoUser())

		homeDir = real.HomeDir
	}

	return filepath.Join(homeDir, ".snap", "auth.json")
}

// realUidGid finds the real user when the command is run
// via sudo. It returns the users record and uid,gid.
func realUidGid() (*user.User, sys.UserID, sys.GroupID, error) {
	real := mylog.Check2(osutil.UserMaybeSudoUser())

	uid, gid := mylog.Check3(osutil.UidGid(real))

	return real, uid, gid, err
}

// writeAuthData saves authentication details for later reuse through ReadAuthData
func writeAuthData(user User) error {
	real, uid, gid := mylog.Check4(realUidGid())

	targetFile := storeAuthDataFilename(real.HomeDir)

	out := mylog.Check2(json.Marshal(user))

	return sys.RunAsUidGid(uid, gid, func() error {
		mylog.Check(os.MkdirAll(filepath.Dir(targetFile), 0700))

		return osutil.AtomicWriteFile(targetFile, out, 0600, 0)
	})
}

// readAuthData reads previously written authentication details
func readAuthData() (*User, error) {
	_, uid, _ := mylog.Check4(realUidGid())

	var user User
	sourceFile := storeAuthDataFilename("")
	mylog.Check(sys.RunAsUidGid(uid, sys.FlagID, func() error {
		f := mylog.Check2(os.Open(sourceFile))

		defer f.Close()

		dec := json.NewDecoder(f)

		return dec.Decode(&user)
	}))

	return &user, nil
}

// removeAuthData removes any previously written authentication details.
func removeAuthData() error {
	_, uid, _ := mylog.Check4(realUidGid())

	filename := storeAuthDataFilename("")

	return sys.RunAsUidGid(uid, sys.FlagID, func() error {
		return os.Remove(filename)
	})
}
