// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/snap"
)

// storeSearchChannelSnap is the snap revision plus a channel name
type storeSearchChannelSnap struct {
	storeSnap
	Channel string `json:"channel"`
}

// storeSearchResult is the result of v2/find calls
type storeSearchResult struct {
	Revision storeSearchChannelSnap `json:"revision"`
	Snap     storeSnap              `json:"snap"`
	Name     string                 `json:"name"`
	SnapID   string                 `json:"snap-id"`
}

func infoFromStoreSearchResult(si *storeSearchResult) (*snap.Info, error) {
	thisSnap := si.Snap
	copyNonZeroFrom(&si.Revision.storeSnap, &thisSnap)

	info := mylog.Check2(infoFromStoreSnap(&thisSnap))

	info.SnapID = si.SnapID
	info.RealName = si.Name
	info.Channel = si.Revision.Channel
	return info, nil
}
