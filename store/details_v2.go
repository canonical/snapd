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

	"github.com/snapcore/snapd/jsonutil/puritan"
	"github.com/snapcore/snapd/snap"
)

// storeSnap holds the information sent as JSON by the store for a snap.
type storeSnap struct {
	Architectures puritan.SimpleStringSlice `json:"architectures"`
	Base          puritan.SimpleString      `json:"base"`
	Confinement   snap.ConfinementType      `json:"confinement"`
	Contact       puritan.String            `json:"contact"`
	CreatedAt     puritan.SimpleString      `json:"created-at"` // revision timestamp
	Description   puritan.Paragraph         `json:"description"`
	Download      storeSnapDownload         `json:"download"`
	Epoch         snap.Epoch                `json:"epoch"`
	License       puritan.String            `json:"license"`
	Name          puritan.SimpleString      `json:"name"`
	Prices        puritan.PriceMap          `json:"prices"` // currency->price,  free: {"USD": "0"}
	Private       bool                      `json:"private"`
	Publisher     storeAccount              `json:"publisher"`
	Revision      int                       `json:"revision"` // store revisions are ints starting at 1
	SnapID        puritan.SimpleString      `json:"snap-id"`
	SnapYAML      puritan.Paragraph         `json:"snap-yaml"` // optional
	Summary       puritan.String            `json:"summary"`
	Title         puritan.String            `json:"title"`
	Type          snap.Type                 `json:"type"`
	Version       puritan.String            `json:"version"`

	// TODO: not yet defined: channel map

	// media
	Media []storeSnapMedia `json:"media"`
}

type storeSnapDownload struct {
	Sha3_384 puritan.SimpleString `json:"sha3-384"`
	Size     int64                `json:"size"`
	URL      puritan.String       `json:"url"`
	Deltas   []storeSnapDelta     `json:"deltas"`
}

type storeSnapDelta struct {
	Format   puritan.SimpleString `json:"format"`
	Sha3_384 puritan.SimpleString `json:"sha3-384"`
	Size     int64                `json:"size"`
	Source   int                  `json:"source"`
	Target   int                  `json:"target"`
	URL      puritan.String       `json:"url"`
}

type storeAccount struct {
	ID          puritan.SimpleString `json:"id"`
	Username    puritan.SimpleString `json:"username"`
	DisplayName puritan.String       `json:"display-name"`
}

type storeSnapMedia struct {
	Type   puritan.SimpleString `json:"type"` // icon/screenshot
	URL    puritan.String       `json:"url"`
	Width  int64                `json:"width"`
	Height int64                `json:"height"`
}

func infoFromStoreSnap(d *storeSnap) (*snap.Info, error) {
	info := &snap.Info{}
	info.RealName = d.Name.Clean()
	info.Revision = snap.R(d.Revision)
	info.SnapID = d.SnapID.Clean()
	info.EditedTitle = d.Title.Clean()
	info.EditedSummary = d.Summary.Clean()
	info.EditedDescription = d.Description.Clean()
	info.Private = d.Private
	info.Contact = d.Contact.Clean()
	info.Architectures = d.Architectures.Clean()
	info.Type = d.Type
	info.Version = d.Version.Clean()
	info.Epoch = d.Epoch
	info.Confinement = d.Confinement
	info.Base = d.Base.Clean()
	info.License = d.License.Clean()
	info.PublisherID = d.Publisher.ID.Clean()
	info.Publisher = d.Publisher.Username.Clean()
	info.DownloadURL = d.Download.URL.Clean()
	info.Size = d.Download.Size
	info.Sha3_384 = d.Download.Sha3_384.Clean()
	if len(d.Download.Deltas) > 0 {
		deltas := make([]snap.DeltaInfo, len(d.Download.Deltas))
		for i, d := range d.Download.Deltas {
			deltas[i] = snap.DeltaInfo{
				FromRevision: d.Source,
				ToRevision:   d.Target,
				Format:       d.Format.Clean(),
				DownloadURL:  d.URL.Clean(),
				Size:         d.Size,
				Sha3_384:     d.Sha3_384.Clean(),
			}
		}
		info.Deltas = deltas
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

	// convert prices
	if ps := d.Prices.Clean(); len(ps) > 0 {
		prices := make(map[string]float64, len(ps))
		for currency, priceStr := range ps {
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
		switch mediaObj.Type.Clean() {
		case "icon":
			if info.IconURL == "" {
				info.IconURL = mediaObj.URL.Clean()
			}
		case "screenshot":
			screenshots = append(screenshots, snap.ScreenshotInfo{
				URL:    mediaObj.URL.Clean(),
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
