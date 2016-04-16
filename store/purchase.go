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
purchase encapsulates the purchase data sent to us from the software center agent.

HTTP/1.1 200 OK
Content-Type: application/json; charset=utf-8

[
  {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "package_name": "com.ubuntu.developer.dev.appname",
    "refundable_until": "2015-07-15 18:46:21",
    "state": "Complete"
  },
  {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "package_name": "com.ubuntu.developer.dev.appname",
    "item_sku": "item-1-sku",
    "purchase_id": "1",
    "refundable_until": null,
    "state": "Complete"
  },
  {
    "open_id": "https://login.staging.ubuntu.com/+id/open_id",
    "package_name": "com.ubuntu.developer.dev.otherapp",
    "refundable_until": "2015-07-17 11:33:29",
    "state": "Complete"
  }
]
*/
type purchase struct {
	OpenID          string `json:"open_id"`
	PackageName     string `json:"package_name"`
	RefundableUntil string `json:"refundable_until"`
	State           string `json:"state"`
	ItemSKU         string `json:"item_sku,omitempty"`
	PurchaseID      string `json:"purchase_id,omitempty"`
}

type purchaseList []purchase
