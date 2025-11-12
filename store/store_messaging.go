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
	"net/url"
)

const (
	messagesEndpointPath = "v2/messages"

	// TODO: DEVELOPMENT ONLY
	devMessagingURL = "http://0.0.0.0:8000/v2/messages"
)

// PollMessagesRequest contains parameters for the unified polling endpoint.
type PollMessagesRequest struct {
	After    string    `json:"after,omitempty"`    // Token for acknowledgement
	Limit    int       `json:"limit"`              // Required: >0 to fetch, =0 for ack-only
	Messages []Message `json:"messages,omitempty"` // Response messages to send
}

// PollMessagesResponse contains request-message messages received from the store with their tokens.
type PollMessagesResponse struct {
	Messages             []MessageWithToken `json:"messages,omitempty"`
	TotalPendingMessages int                `json:"total-pending-messages"`
}

// messageQueueError contains error responses from the message queue.
type messageQueueError struct {
	ErrorList []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error-list"`
}

// Message represents a message and its format.
type Message struct {
	Format string `json:"format"` // e.g. assertion
	Data   string `json:"data"`   // Encoded assertion
}

// MessageWithToken is a request-message with its acknowledgement token.
type MessageWithToken struct {
	Message
	Token string `json:"token"`
}

// PollMessages polls the store's /v2/messages endpoint, sending response-message messages
// and acknowledging received request-message messages.
func (s *Store) PollMessages(ctx context.Context, req *PollMessagesRequest) (*PollMessagesResponse, error) {
	var resp PollMessagesResponse
	var errResp messageQueueError

	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal poll request: %w", err)
	}

	// TODO: DEVELOPMENT ONLY - Use hardcoded URL for testing
	// Remove this and uncomment the endpointURL call below when ready for review
	endpointURL, err := url.Parse(devMessagingURL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse dev messaging URL: %w", err)
	}

	// Production code (currently disabled):
	// endpointURL, err := s.endpointURL(messagesEndpointPath, nil)
	// if err != nil {
	// 	return nil, fmt.Errorf("cannot build messaging endpoint URL: %w", err)
	// }

	reqOptions := &requestOptions{
		Method:      "POST",
		URL:         endpointURL,
		ContentType: jsonContentType,
		Data:        reqData,
		Accept:      jsonContentType,
	}

	httpResp, err := s.retryRequestDecodeJSON(ctx, reqOptions, nil, &resp, &errResp)
	if err != nil {
		return nil, fmt.Errorf("cannot poll messages: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		if len(errResp.ErrorList) > 0 {
			return nil, fmt.Errorf("cannot poll messages: %s (code: %s)",
				errResp.ErrorList[0].Message, errResp.ErrorList[0].Code)
		}

		return nil, fmt.Errorf("cannot poll messages: status %d", httpResp.StatusCode)
	}

	return &resp, nil
}
