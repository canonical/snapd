// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

// Package store has support to use the Ubuntu Store for querying and downloading of snaps, and the related services.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/overlord/auth"
)

func (s *Store) assertionsEndpointURL(p string, query url.Values) *url.URL {
	defBaseURL := s.cfg.StoreBaseURL
	// can be overridden separately!
	if s.cfg.AssertionsBaseURL != nil {
		defBaseURL = s.cfg.AssertionsBaseURL
	}
	return endpointURL(s.baseURL(defBaseURL), path.Join(assertionsPath, p), query)
}

type assertionSvcError struct {
	Status int    `json:"status"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Assertion retrivies the assertion for the given type and primary key.
func (s *Store) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	v := url.Values{}
	v.Set("max-format", strconv.Itoa(assertType.MaxSupportedFormat()))
	u := s.assertionsEndpointURL(path.Join(assertType.Name, path.Join(primaryKey...)), v)

	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: asserts.MediaType,
	}

	var asrt asserts.Assertion

	resp, err := httputil.RetryRequest(reqOptions.URL.String(), func() (*http.Response, error) {
		return s.doRequest(context.TODO(), s.client, reqOptions, user)
	}, func(resp *http.Response) error {
		var e error
		if resp.StatusCode == 200 {
			// decode assertion
			dec := asserts.NewDecoder(resp.Body)
			asrt, e = dec.Decode()
		} else {
			contentType := resp.Header.Get("Content-Type")
			if contentType == jsonContentType || contentType == "application/problem+json" {
				var svcErr assertionSvcError
				dec := json.NewDecoder(resp.Body)
				if e = dec.Decode(&svcErr); e != nil {
					return fmt.Errorf("cannot decode assertion service error with HTTP status code %d: %v", resp.StatusCode, e)
				}
				if svcErr.Status == 404 {
					// best-effort
					headers, _ := asserts.HeadersFromPrimaryKey(assertType, primaryKey)
					return &asserts.NotFoundError{
						Type:    assertType,
						Headers: headers,
					}
				}
				return fmt.Errorf("assertion service error: [%s] %q", svcErr.Title, svcErr.Detail)
			}
		}
		return e
	}, defaultRetryStrategy)

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, respToError(resp, "fetch assertion")
	}

	return asrt, err
}
