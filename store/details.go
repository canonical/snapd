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

	"github.com/snapcore/snapd/jsonutil/puritan"
	"github.com/snapcore/snapd/snap"
)

// snapDetails encapsulates the data sent to us from the store as JSON.
type snapDetails struct {
	AnonDownloadURL  puritan.String            `json:"anon_download_url,omitempty"`
	Architectures    puritan.SimpleStringSlice `json:"architecture"`
	Channel          puritan.String            `json:"channel,omitempty"`
	DownloadSha3_384 puritan.SimpleString      `json:"download_sha3_384,omitempty"`
	Summary          puritan.String            `json:"summary,omitempty"`
	Description      puritan.Paragraph         `json:"description,omitempty"`
	Deltas           []snapDeltaDetail         `json:"deltas,omitempty"`
	DownloadSize     int64                     `json:"binary_filesize,omitempty"`
	DownloadURL      puritan.String            `json:"download_url,omitempty"`
	Epoch            snap.Epoch                `json:"epoch"`
	IconURL          puritan.String            `json:"icon_url"`
	LastUpdated      puritan.String            `json:"last_updated,omitempty"`
	Name             puritan.SimpleString      `json:"package_name"`
	Prices           puritan.OldPriceMap       `json:"prices,omitempty"`
	// Note that the publisher is really the "display name" of the
	// publisher
	Publisher      puritan.String       `json:"publisher,omitempty"`
	RatingsAverage float64              `json:"ratings_average,omitempty"`
	Revision       int                  `json:"revision"` // store revisions are ints starting at 1
	ScreenshotURLs puritan.StringSlice  `json:"screenshot_urls,omitempty"`
	SnapID         puritan.SimpleString `json:"snap_id"`
	SnapYAML       puritan.Paragraph    `json:"snap_yaml_raw"`
	License        puritan.String       `json:"license,omitempty"`
	Base           puritan.SimpleString `json:"base,omitempty"`

	// FIXME: the store should send "contact" here, once it does we
	//        can remove support_url
	SupportURL puritan.String `json:"support_url"`
	Contact    puritan.String `json:"contact"`

	Title   puritan.String `json:"title"`
	Type    snap.Type      `json:"content,omitempty"`
	Version puritan.String `json:"version"`

	// TODO: have the store return a 'developer_username' for this
	Developer   puritan.SimpleString `json:"origin"`
	DeveloperID puritan.SimpleString `json:"developer_id"`

	Private     bool                 `json:"private"`
	Confinement snap.ConfinementType `json:"confinement"`

	ChannelMapList []channelMap `json:"channel_maps_list,omitempty"`
}

// channelMap contains
type channelMap struct {
	Track       puritan.SimpleString     `json:"track"`
	SnapDetails []channelSnapInfoDetails `json:"map,omitempty"`
}

type snapDeltaDetail struct {
	FromRevision    int                  `json:"from_revision"`
	ToRevision      int                  `json:"to_revision"`
	Format          puritan.SimpleString `json:"format"`
	AnonDownloadURL puritan.String       `json:"anon_download_url,omitempty"`
	DownloadURL     puritan.String       `json:"download_url,omitempty"`
	Size            int64                `json:"binary_filesize,omitempty"`
	Sha3_384        puritan.SimpleString `json:"download_sha3_384,omitempty"`
}

// channelSnapInfoDetails is the subset of snapDetails we need to get
// information about the snaps in the various channels
type channelSnapInfoDetails struct {
	Revision     int                  `json:"revision"` // store revisions are ints starting at 1
	Confinement  snap.ConfinementType `json:"confinement"`
	Version      puritan.String       `json:"version"`
	Channel      puritan.String       `json:"channel"`
	Epoch        snap.Epoch           `json:"epoch"`
	DownloadSize int64                `json:"binary_filesize"`
	Info         puritan.String       `json:"info"`
}

