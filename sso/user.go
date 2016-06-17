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

package sso

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
)

// SSOBaseURL is the base url of the SSO service
var SSOBaseURL string

var (
	userLookup = user.Lookup
)

var addUser = func(name string, sshKeys []string) error {
	cmd := exec.Command("adduser", "--gecos", "created by snapd", "--extrausers", "--disabled-password", name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adduser failed with %s: %s", err, output)
	}

	u, err := userLookup(name)
	if err != nil {
		return fmt.Errorf("cannot find user %q: %s", name, err)
	}
	sshDir := filepath.Join(u.HomeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("cannot create %s: %s", sshDir, err)
	}
	authKeys := filepath.Join(sshDir, "authorized_keys")
	authKeysContent := strings.Join(sshKeys, "\n")
	if err := ioutil.WriteFile(authKeys, []byte(authKeysContent), 0644); err != nil {
		return fmt.Errorf("cannot write %s: %s", authKeys, err)
	}

	return nil
}

type keysReply struct {
	Username         string   `json:"username"`
	SshKeys          []string `json:"ssh_keys"`
	OpenIDIdentifier string   `json:"openid_identifier"`
}

func CreateUser(email string) (username string, err error) {
	ssourl := SSOBaseURL
	if ssourl == "" {
		ssourl = os.Getenv("SNAPD_SSO_LOGIN_URL")
		if ssourl == "" {
			ssourl = "https://login.ubuntu.com/"
		}
	}
	ssourl = fmt.Sprintf("%s/api/v2/keys/%s", ssourl, url.QueryEscape(email))

	resp, err := http.Get(ssourl)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var v keysReply
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&v); err != nil {
		return "", fmt.Errorf("cannot unmarshal: %s", err)
	}

	if err := addUser(v.Username, v.SshKeys); err != nil {
		return v.Username, err
	}

	return v.Username, nil
}
