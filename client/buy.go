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

	"github.com/ddkwork/golibrary/mylog"
)

// BuyOptions specifies parameters to buy from the store.
type BuyOptions struct {
	SnapID   string  `json:"snap-id"`
	Price    float64 `json:"price"`
	Currency string  `json:"currency"` // ISO 4217 code as string
}

// BuyResult holds the state of a buy attempt.
type BuyResult struct {
	State string `json:"state,omitempty"`
}

func (client *Client) Buy(opts *BuyOptions) (*BuyResult, error) {
	if opts == nil {
		opts = &BuyOptions{}
	}

	var body bytes.Buffer
	mylog.Check(json.NewEncoder(&body).Encode(opts))

	var result BuyResult
	_ := mylog.Check2(client.doSync("POST", "/v2/buy", nil, nil, &body, &result))

	return &result, nil
}

func (client *Client) ReadyToBuy() error {
	var result bool
	_ := mylog.Check2(client.doSync("GET", "/v2/buy/ready", nil, nil, nil, &result))
	return err
}
