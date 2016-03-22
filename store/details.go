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

import (
	"github.com/ubuntu-core/snappy/snap"
)

// snapDetails encapsulates the data sent to us from the store.
//
// Full json available via:
// curl -s -H "accept: application/hal+json" -H "X-Ubuntu-Release: rolling-core" https://search.apps.ubuntu.com/api/v1/package/ubuntu-core.canonical | python -m json.tool
type snapDetails struct {
	AnonDownloadURL string             `json:"anon_download_url,omitempty"`
	Channel         string             `json:"channel,omitempty"`
	DownloadSha512  string             `json:"download_sha512,omitempty"`
	Summary         string             `json:"summary,omitempty"`
	Description     string             `json:"description,omitempty"`
	DownloadSize    int64              `json:"binary_filesize,omitempty"`
	DownloadURL     string             `json:"download_url,omitempty"`
	IconURL         string             `json:"icon_url"`
	LastUpdated     string             `json:"last_updated,omitempty"`
	Name            string             `json:"package_name"`
	FullName        string             `json:"name"`
	Prices          map[string]float64 `json:"prices,omitempty"`
	Publisher       string             `json:"publisher,omitempty"`
	RatingsAverage  float64            `json:"ratings_average,omitempty"`
	Revision        int                `json:"revision"`
	SupportURL      string             `json:"support_url"`
	Title           string             `json:"title"`
	Type            snap.Type          `json:"content,omitempty"`
	Version         string             `json:"version"`

	// FIXME: the store should return "developer" to us instead of
	//        origin
	Developer string `json:"origin" yaml:"origin"`
}

/*
A Purchase encapsulates the purchase data sent to us from the software center agent.

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
type Purchase struct {
	OpenID          string `json:"open_id"`
	PackageName     string `json:"package_name"`
	RefundableUntil string `json:"refundable_until"`
	State           string `json:"state"`
	ItemSKU         string `json:"item_sku,omitempty"`
	PurchaseID      string `json:"purchase_id,omitempty"`
}

/*
PurchaseInstruction encapsulates the data that must be sent
in order to make a purchase from the store.
*/
type PurchaseInstruction struct {
	DeviceID  string  `json:"device_id,omitempty"`
	Name      string  `json:"name"`
	ItemSKU   string  `json:"item_sku,omitempty"`
	Amount    float64 `json:"amount,omitempty"`
	Currency  string  `json:"currency,omitempty"`
	BackendID string  `json:"backend_id,omitempty"`
	MethodID  int64   `json:"method_id,omitempty"`
}

/*
AuthError contains the reason behind an authentication failure
*/
type AuthError struct {
	Threshold int64  `json:"threshold"`
	Error     string `json:"error"`
}
