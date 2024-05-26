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
	"net/url"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

// SetConf requests a snap to apply the provided patch to the configuration.
func (client *Client) SetConf(snapName string, patch map[string]interface{}) (changeID string, err error) {
	b := mylog.Check2(json.Marshal(patch))

	return client.doAsync("PUT", "/v2/snaps/"+snapName+"/conf", nil, nil, bytes.NewReader(b))
}

// Conf asks for a snap's current configuration.
//
// Note that the configuration may include json.Numbers.
func (client *Client) Conf(snapName string, keys []string) (configuration map[string]interface{}, err error) {
	// Prepare query
	query := url.Values{}
	query.Set("keys", strings.Join(keys, ","))

	_ = mylog.Check2(client.doSync("GET", "/v2/snaps/"+snapName+"/conf", query, nil, nil, &configuration))

	return configuration, nil
}
