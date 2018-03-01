// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"strconv"

	"github.com/snapcore/snapd/snap"
)

// storeSnap holds the information sent as JSON by the store for a snap.
type storeSnap struct {
	Architectures []string          `json:"architectures"`
	Base          string            `json:"base"`
	Confinement   string            `json:"confinement"`
	Contact       string            `json:"contact"`
	CreatedAt     string            `json:"created-at"` // revision timestamp
	Description   string            `json:"description"`
	Download      storeSnapDownload `json:"download"`
	Epoch         snap.Epoch        `json:"epoch"`
	License       string            `json:"license"`
	Name          string            `json:"name"`
	Prices        map[string]string `json:"prices"` // currency->price,  free: {"USD": "0"}
	Private       bool              `json:"private"`
	Publisher     storeAccount      `json:"publisher"`
	Revision      int               `json:"revision"` // store revisions are ints starting at 1
	SnapID        string            `json:"snap-id"`
	SnapYAML      string            `json:"snap-yaml"` // optional
	Summary       string            `json:"summary"`
	Title         string            `json:"title"`
	Type          snap.Type         `json:"type"`
	Version       string            `json:"version"`

	// TODO: not yet defined: channel map

	// media
	Media []storeSnapMedia `json:"media"`
}

type storeSnapDownload struct {
	Sha3_384 string           `json:"sha3-384"`
	Size     int64            `json:"size"`
	URL      string           `json:"url"`
	Deltas   []storeSnapDelta `json:"deltas"`
}

type storeSnapDelta struct {
	Format   string `json:"format"`
	Sha3_384 string `json:"sha3-384"`
	Size     int64  `json:"size"`
	Source   int    `json:"source"`
	Target   int    `json:"target"`
	URL      string `json:"url"`
}

type storeAccount struct {
	ID    string `json:"id"`
	Name  string `json:"name"`  // aka username
	Title string `json:"title"` // aka display-name
}

type storeSnapMedia struct {
	Type   string `json:"type"` // icon/screenshot
	URL    string `json:"url"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

func infoFromStoreSnap(d *storeSnap) (*snap.Info, error) {
	info := &snap.Info{}
	info.RealName = d.Name
	info.Revision = snap.R(d.Revision)
	info.SnapID = d.SnapID
	info.EditedTitle = d.Title
	info.EditedSummary = d.Summary
	info.EditedDescription = d.Description
	info.Private = d.Private
	info.Contact = d.Contact
	info.Architectures = d.Architectures
	info.Type = d.Type
	info.Version = d.Version
	info.Epoch = d.Epoch
	info.Confinement = snap.ConfinementType(d.Confinement)
	info.Base = d.Base
	info.License = d.License
	info.PublisherID = d.Publisher.ID
	info.Publisher = d.Publisher.Name
	info.DownloadURL = d.Download.URL
	info.Size = d.Download.Size
	info.Sha3_384 = d.Download.Sha3_384
	if len(d.Download.Deltas) > 0 {
		deltas := make([]snap.DeltaInfo, len(d.Download.Deltas))
		for i, d := range d.Download.Deltas {
			deltas[i] = snap.DeltaInfo{
				FromRevision: d.Source,
				ToRevision:   d.Target,
				Format:       d.Format,
				DownloadURL:  d.URL,
				Size:         d.Size,
				Sha3_384:     d.Sha3_384,
			}
		}
		info.Deltas = deltas
	}

	// fill in the plug/slot data
	if rawYamlInfo, err := snap.InfoFromSnapYaml([]byte(d.SnapYAML)); err == nil {
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

	// convert prices
	if len(d.Prices) > 0 {
		prices := make(map[string]float64, len(d.Prices))
		for currency, priceStr := range d.Prices {
			price, err := strconv.ParseFloat(priceStr, 64)
			if err != nil {
				return nil, fmt.Errorf("cannot parse snap price: %v", err)
			}
			prices[currency] = price
		}
		info.Paid = true
		info.Prices = prices
	}

	// media
	screenshots := make([]snap.ScreenshotInfo, 0, len(d.Media))
	for _, mediaObj := range d.Media {
		switch mediaObj.Type {
		case "icon":
			if info.IconURL == "" {
				info.IconURL = mediaObj.URL
			}
		case "screenshot":
			screenshots = append(screenshots, snap.ScreenshotInfo{
				URL:    mediaObj.URL,
				Width:  mediaObj.Width,
				Height: mediaObj.Height,
			})
		}
	}
	if len(screenshots) > 0 {
		info.Screenshots = screenshots
	}

	return info, nil
}
