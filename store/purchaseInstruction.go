// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package store

/*
PurchaseInstruction encapsulates the data that must be sent in order to make a purchase from the store.

The Device ID can be sent in the extended header "X-Device-Id".
The Partner ID (e.g. "bq") can be sent in the extended header "X-Partner-Id".
*/
type PurchaseInstruction struct {
	Name      string  `json:"name"`
	ItemSKU   string  `json:"item_sku,omitempty"`
	Amount    float64 `json:"amount,omitempty"`
	Currency  string  `json:"currency,omitempty"`
	BackendID string  `json:"backend_id,omitempty"`
	MethodID  int64   `json:"method_id,omitempty"`
}
