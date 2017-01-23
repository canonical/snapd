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
	"time"

	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/httputil"
)

var (
	httpClient = httputil.NewHTTPClient(&httputil.ClientOpts{
		Timeout:    10 * time.Second,
		MayLogBody: true,
	})
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

func UserInfo(email string) (userinfo *User, err error) {
	ssourl := fmt.Sprintf("%s/keys/%s", authURL(), url.QueryEscape(email))
	for attempt := retry.Start(defaultRetryStrategy, nil); attempt.Next(); {
		var resp *http.Response
		resp, err = httpClient.Get(ssourl)
		if err != nil {
			if shouldRetryError(attempt, err) {
				continue
			}
			break
		}

		if shouldRetryHttpResponse(attempt, resp) {
			resp.Body.Close()
			continue
		}

		defer resp.Body.Close()

		switch resp.StatusCode {
		case 200: // good
		case 404:
			return nil, fmt.Errorf("cannot find user %q", email)
		default:
			return nil, respToError(resp, fmt.Sprintf("look up user %q", email))
		}

		var v keysReply
		dec := json.NewDecoder(resp.Body)
		if err = dec.Decode(&v); err != nil {
			if shouldRetryError(attempt, err) {
				continue
			}
			return nil, fmt.Errorf("cannot unmarshal: %s", err)
		}

		return &User{
			Username:         v.Username,
			SSHKeys:          v.SSHKeys,
			OpenIDIdentifier: v.OpenIDIdentifier,
		}, nil
	}

	return nil, err
}
