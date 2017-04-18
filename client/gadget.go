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

// Gadget represents a gadget snap
type Gadget struct {
	Branding GadgetBranding `json:"branding,omitempty"`
}

// GadgetBranding is the optional custom branding of a gadget
type GadgetBranding struct {
	Name    string `json:"name,omitempty"`
	SubName string `json:"subname,omitempty"`
}

// Gadget returns the details of the gadget snap if one is active
func (client *Client) Gadget() (*Gadget, error) {
	var gadget *Gadget
	if err := client.doSync("GET", "/2.0/gadget", nil, nil, &gadget); err != nil {
		return nil, fmt.Errorf("%s: %s", "cannot fetch gadget", err)
	}

	return gadget, nil
}
