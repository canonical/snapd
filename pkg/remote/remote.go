// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package remote

import (
	"github.com/ubuntu-core/snappy/pkg"
)

// A Snap encapsulates the data sent to us from the store.
type Snap struct {
	Alias           string             `json:"alias,omitempty"`
	AnonDownloadURL string             `json:"anon_download_url,omitempty"`
	Channel         string             `json:"channel,omitempty"`
	DownloadSha512  string             `json:"download_sha512,omitempty"`
	Description     string             `json:"description,omitempty"`
	DownloadSize    int64              `json:"binary_filesize,omitempty"`
	DownloadURL     string             `json:"download_url,omitempty"`
	IconURL         string             `json:"icon_url"`
	LastUpdated     string             `json:"last_updated,omitempty"`
	Name            string             `json:"package_name"`
	Origin          string             `json:"origin"`
	Prices          map[string]float64 `json:"prices,omitempty"`
	Publisher       string             `json:"publisher,omitempty"`
	RatingsAverage  float64            `json:"ratings_average,omitempty"`
	SupportURL      string             `json:"support_url"`
	Title           string             `json:"title"`
	Type            pkg.Type           `json:"content,omitempty"`
	Version         string             `json:"version"`
}
