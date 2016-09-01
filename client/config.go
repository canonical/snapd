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
)

// SetConfig requests a snap to set the provided config.
func (client *Client) SetConfig(snapName string, config map[string]interface{}) (changeID string, err error) {
	b, err := json.Marshal(config)
	if err != nil {
		return "", err
	}
	return client.doAsync("PUT", "/v2/snaps/"+snapName+"/config", nil, nil, bytes.NewReader(b))
}

// GetConfig asks for a snap's current config.
func (client *Client) GetConfig(snapName, configKey string) (configuration string, err error) {
	_, err = client.doSync("GET", "/v2/snaps/"+snapName+"/config/"+configKey, nil, nil, nil, &configuration)
	if err != nil {
		return "", err
	}

	return configuration, nil
}
