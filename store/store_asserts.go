// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/overlord/auth"
)

func (s *Store) assertionsEndpointURL(p string, query url.Values) (*url.URL, error) {
	mylog.Check(s.checkStoreOnline())

	defBaseURL := s.cfg.StoreBaseURL
	// can be overridden separately!
	if s.cfg.AssertionsBaseURL != nil {
		defBaseURL = s.cfg.AssertionsBaseURL
	}
	return endpointURL(s.baseURL(defBaseURL), path.Join(assertionsPath, p), query), nil
}

type assertionSvcError struct {
	// v1 error fields
	// XXX: remove once switched to v2 API request.
	Status int    `json:"status"`
	Type   string `json:"type"`
	Title  string `json:"title"`
	Detail string `json:"detail"`

	// v2 error list - the only field included in v2 error response.
	// XXX: there is an overlap with searchV2Results (and partially with
	// errorListEntry), we could share the definition.
	ErrorList []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error-list"`
}

func (e *assertionSvcError) isNotFound() bool {
	return (len(e.ErrorList) > 0 && e.ErrorList[0].Code == "not-found" /* v2 error */) || e.Status == 404
}

func (e *assertionSvcError) toError() error {
	// is it v2 error?
	if len(e.ErrorList) > 0 {
		return fmt.Errorf("assertion service error: %q", e.ErrorList[0].Message)
	}
	// v1 error
	return fmt.Errorf("assertion service error: [%s] %q", e.Title, e.Detail)
}

func (s *Store) setMaxFormat(v url.Values, assertType *asserts.AssertionType) {
	var maxFormat int
	if s.cfg.AssertionMaxFormats == nil {
		maxFormat = assertType.MaxSupportedFormat()
	} else {
		maxFormat = s.cfg.AssertionMaxFormats[assertType.Name]
	}
	v.Set("max-format", strconv.Itoa(maxFormat))
}

// Assertion retrieves the assertion for the given type and primary key.
func (s *Store) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	v := url.Values{}
	s.setMaxFormat(v, assertType)

	u := mylog.Check2(s.assertionsEndpointURL(path.Join(assertType.Name, path.Join(asserts.ReducePrimaryKey(assertType, primaryKey)...)), v))

	var asrt asserts.Assertion
	mylog.Check(s.downloadAssertions(u, func(r io.Reader) error {
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
	}, "fetch assertion", user))

	return asrt, nil
}

// SeqFormingAssertion retrieves the sequence-forming assertion for the given
// type (currently validation-set only). For sequence <= 0 we query for the
// latest sequence, otherwise the latest revision of the given sequence is
// requested.
func (s *Store) SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	if !assertType.SequenceForming() {
		return nil, fmt.Errorf("internal error: requested non sequence-forming assertion type %q", assertType.Name)
	}
	v := url.Values{}
	s.setMaxFormat(v, assertType)

	hasSequenceNumber := sequence > 0
	if hasSequenceNumber {
		// full primary key passed, query specific sequence number.
		v.Set("sequence", fmt.Sprintf("%d", sequence))
	} else {
		// query for the latest sequence.
		v.Set("sequence", "latest")
	}
	u := mylog.Check2(s.assertionsEndpointURL(path.Join(assertType.Name, path.Join(sequenceKey...)), v))

	var asrt asserts.Assertion
	mylog.Check(s.downloadAssertions(u, func(r io.Reader) error {
		// decode assertion
		dec := asserts.NewDecoder(r)
		var e error
		asrt, e = dec.Decode()
		return e
	}, func(svcErr *assertionSvcError) error {
		// error-list indicates v2 error response.
		if svcErr.isNotFound() {
			// XXX: this re-implements asserts.HeadersFromPrimaryKey() but is
			// more relaxed about key length, making sequence optional. Should
			// we make it a helper on its own in store for the not-found-error
			// handling?
			if len(sequenceKey) != len(assertType.PrimaryKey)-1 {
				return fmt.Errorf("sequence key has wrong length for %q assertion", assertType.Name)
			}
			headers := make(map[string]string)
			for i, keyVal := range sequenceKey {
				name := assertType.PrimaryKey[i]
				if keyVal == "" {
					return fmt.Errorf("sequence key %q header cannot be empty", name)
				}
				headers[name] = keyVal
			}
			if hasSequenceNumber {
				headers[assertType.PrimaryKey[len(assertType.PrimaryKey)-1]] = fmt.Sprintf("%d", sequence)
			}
			return &asserts.NotFoundError{
				Type:    assertType,
				Headers: headers,
			}
		}
		// default error
		return nil
	}, "fetch assertion", user))

	return asrt, nil
}

func (s *Store) downloadAssertions(u *url.URL, decodeBody func(io.Reader) error, handleSvcErr func(*assertionSvcError) error, what string, user *auth.UserState) error {
	reqOptions := &requestOptions{
		Method: "GET",
		URL:    u,
		Accept: asserts.MediaType,
	}

	resp := mylog.Check2(httputil.RetryRequest(reqOptions.URL.String(), func() (*http.Response, error) {
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
	}, defaultRetryStrategy))

	if resp.StatusCode != 200 {
		return respToError(resp, what)
	}

	return nil
}

// DownloadAssertions download the assertion streams at the given URLs
// and adds their assertions to the given asserts.Batch.
func (s *Store) DownloadAssertions(streamURLs []string, b *asserts.Batch, user *auth.UserState) error {
	for _, ustr := range streamURLs {
		u := mylog.Check2(url.Parse(ustr))
		mylog.Check(s.downloadAssertions(u, func(r io.Reader) error {
			// decode stream
			_, e := b.AddStream(r)
			return e
		}, nil, "download assertion stream", user))

	}
	return nil
}
