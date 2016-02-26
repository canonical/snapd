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
	"fmt"
)

type Branding struct {
	Name    string `json:"name"`
	SubName string `json:"subname"`
}

func (client *Client) Branding() (*Branding, error) {
	var branding *Branding
	if err := client.doSync("GET", "/2.0/branding", nil, nil, &branding); err != nil {
		return &Branding{}, fmt.Errorf("cannot retrieve branding: %s", err)
	}

	return branding, nil
}
