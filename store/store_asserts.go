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
	"io"
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
	// v2 error list - the only field included in v2 error response.
	// XXX: there is an overlap with searchV2Results (and partially with
	// errorListEntry), we could share the definition.
	ErrorList []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error-list"`
}

func (e *assertionSvcError) isNotFound() bool {
	return len(e.ErrorList) > 0 && e.ErrorList[0].Code == "not-found"
}

func (e *assertionSvcError) toError() error {
	// is it v2 error?
	if len(e.ErrorList) > 0 {
		return fmt.Errorf("assertion service error: %q", e.ErrorList[0].Message)
	}
	return fmt.Errorf("unexpected assertion service error")
}

// Assertion retrieves the assertion for the given type and primary key.
func (s *Store) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	v := url.Values{}
	v.Set("max-format", strconv.Itoa(assertType.MaxSupportedFormat()))
	u := s.assertionsEndpointURL(path.Join(assertType.Name, path.Join(primaryKey...)), v)

	var asrt asserts.Assertion

	err := s.downloadAssertions(u, func(r io.Reader) error {
		// decode assertion
		dec := asserts.NewDecoder(r)
		var e error
		asrt, e = dec.Decode()
		return e
	}, func(svcErr *assertionSvcError) error {
		// error-list indicates v2 error response.
		if svcErr.isNotFound() {
			// best-effort
			headers, _ := asserts.HeadersFromPrimaryKey(assertType, primaryKey)
			return &asserts.NotFoundError{
				Type:    assertType,
				Headers: headers,
			}
		}
		// default error
		return nil
	}, "fetch assertion", user)
	if err != nil {
		return nil, err
	}
	return asrt, nil
}

func (s *Store) downloadAssertions(u *url.URL, decodeBody func(io.Reader) error, handleSvcErr func(*assertionSvcError) error, what string, user *auth.UserState) error {
	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: asserts.MediaType,
	}

	resp, err := httputil.RetryRequest(reqOptions.URL.String(), func() (*http.Response, error) {
		return s.doRequest(context.TODO(), s.client, reqOptions, user)
	}, func(resp *http.Response) error {
		var e error
		if resp.StatusCode == 200 {
			e = decodeBody(resp.Body)
		} else {
			contentType := resp.Header.Get("Content-Type")
			if contentType == jsonContentType || contentType == "application/problem+json" {
				var svcErr assertionSvcError
				dec := json.NewDecoder(resp.Body)
				if e = dec.Decode(&svcErr); e != nil {
					return fmt.Errorf("cannot decode assertion service error with HTTP status code %d: %v", resp.StatusCode, e)
				}
				if handleSvcErr != nil {
					if e := handleSvcErr(&svcErr); e != nil {
						return e
					}
				}
				// default error handling
				return svcErr.toError()
			}
		}
		return e
	}, defaultRetryStrategy)

	if err != nil {
		return err
	}

	if resp.StatusCode != 200 {
		return respToError(resp, what)
	}

	return nil
}

// DownloadAssertions download the assertion streams at the given URLs
// and adds their assertions to the given asserts.Batch.
func (s *Store) DownloadAssertions(streamURLs []string, b *asserts.Batch, user *auth.UserState) error {
	for _, ustr := range streamURLs {
		u, err := url.Parse(ustr)
		if err != nil {
			return fmt.Errorf("invalid assertions stream URL: %v", err)
		}

		err = s.downloadAssertions(u, func(r io.Reader) error {
			// decode stream
			_, e := b.AddStream(r)
			return e
		}, nil, "download assertion stream", user)
		if err != nil {
			return err
		}

	}
	return nil
}
