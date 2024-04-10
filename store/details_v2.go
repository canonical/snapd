// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2023 Canonical Ltd
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
	"strings"
	"time"

	"github.com/snapcore/snapd/jsonutil/safejson"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/strutil"
)

// storeSnap holds the information sent as JSON by the store for a snap.
type storeSnap struct {
	Architectures []string            `json:"architectures"`
	Base          string              `json:"base"`
	Confinement   string              `json:"confinement"`
	Links         map[string][]string `json:"links"`
	Contact       string              `json:"contact"`
	CreatedAt     string              `json:"created-at"` // revision timestamp
	Description   safejson.Paragraph  `json:"description"`
	Download      storeDownload       `json:"download"`
	Epoch         snap.Epoch          `json:"epoch"`
	License       string              `json:"license"`
	Name          string              `json:"name"`
	Prices        map[string]string   `json:"prices"` // currency->price,  free: {"USD": "0"}
	Private       bool                `json:"private"`
	Publisher     snap.StoreAccount   `json:"publisher"`
	Revision      int                 `json:"revision"` // store revisions are ints starting at 1
	SnapID        string              `json:"snap-id"`
	SnapYAML      string              `json:"snap-yaml"` // optional
	Summary       safejson.String     `json:"summary"`
	Title         safejson.String     `json:"title"`
	Type          snap.Type           `json:"type"`
	Version       string              `json:"version"`
	Website       string              `json:"website"`
	StoreURL      string              `json:"store-url"`
	Resources     []storeResource     `json:"resources"`

	// TODO: not yet defined: channel map

	// media
	Media []storeSnapMedia `json:"media"`

	CommonIDs []string `json:"common-ids"`

	Categories []storeSnapCategory `json:"categories"`
}

type storeDownload struct {
	Sha3_384 string           `json:"sha3-384"`
	Size     int64            `json:"size"`
	URL      string           `json:"url"`
	Deltas   []storeSnapDelta `json:"deltas"`
}

type storeResource struct {
	Download    storeDownload      `json:"download"`
	Type        string             `json:"type"`
	Name        string             `json:"name"`
	Revision    int                `json:"revision"`
	Version     string             `json:"version"`
	CreatedAt   string             `json:"created-at"`
	Description safejson.Paragraph `json:"description"`
}

type storeSnapDelta struct {
	Format   string `json:"format"`
	Sha3_384 string `json:"sha3-384"`
	Size     int64  `json:"size"`
	Source   int    `json:"source"`
	Target   int    `json:"target"`
	URL      string `json:"url"`
}

