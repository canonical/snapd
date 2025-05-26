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

package main_test

import (
	"context"

	. "gopkg.in/check.v1"

	main "github.com/snapcore/snapd/cmd/snap-gpio-helper"
	"github.com/snapcore/snapd/strutil"
)

func (s *snapGpioHelperSuite) TestExportGpioChardevBadLine(c *C) {
	exportCalled := 0
	restore := main.MockGpioExportGadgetChardevChip(func(ctx context.Context, chipLabels []string, lines strutil.Range, gadgetName, slotName string) error {
		exportCalled++
		return nil
	})
	defer restore()

	ensureDriverCalled := 0
	restore = main.MockGpioEnsureAggregatorDriver(func() error {
		ensureDriverCalled++
		return nil
	})
	defer restore()

	for lines, expectedErr := range map[string]string{
		"0-2,1": `invalid lines argument: overlapping range span found "1"`,
		"1-0":   `invalid lines argument: invalid range span "1-0": ends before it starts`,
		"0-":    `invalid lines argument: .*: invalid syntax`,
		"a":     `invalid lines argument: .*: invalid syntax`,
	} {
		err := main.Run([]string{
			"export-chardev", "label-0", lines, "gadget-name", "slot-name",
		})
		c.Check(err, ErrorMatches, expectedErr)
	}

	c.Assert(exportCalled, Equals, 0)
	c.Assert(ensureDriverCalled, Equals, 4)
}

func (s *snapGpioHelperSuite) TestExportGpioChardev(c *C) {
	exportCalled := 0
	restore := main.MockGpioExportGadgetChardevChip(func(ctx context.Context, chipLabels []string, lines strutil.Range, gadgetName, slotName string) error {
		exportCalled++
		c.Check(chipLabels, DeepEquals, []string{"label-0", "label-1"})
		c.Check(lines, DeepEquals, strutil.Range{
			{Start: 0, End: 6},
			{Start: 7, End: 7},
			{Start: 8, End: 100},
		})
		c.Check(gadgetName, Equals, "gadget-name")
		c.Check(slotName, Equals, "slot-name")
		return nil
	})
	defer restore()

	ensureDriverCalled := 0
	restore = main.MockGpioEnsureAggregatorDriver(func() error {
		ensureDriverCalled++
		return nil
	})
	defer restore()

	err := main.Run([]string{
		"export-chardev", "label-0,label-1", "7,0-6,8-100", "gadget-name", "slot-name",
	})
	c.Check(err, IsNil)
	c.Assert(exportCalled, Equals, 1)
	c.Assert(ensureDriverCalled, Equals, 1)
}
