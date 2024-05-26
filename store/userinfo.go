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

package store

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/httputil"
)

type keysReply struct {
	Username         string   `json:"username"`
	SSHKeys          []string `json:"ssh_keys"`
	OpenIDIdentifier string   `json:"openid_identifier"`
}

type User struct {
	Username         string
	SSHKeys          []string
	OpenIDIdentifier string
}

func (s *Store) UserInfo(email string) (userinfo *User, err error) {
	mylog.Check(
		// most other store network operations use s.endpointURL, which returns an
		// error if the store is offline. this doesn't, so we need to explicitly
		// check.
		s.checkStoreOnline())

	var v keysReply
	ssourl := fmt.Sprintf("%s/keys/%s", authURL(), url.QueryEscape(email))

	resp := mylog.Check2(httputil.RetryRequest(ssourl, func() (*http.Response, error) {
		return s.client.Get(ssourl)
	}, func(resp *http.Response) error {
		if resp.StatusCode != 200 {
			// we recheck the status
			return nil
		}
		dec := json.NewDecoder(resp.Body)
		mylog.Check(dec.Decode(&v))

		return nil
	}, defaultRetryStrategy))

	switch resp.StatusCode {
	case 200: // good
	case 404:
		return nil, fmt.Errorf("cannot find user %q", email)
	default:
		return nil, respToError(resp, fmt.Sprintf("look up user %q", email))
	}

	return &User{
		Username:         v.Username,
		SSHKeys:          v.SSHKeys,
		OpenIDIdentifier: v.OpenIDIdentifier,
	}, nil
}
