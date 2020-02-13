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

	"golang.org/x/xerrors"
)

type CohortAction struct {
	Action string   `json:"action"`
	Snaps  []string `json:"snaps"`
}

func (client *Client) CreateCohorts(snaps []string) (map[string]string, error) {
	data, err := json.Marshal(&CohortAction{Action: "create", Snaps: snaps})
	if err != nil {
		return nil, fmt.Errorf("cannot request cohorts: %v", err)
	}

	var cohorts map[string]string

	if _, err := client.doSync("POST", "/v2/cohorts", nil, nil, bytes.NewReader(data), &cohorts); err != nil {
		fmt := "cannot create cohorts: %w"
		return nil, xerrors.Errorf(fmt, err)
	}

	return cohorts, nil

}
