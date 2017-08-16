// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
)

type storeData struct {
	Store string `json:"store"`
}

type setStoreResult struct {
	URL string `json:"url"`
}

// SetStore configures the store according to an existing store assertion for
// the given operator and store name.
func (client *Client) SetStore(store string) (storeURL string, err error) {
	body, err := json.Marshal(storeData{
		Store: store,
	})
	if err != nil {
		return "", err
	}

	var result setStoreResult
	_, err = client.doSync("PUT", "/v2/store", nil, nil, bytes.NewReader(body), &result)
	if err != nil {
		return "", err
	}
	return result.URL, nil
}

// UnsetStore restores store configuration to system defaults.
func (client *Client) UnsetStore() error {
	_, err := client.doSync("DELETE", "/v2/store", nil, nil, nil, nil)
	return err
}