type storeSnapMedia struct {
	Type   string `json:"type"` // icon/screenshot
	URL    string `json:"url"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

type storeSnapCategory struct {
	Featured bool   `json:"featured"`
	Name     string `json:"name"`
}

// storeInfoChannel is the channel description included in info results
type storeInfoChannel struct {
	Architecture string    `json:"architecture"`
	Name         string    `json:"name"`
	Risk         string    `json:"risk"`
	Track        string    `json:"track"`
	ReleasedAt   time.Time `json:"released-at"`
}

// storeInfoChannelSnap is the snap-in-a-channel of which the channel map is made
type storeInfoChannelSnap struct {
	storeSnap
	Channel storeInfoChannel `json:"channel"`
}

// storeInfo is the result of v2/info calls
type storeInfo struct {
	ChannelMap []*storeInfoChannelSnap `json:"channel-map"`
	Snap       storeSnap               `json:"snap"`
	Name       string                  `json:"name"`
	SnapID     string                  `json:"snap-id"`
}

func infoFromStoreInfo(si *storeInfo) (*snap.Info, error) {
	if len(si.ChannelMap) == 0 {
		// if a snap has no released revisions, it _could_ be returned
		// (currently no, but spec is purposely ambiguous)
		// we treat it as a 'not found' for now at least
		return nil, ErrSnapNotFound
	}

	thisOne := si.ChannelMap[0]
	thisSnap := thisOne.storeSnap // copy it as we're about to modify it
	// here we assume that the ChannelSnapInfo can be populated with data
	// that's in the channel map and not the outer snap. This is a
	// reasonable assumption today, but copyNonZeroFrom can easily be
	// changed to copy to a list if needed.
	copyNonZeroFrom(&si.Snap, &thisSnap)

	info, err := infoFromStoreSnap(&thisSnap)
	if err != nil {
		return nil, err
	}
	info.Channel = thisOne.Channel.Name
	info.Channels = make(map[string]*snap.ChannelSnapInfo, len(si.ChannelMap))
	seen := make(map[string]bool, len(si.ChannelMap))
	for _, s := range si.ChannelMap {
		ch := s.Channel
		chName := ch.Track + "/" + ch.Risk
		info.Channels[chName] = &snap.ChannelSnapInfo{
			Revision:    snap.R(s.Revision),
			Confinement: snap.ConfinementType(s.Confinement),
			Version:     s.Version,
			Channel:     chName,
			Epoch:       s.Epoch,
			Size:        s.Download.Size,
			ReleasedAt:  ch.ReleasedAt.UTC(),
		}
		if !seen[ch.Track] {
			seen[ch.Track] = true
			info.Tracks = append(info.Tracks, ch.Track)
		}
	}

	return info, nil
}

func minimalFromStoreInfo(si *storeInfo) (naming.SnapRef, *channel.Channel, error) {
	if len(si.ChannelMap) == 0 {
		// if a snap has no released revisions, it _could_ be returned
		// (currently no, but spec is purposely ambiguous)
		// we treat it as a 'not found' for now at least
		return nil, nil, ErrSnapNotFound
	}

	snapRef := naming.NewSnapRef(si.Name, si.SnapID)
	first := si.ChannelMap[0].Channel
	ch := channel.Channel{
		Architecture: first.Architecture,
		Name:         first.Name,
		Track:        first.Track,
		Risk:         first.Risk,
	}
	ch = ch.Clean()
	return snapRef, &ch, nil
}

// copy non-zero fields from src to dst
func copyNonZeroFrom(src, dst *storeSnap) {
	if len(src.Architectures) > 0 {
		dst.Architectures = src.Architectures
	}
	if src.Base != "" {
		dst.Base = src.Base
	}
	if src.Confinement != "" {
		dst.Confinement = src.Confinement
	}
	if len(src.Links) != 0 {
		dst.Links = src.Links
	}
	if src.Contact != "" {
		dst.Contact = src.Contact
	}
	if src.CreatedAt != "" {
		dst.CreatedAt = src.CreatedAt
	}
	if src.Description.Clean() != "" {
		dst.Description = src.Description
	}
	if src.Download.URL != "" {
		dst.Download = src.Download
	} else if src.Download.Size != 0 {
		// search v2 results do not contain download url, only size
		dst.Download.Size = src.Download.Size
	}
	if src.Epoch.String() != "0" {
		dst.Epoch = src.Epoch
	}
	if src.License != "" {
		dst.License = src.License
	}
	if src.Name != "" {
		dst.Name = src.Name
	}
	if len(src.Prices) > 0 {
		dst.Prices = src.Prices
	}
	if src.Private {
		dst.Private = src.Private
	}
	if src.Publisher.ID != "" {
		dst.Publisher = src.Publisher
	}
	if src.Revision > 0 {
		dst.Revision = src.Revision
	}
	if src.SnapID != "" {
		dst.SnapID = src.SnapID
	}
	if src.SnapYAML != "" {
		dst.SnapYAML = src.SnapYAML
	}
	if src.StoreURL != "" {
		dst.StoreURL = src.StoreURL
	}
	if src.Summary.Clean() != "" {
		dst.Summary = src.Summary
	}
	if src.Title.Clean() != "" {
		dst.Title = src.Title
	}
	if src.Type != "" {
		dst.Type = src.Type
	}
	if src.Version != "" {
		dst.Version = src.Version
	}
	if len(src.Media) > 0 {
		dst.Media = src.Media
	}
	if len(src.CommonIDs) > 0 {
		dst.CommonIDs = src.CommonIDs
	}
	if len(src.Categories) > 0 {
		dst.Categories = src.Categories
	}
	if len(src.Website) > 0 {
		dst.Website = src.Website
	}
	if len(src.Resources) > 0 {
		dst.Resources = src.Resources
	}
}

func infoFromStoreSnap(d *storeSnap) (*snap.Info, error) {
	info := &snap.Info{}
	// if snap-yaml is available fill in as much as possible from there
	if len(d.SnapYAML) != 0 {
		if parsedYamlInfo, err := snap.InfoFromSnapYaml([]byte(d.SnapYAML)); err == nil {
			info = parsedYamlInfo
		}
	}

	info.RealName = d.Name
	info.Revision = snap.R(d.Revision)
	info.SnapID = d.SnapID

	// https://forum.snapcraft.io/t/title-length-in-snapcraft-yaml-snap-yaml/8625/10
	info.EditedTitle = strutil.ElliptRight(d.Title.Clean(), 40)

	info.EditedSummary = d.Summary.Clean()
	info.EditedDescription = d.Description.Clean()
	info.Private = d.Private
	// needs to be set for old snapd
	info.LegacyEditedContact = d.Contact
	// info.EditedLinks should contain normalized edited links. info.Links() normalizes
	// non-empty edited links, otherwise it returns normalized original links.
	if len(d.Links) != 0 {
		info.EditedLinks = d.Links
		info.EditedLinks = info.Links()
	}
	info.Architectures = d.Architectures
	info.SnapType = d.Type
	info.Version = d.Version
	info.Epoch = d.Epoch
	info.Confinement = snap.ConfinementType(d.Confinement)
	info.Base = d.Base
	info.License = d.License
	info.Publisher = d.Publisher
	info.DownloadInfo = downloadInfoFromStoreDownload(d.Download)
	info.CommonIDs = d.CommonIDs
	if len(info.EditedLinks) == 0 {
		// if non empty links was provided, no need to set this
		// separately as in itself it is not persisted
		info.LegacyWebsite = d.Website
	}
	info.StoreURL = d.StoreURL

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

	// if snap-yaml is not available, try to fill in components from the
	// resources available
	if d.SnapYAML == "" {
		addComponents(info, d.Resources)
	}

	// media
	addMedia(info, d.Media)

	addCategories(info, d.Categories)

	return info, nil
}

func componentFromStoreResource(r storeResource) (*snap.Component, bool) {
	compType, ok := strings.CutPrefix(r.Type, "component/")
	if !ok {
		return nil, false
	}

	comp := &snap.Component{
		Name:        r.Name,
		Summary:     r.Description.Clean(),
		Description: r.Description.Clean(),
		Type:        snap.ComponentType(compType),

		// unable to fill the rest of the struct from a store resource
	}

	return comp, true
}

func addComponents(info *snap.Info, resources []storeResource) {
	for _, r := range resources {
		if comp, ok := componentFromStoreResource(r); ok {
			if info.Components == nil {
				info.Components = make(map[string]*snap.Component)
			}

			info.Components[comp.Name] = comp
		}
	}
}

func downloadInfoFromStoreDownload(d storeDownload) snap.DownloadInfo {
	downloadInfo := snap.DownloadInfo{
		DownloadURL: d.URL,
		Size:        d.Size,
		Sha3_384:    d.Sha3_384,
	}

	if len(d.Deltas) > 0 {
		downloadInfo.Deltas = make([]snap.DeltaInfo, 0, len(d.Deltas))
		for _, d := range d.Deltas {
			downloadInfo.Deltas = append(downloadInfo.Deltas, snap.DeltaInfo{
				FromRevision: d.Source,
				ToRevision:   d.Target,
				Format:       d.Format,
				DownloadURL:  d.URL,
				Size:         d.Size,
				Sha3_384:     d.Sha3_384,
			})
		}
	}

	return downloadInfo
}

func addMedia(info *snap.Info, media []storeSnapMedia) {
	if len(media) == 0 {
		return
	}
	info.Media = make(snap.MediaInfos, len(media))
	for i, mediaObj := range media {
		info.Media[i].Type = mediaObj.Type
		info.Media[i].URL = mediaObj.URL
		info.Media[i].Width = mediaObj.Width
		info.Media[i].Height = mediaObj.Height
	}
}

func addCategories(info *snap.Info, categories []storeSnapCategory) {
	if len(categories) == 0 {
		return
	}
	info.Categories = make([]snap.CategoryInfo, len(categories))
	for i, category := range categories {
		info.Categories[i].Featured = category.Featured
		info.Categories[i].Name = category.Name
	}
}
