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
	"fmt"
	"strings"

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
	Epoch            snap.Epoch         `json:"epoch"`
	IconURL          string             `json:"icon_url"`
	LastUpdated      string             `json:"last_updated,omitempty"`
	Name             string             `json:"package_name"`
	Prices           map[string]float64 `json:"prices,omitempty"`
	// Note that the publisher is really the "display name" of the
	// publisher
	Publisher      string   `json:"publisher,omitempty"`
	RatingsAverage float64  `json:"ratings_average,omitempty"`
	Revision       int      `json:"revision"` // store revisions are ints starting at 1
	ScreenshotURLs []string `json:"screenshot_urls,omitempty"`
	SnapID         string   `json:"snap_id"`
	License        string   `json:"license,omitempty"`
	Base           string   `json:"base,omitempty"`

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

	ChannelMapList []channelMap `json:"channel_maps_list,omitempty"`
}

// channelMap contains
type channelMap struct {
	Track       string                   `json:"track"`
	SnapDetails []channelSnapInfoDetails `json:"map,omitempty"`
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
	Revision     int        `json:"revision"` // store revisions are ints starting at 1
	Confinement  string     `json:"confinement"`
	Version      string     `json:"version"`
	Channel      string     `json:"channel"`
	Epoch        snap.Epoch `json:"epoch"`
	DownloadSize int64      `json:"binary_filesize"`
	Info         string     `json:"info"`
}

func infoFromRemote(d *snapDetails) *snap.Info {
	info := &snap.Info{}
	info.Architectures = d.Architectures
	info.Type = d.Type
	info.Version = d.Version
	info.Epoch = d.Epoch
	info.RealName = d.Name
	info.SnapID = d.SnapID
	info.Revision = snap.R(d.Revision)
	info.EditedTitle = d.Title
	info.EditedSummary = d.Summary
	info.EditedDescription = d.Description
	// Note that the store side is using confusing terminology here.
	// What the store calls "developer" is actually the publisher
	// username.
	//
	// It also sends "publisher" which is the "publisher display name"
	// which we cannot use currently because it is not validated
	// (i.e. the publisher could put anything in there and mislead
	// the users this way).
	info.Publisher = d.Developer
	info.PublisherID = d.DeveloperID
	info.Channel = d.Channel
	info.Sha3_384 = d.DownloadSha3_384
	info.Size = d.DownloadSize
	info.IconURL = d.IconURL
	info.AnonDownloadURL = d.AnonDownloadURL
	info.DownloadURL = d.DownloadURL
	info.Prices = d.Prices
	info.Private = d.Private
	info.Paid = len(info.Prices) > 0
	info.Confinement = snap.ConfinementType(d.Confinement)
	info.Contact = d.Contact
	info.License = d.License
	info.Base = d.Base

	deltas := make([]snap.DeltaInfo, len(d.Deltas))
	for i, d := range d.Deltas {
		deltas[i] = snap.DeltaInfo{
			FromRevision:    d.FromRevision,
			ToRevision:      d.ToRevision,
			Format:          d.Format,
			AnonDownloadURL: d.AnonDownloadURL,
			DownloadURL:     d.DownloadURL,
			Size:            d.Size,
			Sha3_384:        d.Sha3_384,
		}
	}
	info.Deltas = deltas

	screenshots := make([]snap.ScreenshotInfo, 0, len(d.ScreenshotURLs))
	for _, url := range d.ScreenshotURLs {
		screenshots = append(screenshots, snap.ScreenshotInfo{
			URL: url,
		})
	}
	info.Screenshots = screenshots
	// FIXME: once the store sends "contact" for everything, remove
	//        the "SupportURL" part of the if
	if info.Contact == "" {
		info.Contact = d.SupportURL
	}

	// fill in the tracks data
	if len(d.ChannelMapList) > 0 {
		info.Channels = make(map[string]*snap.ChannelSnapInfo)
		info.Tracks = make([]string, len(d.ChannelMapList))
		for i, cm := range d.ChannelMapList {
			info.Tracks[i] = cm.Track
			for _, ch := range cm.SnapDetails {
				// nothing in this channel
				if ch.Info == "" {
					continue
				}
				var k string
				if strings.HasPrefix(ch.Channel, cm.Track) {
					k = ch.Channel
				} else {
					k = fmt.Sprintf("%s/%s", cm.Track, ch.Channel)
				}
				info.Channels[k] = &snap.ChannelSnapInfo{
					Revision:    snap.R(ch.Revision),
					Confinement: snap.ConfinementType(ch.Confinement),
					Version:     ch.Version,
					Channel:     ch.Channel,
					Epoch:       ch.Epoch,
					Size:        ch.DownloadSize,
				}
			}
		}
	}

	return info
}