func infoFromRemote(d *snapDetails) *snap.Info {
	info := &snap.Info{}
	info.Architectures = d.Architectures.Clean()
	info.Type = d.Type
	info.Version = d.Version.Clean()
	info.Epoch = d.Epoch
	info.RealName = d.Name.Clean()
	info.SnapID = d.SnapID.Clean()
	info.Revision = snap.R(d.Revision)
	info.EditedTitle = d.Title.Clean()
	info.EditedSummary = d.Summary.Clean()
	info.EditedDescription = d.Description.Clean()
	// Note that the store side is using confusing terminology here.
	// What the store calls "developer" is actually the publisher
	// username.
	//
	// It also sends "publisher" which is the "publisher display name"
	// which we cannot use currently because it is not validated
	// (i.e. the publisher could put anything in there and mislead
	// the users this way).
	info.Publisher = d.Developer.Clean()
	info.PublisherID = d.DeveloperID.Clean()
	info.Channel = d.Channel.Clean()
	info.Sha3_384 = d.DownloadSha3_384.Clean()
	info.Size = d.DownloadSize
	info.IconURL = d.IconURL.Clean()
	info.AnonDownloadURL = d.AnonDownloadURL.Clean()
	info.DownloadURL = d.DownloadURL.Clean()
	info.Prices = d.Prices.Clean()
	info.Private = d.Private
	info.Paid = len(info.Prices) > 0
	info.Confinement = d.Confinement
	info.Contact = d.Contact.Clean()
	info.License = d.License.Clean()
	info.Base = d.Base.Clean()

	deltas := make([]snap.DeltaInfo, len(d.Deltas))
	for i, d := range d.Deltas {
		deltas[i] = snap.DeltaInfo{
			FromRevision:    d.FromRevision,
			ToRevision:      d.ToRevision,
			Format:          d.Format.Clean(),
			AnonDownloadURL: d.AnonDownloadURL.Clean(),
			DownloadURL:     d.DownloadURL.Clean(),
			Size:            d.Size,
			Sha3_384:        d.Sha3_384.Clean(),
		}
	}
	info.Deltas = deltas

	screenshots := make([]snap.ScreenshotInfo, 0, len(d.ScreenshotURLs))
	for _, url := range d.ScreenshotURLs {
		screenshots = append(screenshots, snap.ScreenshotInfo{
			URL: url.Clean(),
		})
	}
	info.Screenshots = screenshots
	// FIXME: once the store sends "contact" for everything, remove
	//        the "SupportURL" part of the if
	if info.Contact == "" {
		info.Contact = d.SupportURL.Clean()
	}

	// fill in the plug/slot data
	if rawYamlInfo, err := snap.InfoFromSnapYaml([]byte(d.SnapYAML.Clean())); err == nil {
		if info.Plugs == nil {
			info.Plugs = make(map[string]*snap.PlugInfo)
		}
		for k, v := range rawYamlInfo.Plugs {
			info.Plugs[k] = v
			info.Plugs[k].Snap = info
		}
		if info.Slots == nil {
			info.Slots = make(map[string]*snap.SlotInfo)
		}
		for k, v := range rawYamlInfo.Slots {
			info.Slots[k] = v
			info.Slots[k].Snap = info
		}
	}

	// fill in the tracks data
	if len(d.ChannelMapList) > 0 {
		info.Channels = make(map[string]*snap.ChannelSnapInfo)
		info.Tracks = make([]string, len(d.ChannelMapList))
		for i, cm := range d.ChannelMapList {
			info.Tracks[i] = cm.Track.Clean()
			for _, ch := range cm.SnapDetails {
				// nothing in this channel
				if ch.Info.Clean() == "" {
					continue
				}
				k := ch.Channel.Clean()
				track := cm.Track.Clean()
				if !strings.HasPrefix(k, track) {
					k = fmt.Sprintf("%s/%s", track, k)
				}
				info.Channels[k] = &snap.ChannelSnapInfo{
					Revision:    snap.R(ch.Revision),
					Confinement: ch.Confinement,
					Version:     ch.Version.Clean(),
					Channel:     ch.Channel.Clean(),
					Epoch:       ch.Epoch,
					Size:        ch.DownloadSize,
				}
			}
		}
	}

	return info
}
