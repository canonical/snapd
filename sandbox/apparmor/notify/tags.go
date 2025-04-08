// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package notify

import (
	"sort"

	"github.com/snapcore/snapd/interfaces/apparmor"
)

var (
	apparmorInterfaceForMetadataTag = apparmor.InterfaceForMetadataTag
)

// MetadataTags is a list of tags received from AppArmor in the kernel.
type MetadataTags []string

// TagsetMap maps from permission mask to the ordered list of tags associated
// with those permissions, as received from AppArmor in the kernel.
type TagsetMap map[AppArmorPermission]MetadataTags

// InterfaceForPermission computes the interface associated with the message
// based on the map of metadata tags and the given permission mask, if one is
// present and registered. If not, returns false.
//
// First gets the ordered list of tags which apply to any permission in the
// given permission mask, and then selects the interface of the first tag in
// the list which is registered with a snapd interface.
func (tm TagsetMap) InterfaceForPermission(perm AppArmorPermission) (string, bool) {
	orderedTags := tm.metadataTagsForPermission(perm)
	for _, tag := range orderedTags {
		if iface, ok := apparmorInterfaceForMetadataTag(tag); ok {
			return iface, true
		}
	}
	return "", false
}

// metadataTagsForPermission extracts the metadata tags which apply to the
// given permission mask in the order in which the kernel provided them.
//
// The caller should pass in the permission mask for the permissions which were
// initially denied by AppArmor rules, as these are the permissions for which
// the request applies.
//
// Ideally, only one tagset applies for the given permission, and the tags in
// that tagset can be returned directly. If the permission mask includes
// permissions associated with more than one tagset, then the metadata tags
// from all matching tagsets are concatenated into one list, which may contain
// duplicate tags if the same tag occurs in more than one tagset. The tags
// from a given tagset remain ordered, but the tagsets are concatenated in
// an arbitrary (but consistent) order.
func (tm TagsetMap) metadataTagsForPermission(perm AppArmorPermission) MetadataTags {
	matchingTagsets := make([]permAndTags, 0, 1)
	totalTags := 0
	for permMask, tags := range tm {
		overlap := perm.AsAppArmorOpMask() & permMask.AsAppArmorOpMask()
		if overlap == 0 {
			continue
		}
		matchingTagsets = append(matchingTagsets, permAndTags{
			perm: overlap,
			tags: tags,
		})
		totalTags += len(tags)
	}
	sort.Slice(matchingTagsets, func(i, j int) bool {
		return matchingTagsets[i].perm < matchingTagsets[j].perm
	}) // TODO: use slices.Sort once on go 1.21+

	concatenated := make(MetadataTags, 0, totalTags)
	for _, tags := range matchingTagsets {
		concatenated = append(concatenated, tags.tags...)
	}
	return concatenated
}

type permAndTags struct {
	perm uint32
	tags MetadataTags
}
