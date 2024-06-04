// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"strings"
)

func (c *Client) AspectGet(aspectID string, requests []string) (result map[string]interface{}, err error) {
	query := url.Values{}
	query.Add("fields", strings.Join(requests, ","))

	endpoint := fmt.Sprintf("/v2/aspects/%s", aspectID)
	_, err = c.doSync("GET", endpoint, query, nil, nil, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (c *Client) AspectSet(aspectID string, requestValues map[string]interface{}) (changeID string, err error) {
	body, err := json.Marshal(requestValues)
	if err != nil {
		return "", err
	}

	headers := make(map[string]string)
	headers["Content-Type"] = "application/json"

	endpoint := fmt.Sprintf("/v2/aspects/%s", aspectID)
	return c.doAsync("PUT", endpoint, nil, headers, bytes.NewReader(body))
}
