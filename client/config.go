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

package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"gopkg.in/yaml.v2"
)

// ConfigFromSnippet fetches the configuration under a given key of a
// given snap, optionally setting it to the given value first.
func (client *Client) ConfigFromSnippet(name, key, value string) (string, error) {
	var query url.Values
	var method string
	var body bytes.Buffer

	path := fmt.Sprintf("/2.0/snaps/%s/config", name)
	if value == "" {
		method = "GET"
		query = url.Values{"snippet": []string{key}}
	} else {
		method = "PATCH"

		// value is expected to be YAML-encoded (as all of
		// config is), but the REST API wants JSON (as all the
		// REST API is). So, decode and re-encode.
		var encv interface{}
		if err := yaml.Unmarshal([]byte(value), &encv); err != nil {
			return "", err
		}
		obj := map[string]interface{}{key: encv}

		json.NewEncoder(&body).Encode(obj)
	}

	var v interface{}
	if err := client.doSync(method, path, query, &body, &v); err != nil {
		return "", err
	}

	bs, err := yaml.Marshal(v)
	if err != nil {
		return "", err
	}

	return string(bytes.TrimSpace(bs)), nil
}

// ConfigFromReader fetches the configuration of the given snap,
// optionally setting it first.
func (client *Client) ConfigFromReader(name string, fin io.Reader) (io.Reader, error) {
	path := fmt.Sprintf("/2.0/snaps/%s/config", name)
	method := "PUT"
	if fin == nil {
		method = "GET"
	}
	var buf string
	if err := client.doSync(method, path, nil, fin, &buf); err != nil {
		return nil, err
	}

	return strings.NewReader(buf), nil
}
