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

package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

type SystemKeyMismatchAction string

const (
	MismatchActionProceed       SystemKeyMismatchAction = "proceed"
	MismatchActionWaitForChange SystemKeyMismatchAction = "wait-for-change"
)

var (
	// ErrMismatchAdviceUnsupported indicates that current snapd does not
	// support the API to obtain advice on system key mismatch.
	ErrMismatchAdviceUnsupported = errors.New("system-key mismatch action is not supported")
)

type AdvisedAction struct {
	SuggestedAction SystemKeyMismatchAction
	// ChangeID carries the change to wait for if action was "wait-for-change".
	ChangeID string
}

// SystemKeyMismatchAdvise is usually called after having detected a mismatch of
// the system keys and performs an access to snapd API with the goal of getting
// an advice from snapd on how to proceed. The request carries system-key
// derived by the client. Returns an error of type *Error for errors
// communicated directly through snapd API. Note, that the response is only
// advisory.
func (client *Client) SystemKeyMismatchAdvice(systemKey any) (*AdvisedAction, error) {
	// system key implements Stringer
	sks, ok := systemKey.(fmt.Stringer)
	if systemKey == nil || !ok {
		return nil, fmt.Errorf("cannot be marshaled as system key")
	}

	sk := sks.String()
	if sk == "" {
		return nil, fmt.Errorf("no system key provided")
	}

	// same as for /v2/system-info
	opts := &doOptions{
		Timeout: 25 * time.Second,
		Retry:   doRetry,
	}

	// update maintenance status
	client.checkMaintenanceJSON()

	var rsp response
	var body io.Reader

	d, doErr := json.Marshal(struct {
		Action    string `json:"action"`
		SystemKey string `json:"system-key"`
	}{
		Action:    "advise-system-key-mismatch",
		SystemKey: sk,
	})
	if doErr != nil {
		return nil, fmt.Errorf("cannot marshal request data: %w", doErr)
	}

	body = bytes.NewReader(d)
	hdrs := map[string]string{
		"Content-Type": "application/json",
	}
	statusCode, doErr := client.do("POST", "/v2/system-info", nil, hdrs, body, &rsp, opts)
	if statusCode == 405 {
		// Method Not Allowed, most likely we are talking to an older version of
		// snapd, which does not support the advisory system key mismatch
		// handling, but since we got a response, snapd must be up already
		return nil, ErrMismatchAdviceUnsupported
	}

	if doErr != nil {
		return nil, doErr
	}

	if err := rsp.err(client, statusCode); err != nil {
		return nil, err
	}

	var act *AdvisedAction

	switch rsp.Type {
	case "sync":
		// body in the request?
		act = &AdvisedAction{
			SuggestedAction: MismatchActionProceed,
		}
	case "async":
		// async response with change implies that the client should wait
		if rsp.Change == "" {
			return nil, fmt.Errorf("async response without change reference")
		}
		act = &AdvisedAction{
			SuggestedAction: MismatchActionWaitForChange,
			ChangeID:        rsp.Change,
		}
	default:
		return nil, fmt.Errorf(`unexpected response for "POST" on "/v2/system-info", got %q`, rsp.Type)
	}

	client.warningCount = rsp.WarningCount
	client.warningTimestamp = rsp.WarningTimestamp

	return act, nil
}
