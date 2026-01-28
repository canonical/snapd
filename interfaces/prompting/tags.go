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

package prompting

import (
	"github.com/snapcore/snapd/interfaces/apparmor"
	prompting_errors "github.com/snapcore/snapd/interfaces/prompting/errors"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
)

var (
	apparmorInterfaceForMetadataTag = apparmor.InterfaceForMetadataTag
)

// interfaceFromTagsets returns the interface associated with all of the given
// tagsets.
//
// Potential interfaces are identified by looking up whether a given tag is
// registered in association with a particular snapd interface.
//
// If none of the given tags are registered with an interface, then returns
// ErrNoInterfaceTags.
//
// If tags are registered with more than one interface, then returns
// ErrMultipleInterfaces.
//
// If one interface is associated with tags in the tagsets but those tags are
// not found in every tagset, then returns ErrNoCommonInterface.
func interfaceFromTagsets(tagsets notify.TagsetMap) (iface string, err error) {
	if len(tagsets) == 0 {
		return "", prompting_errors.ErrNoInterfaceTags
	}

	aPermHadNoInterfaces := false
	for _, tagset := range tagsets {
		thisPermHasNoInterfaces := true
		for _, tag := range tagset {
			tagIface, ok := apparmorInterfaceForMetadataTag(tag)
			if !ok {
				continue
			}
			if iface != "" && tagIface != iface {
				// We already saw a different interface
				return "", prompting_errors.ErrMultipleInterfaces
			}
			iface = tagIface
			thisPermHasNoInterfaces = false
		}
		if thisPermHasNoInterfaces {
			aPermHadNoInterfaces = true
		}
	}

	if iface == "" {
		// No tags matched any interface
		return "", prompting_errors.ErrNoInterfaceTags
	}

	if aPermHadNoInterfaces {
		// At least one tagset matched an interface, but not all of them
		return "", prompting_errors.ErrNoCommonInterface
	}

	return iface, nil
}
