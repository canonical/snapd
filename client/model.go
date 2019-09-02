// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"
	"net/url"

	"github.com/snapcore/snapd/asserts"
)

type remodelData struct {
	NewModel string `json:"new-model"`
}

// Remodel tries to remodel the system with the given assertion data
func (client *Client) Remodel(b []byte) (changeID string, err error) {
	data, err := json.Marshal(&remodelData{
		NewModel: string(b),
	})
	if err != nil {
		return "", fmt.Errorf("cannot marshal remodel data: %v", err)
	}
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	return client.doAsync("POST", "/v2/model", nil, headers, bytes.NewReader(data))
}

// CurrentModelAssertion returns the current model assertion
func (client *Client) CurrentModelAssertion() (*asserts.Model, error) {
	assert, err := currentAssertion(client, "/v2/model")
	if err != nil {
		return nil, err
	}
	modelAssert, ok := assert.(*asserts.Model)
	if !ok {
		return nil, fmt.Errorf("unexpected assertion type (%s) returned", assert.Type().Name)
	}
	return modelAssert, nil
}

// CurrentSerialAssertion returns the current serial assertion
func (client *Client) CurrentSerialAssertion() (*asserts.Serial, error) {
	assert, err := currentAssertion(client, "/v2/model/serial")
	if err != nil {
		return nil, err
	}
	serialAssert, ok := assert.(*asserts.Serial)
	if !ok {
		return nil, fmt.Errorf("unexpected assertion type (%s) returned", assert.Type().Name)
	}
	return serialAssert, nil
}

// helper function for getting assertions from the daemon via a REST path
func currentAssertion(client *Client, path string) (asserts.Assertion, error) {
	q := url.Values{}

	response, err := client.raw("GET", path, q, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to query current assertion: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, parseError(response)
	}

	dec := asserts.NewDecoder(response.Body)

	// only decode a single assertion - we can't ever get more than a single
	// assertion through these endpoints by design
	assert, err := dec.Decode()
	if err != nil {
		return nil, fmt.Errorf("failed to decode assertions: %v", err)
	}

	return assert, nil
}
