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
	"github.com/snapcore/snapd/snap"
)

// snapDetails encapsulates the data sent to us from the store as JSON.
type snapDetails struct {
	AnonDownloadURL  string             `json:"anon_download_url,omitempty"`
	Architectures    []string           `json:"architecture"`
	Channel          string             `json:"channel,omitempty"`
	DownloadSha3_384 string             `json:"download_sha3_384,omitempty"`
	Summary          string             `json:"summary,omitempty"`
	Description      string             `json:"description,omitempty"`
	Deltas           []snapDeltaDetail  `json:"deltas,omitempty"`
	DownloadSize     int64              `json:"binary_filesize,omitempty"`
	DownloadURL      string             `json:"download_url,omitempty"`
	Epoch            string             `json:"epoch"`
	IconURL          string             `json:"icon_url"`
	LastUpdated      string             `json:"last_updated,omitempty"`
	Name             string             `json:"package_name"`
	Prices           map[string]float64 `json:"prices,omitempty"`
	Publisher        string             `json:"publisher,omitempty"`
	RatingsAverage   float64            `json:"ratings_average,omitempty"`
	Revision         int                `json:"revision"` // store revisions are ints starting at 1
	ScreenshotURLs   []string           `json:"screenshot_urls,omitempty"`
	SnapID           string             `json:"snap_id"`

	// FIXME: the store should send "contact" here, once it does we
	//        can remove support_url
	SupportURL string `json:"support_url"`
	Contact    string `json:"contact"`

	Title   string    `json:"title"`
	Type    snap.Type `json:"content,omitempty"`
	Version string    `json:"version"`

	// TODO: have the store return a 'developer_username' for this
	Developer   string `json:"origin"`
	DeveloperID string `json:"developer_id"`

	Private     bool   `json:"private"`
	Confinement string `json:"confinement"`
}

type snapDeltaDetail struct {
	FromRevision    int    `json:"from_revision"`
	ToRevision      int    `json:"to_revision"`
	Format          string `json:"format"`
	AnonDownloadURL string `json:"anon_download_url,omitempty"`
	DownloadURL     string `json:"download_url,omitempty"`
	Size            int64  `json:"binary_filesize,omitempty"`
	Sha3_384        string `json:"download_sha3_384,omitempty"`
}

// channelSnapInfoDetails is the subset of snapDetails we need to get
// information about the snaps in the various channels
type channelSnapInfoDetails struct {
	Revision     int    `json:"revision"` // store revisions are ints starting at 1
	Confinement  string `json:"confinement"`
	Version      string `json:"version"`
	Channel      string `json:"channel"`
	Epoch        string `json:"epoch"`
	DownloadSize int64  `json:"binary_filesize"`
}
