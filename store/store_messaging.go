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

// Package store has support to use the Ubuntu Store for querying and downloading of snaps, and the related services.
package store

import (
	"context"
	"encoding/json"
	"fmt"
)

const (
	messagesEndpointPath = "v2/messages"
)

// MessageExchangeRequest contains parameters for polling & sending messages to the Store.
type MessageExchangeRequest struct {
	// Token of last successfully stored request message.
	// Acknowledges all messages up to and including the specified token.
	After string `json:"after,omitempty"`

	// Controls the operation mode:
	//   - limit > 0: Fetch up to limit messages (server may return fewer)
	//   - limit = 0: Don't return messages (useful for ack-only, response-only, or status checks)
	Limit int `json:"limit"`

	// The response messages to send to the Store.
	Messages []Message `json:"messages,omitempty"`
}

// MessageExchangeResponse contains request messages received from the store with their tokens.
//
// When:
//   - limit > 0: messages contains up to limit request messages
//   - limit = 0: messages is omitted, only queue status is returned
type MessageExchangeResponse struct {
	// Request messages with their acknowledgement tokens.
	Messages []MessageWithToken `json:"messages"`

	// Total unacknowledged messages in the device's queue.
	TotalPendingMessages int `json:"total-pending-messages"`
}

// messageQueueError contains error responses from the message queue.
type messageQueueError struct {
	ErrorList []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error-list"`
}

func (e *messageQueueError) Error() string {
	msg := ""
	for i, err := range e.ErrorList {
		if i > 0 {
			msg += "; "
		}
		msg += fmt.Sprintf("%s (code: %s)", err.Message, err.Code)
	}

	return msg
}

// Message represents a message and its format.
type Message struct {
	Format string `json:"format"` // e.g. assertion
	Data   string `json:"data"`   // Encoded assertion
}

// MessageWithToken is a request message with its acknowledgement token.
// The token should be echoed back in a subsequent poll's After field once the
// message has been successfully received and persisted.
type MessageWithToken struct {
	Message
	Token string `json:"token"`
}

// ExchangeMessages calls the store's /v2/messages endpoint to fetch request messages,
// acknowledge received messages, and send response messages.
func (s *Store) ExchangeMessages(ctx context.Context, req *MessageExchangeRequest) (*MessageExchangeResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("message request cannot be nil")
	}
	if req.Limit < 0 {
		return nil, fmt.Errorf("limit must be non-negative, got %d", req.Limit)
	}

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal message request: %w", err)
	}

	url, err := s.endpointURL(messagesEndpointPath, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot build messaging endpoint URL: %w", err)
	}

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         url,
		ContentType: jsonContentType,
		Data:        reqData,
		Accept:      jsonContentType,
	}

	var resp MessageExchangeResponse
	var errResp messageQueueError
	httpResp, err := s.retryRequestDecodeJSON(ctx, reqOptions, nil, &resp, &errResp)
	if err != nil {
		return nil, fmt.Errorf("cannot exchange messages: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		if len(errResp.ErrorList) > 0 {
			return nil, fmt.Errorf("cannot exchange messages: %w (status: %d)", &errResp, httpResp.StatusCode)
		}

		return nil, respToError(httpResp, "exchange messages")
	}

	return &resp, nil
}
