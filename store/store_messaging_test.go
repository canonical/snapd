// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package store_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"

	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
	. "gopkg.in/check.v1"
)

type storeMessagingSuite struct {
	baseStoreSuite
}

var _ = Suite(&storeMessagingSuite{})

func (s *storeMessagingSuite) SetUpTest(c *C) {
	s.baseStoreSuite.SetUpTest(c)
}

func (s *storeMessagingSuite) TestFetchMessagesOK(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Check(r.Method, Equals, "POST")
		c.Check(r.URL.Path, Equals, "/v2/messages")
		c.Check(r.Header.Get("Content-Type"), Equals, "application/json")

		body, err := io.ReadAll(r.Body)
		c.Assert(err, IsNil)

		var req store.MessagesRequest
		err = json.Unmarshal(body, &req)
		c.Assert(err, IsNil)
		c.Check(req.Limit, Equals, 10)
		c.Check(req.After, Equals, "token-42")
		c.Check(req.Messages, HasLen, 1)
		c.Check(req.Messages[0].Data, Equals, "response-message-42")

		resp := map[string]any{
			"messages": []map[string]string{
				{
					"format": "assertion",
					"data":   "request-message-43",
					"token":  "token-43",
				},
			},
			"total-pending-messages": 5,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		json.NewEncoder(w).Encode(resp)
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	req := &store.MessagesRequest{
		After: "token-42",
		Limit: 10,
		Messages: []store.Message{
			{Format: "assertion", Data: "response-message-42"},
		},
	}
	resp, err := sto.FetchMessages(s.ctx, req)
	c.Assert(err, IsNil)
	c.Assert(resp, NotNil)
	c.Assert(resp.Messages, HasLen, 1)
	c.Check(resp.Messages[0].Data, Equals, "request-message-43")
	c.Check(resp.Messages[0].Token, Equals, "token-43")
	c.Check(resp.TotalPendingMessages, Equals, 5)
}

func (s *storeMessagingSuite) TestFetchMessagesNilRequest(c *C) {
	mockServerURL, _ := url.Parse("http://store.example.com")
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	resp, err := sto.FetchMessages(s.ctx, nil)
	c.Assert(err, ErrorMatches, "message request cannot be nil")
	c.Check(resp, IsNil)
}

func (s *storeMessagingSuite) TestFetchMessagesNegativeLimit(c *C) {
	mockServerURL, _ := url.Parse("http://store.example.com")
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	req := &store.MessagesRequest{Limit: -5}

	resp, err := sto.FetchMessages(s.ctx, req)
	c.Assert(err, ErrorMatches, "limit must be non-negative, got -5")
	c.Check(resp, IsNil)
}

func (s *storeMessagingSuite) TestFetchMessagesBadRequest(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		errResp := map[string]any{
			"error-list": []map[string]string{
				{
					"code":    "bad-request",
					"message": "invalid request format",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(errResp)
	}))
	c.Assert(mockServer, NotNil)
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	req := &store.MessagesRequest{After: "token-42", Limit: 0}

	resp, err := sto.FetchMessages(s.ctx, req)
	c.Assert(
		err,
		ErrorMatches,
		`cannot fetch messages: invalid request format \(code: bad-request\) \(status: 400\)`,
	)
	c.Check(resp, IsNil)
}

func (s *storeMessagingSuite) TestFetchMessagesServerError(c *C) {
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("{}"))
	}))
	defer mockServer.Close()

	mockServerURL, _ := url.Parse(mockServer.URL)
	dauthCtx := &testDauthContext{c: c, device: s.device}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	req := &store.MessagesRequest{Limit: 10}

	resp, err := sto.FetchMessages(s.ctx, req)
	c.Assert(err, ErrorMatches, "cannot fetch messages: got unexpected HTTP status code 500 via POST to .*")
	c.Check(resp, IsNil)
}

func (s *storeMessagingSuite) TestFetchMessagesStoreOffline(c *C) {
	mockServerURL, _ := url.Parse("http://store.example.local")
	dauthCtx := &testDauthContext{c: c, device: s.device, storeOffline: true}
	sto := store.New(&store.Config{
		StoreBaseURL: mockServerURL,
	}, dauthCtx)

	req := &store.MessagesRequest{Limit: 10}

	resp, err := sto.FetchMessages(s.ctx, req)
	c.Assert(err, testutil.ErrorIs, store.ErrStoreOffline)
	c.Check(resp, IsNil)
}
