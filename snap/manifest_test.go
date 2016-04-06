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

package snap_test

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/snap"
)

type manifestSuite struct{}

var _ = Suite(&manifestSuite{})

func (s *manifestSuite) TestCompleteInfo(c *C) {
	info := &snap.Info{
		Name:        "name",
		Revision:    1,
		Channel:     "channel",
		Developer:   "devel",
		Summary:     "summary",
		Description: "desc",
		Size:        22,
		Sha512:      "sha",
		IconURL:     "iconURL",
	}

	mf := snap.ManifestFromInfo(info)
	info1 := &snap.Info{}

	snap.CompleteInfo(info1, mf)

	c.Check(info1, DeepEquals, info)

	// nops
	snap.CompleteInfo(info1, nil)
	snap.CompleteInfo(info1, &snap.Manifest{})
	c.Check(info1, DeepEquals, info)
}

func (s *manifestSuite) TestCompleteInfoOverrides(c *C) {
	info := &snap.Info{
		Name:        "name",
		Revision:    1,
		Channel:     "channel",
		Developer:   "devel",
		Summary:     "summary",
		Description: "desc",
		Size:        22,
		Sha512:      "sha",
		IconURL:     "iconURL",
	}

	snap.CompleteInfo(info, &snap.Manifest{
		Name:        "newname",
		Summary:     "fixed summary",
		Description: "fixed desc",
	})

	c.Check(info, DeepEquals, &snap.Info{
		Name:        "newname",
		Revision:    1,
		Channel:     "channel",
		Developer:   "devel",
		Summary:     "fixed summary",
		Description: "fixed desc",
		Size:        22,
		Sha512:      "sha",
		IconURL:     "iconURL",
	})
}
