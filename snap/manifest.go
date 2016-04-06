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

package snap

// Manifest holds snap metadata that is not included in snap.yaml or for which the store is the canonical source.
// It is a subset of what Info can hold.
// It can be marshalled both as JSON and YAML.
type Manifest struct {
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// XXX likely we want also snap-id
	Revision    int    `yaml:"revision" json:"revision"`
	Channel     string `yaml:"channel,omitempty" json:"channel,omitempty"`
	Developer   string `yaml:"developer,omitempty" json:"developer,omitempty"`
	Summary     string `yaml:"summary,omitempty" json:"summary,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Size        int64  `yaml:"size,omitempty" json:"size,omitempty"`
	Sha512      string `yaml:"sha512,omitempty" json:"sha512,omitempty"`
	IconURL     string `yaml:"icon-url,omitempty" json:"icon-url,omitempty"`
}

// ManifestFromInfo extracts the manifest subset from the given info
func ManifestFromInfo(info *Info) *Manifest {
	return &Manifest{
		Name:        info.Name,
		Revision:    info.Revision,
		Channel:     info.Channel,
		Developer:   info.Developer,
		Summary:     info.Summary,
		Description: info.Description,
		Size:        info.Size,
		Sha512:      info.Sha512,
		IconURL:     info.IconURL,
	}
}

// CompleteInfo complete the info using values from the given manifest.
// mf can be nil in which case nothing is done.
func CompleteInfo(info *Info, mf *Manifest) {
	if mf == nil {
		return
	}
	if mf.Name != "" {
		info.Name = mf.Name
	}
	if mf.Revision != 0 {
		info.Revision = mf.Revision
	}
	if mf.Channel != "" {
		info.Channel = mf.Channel
	}
	if mf.Developer != "" {
		info.Developer = mf.Developer
	}
	if mf.Summary != "" {
		info.Summary = mf.Summary
	}
	if mf.Description != "" {
		info.Description = mf.Description
	}
	if mf.Size != 0 {
		info.Size = mf.Size
	}
	if mf.Sha512 != "" {
		info.Sha512 = mf.Sha512
	}
	if mf.IconURL != "" {
		info.IconURL = mf.IconURL
	}
}
