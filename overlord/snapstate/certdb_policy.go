// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate

import (
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
)

type UpdateCertDBForRefreshOptions struct {
	DeviceCtx    DeviceContext
	IsRefresh    bool
	SnapType     snap.Type
	InstanceName string
}

// ShouldScheduleUpdateCertDBForRefresh reports whether a refresh operation for
// a specific snap should inject an update-cert-db task.
func ShouldScheduleUpdateCertDBForRefresh(opts UpdateCertDBForRefreshOptions) bool {
	if !opts.IsRefresh {
		return false
	}

	// There must be a device context, and we should not be remodelling, that
	// check/handling is done elsewhere
	if opts.DeviceCtx == nil || opts.DeviceCtx.ForRemodeling() {
		return false
	}

	if opts.SnapType != snap.TypeBase {
		return false
	}

	model := opts.DeviceCtx.Model()
	if model.Classic() {
		return false
	}

	return opts.InstanceName == model.Base()
}

// ShouldScheduleUpdateCertDBForModelChange reports whether a model transition
// should inject an update-cert-db task.
func ShouldScheduleUpdateCertDBForModelChange(current, new *asserts.Model) bool {
	if current == nil || new == nil {
		return false
	}

	// When upgrading the boot base and when the track is changed.
	if current.Base() != "" && new.Base() != "" && current.Base() != new.Base() {
		return true
	}

	baseTrack := func(ms *asserts.ModelSnap) string {
		if ms == nil {
			return ""
		}
		if ms.PinnedTrack != "" {
			return ms.PinnedTrack
		}
		ch, err := channel.ParseVerbatim(ms.DefaultChannel, "-")
		if err != nil {
			return ""
		}
		return ch.Track
	}

	return baseTrack(current.BaseSnap()) != baseTrack(new.BaseSnap())
}
