// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package systemd_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/systemd"
)

func (ts *SystemdTestSuite) TestEscapePath(c *C) {
	tt := []struct {
		in      string
		out     string
		comment string
	}{
		{
			`Hallöchen, Meister`,
			`Hall\xc3\xb6chen\x2c\x20Meister`,
			`utf-8 char and spaces`,
		},
		{
			`/tmp//waldi/foobar/`,
			`tmp-waldi-foobar`,
			`unclean path`,
		},
		{
			`/.foo/.bar`,
			`\x2efoo-.bar`,
			`leading dot escaped differently`,
		},
		{
			`.foo/.bar`,
			`\x2efoo-.bar`,
			`leading dot escaped differently (without leading slash)`,
		},
		{
			// TODO: this case shouldn't work it should cause an error to be
			// fully compatible with systemd-escape(1), but it's not documented
			// how or why systemd-escape considers this kind of path to be
			// invalid, so for now we just process it in an expected and
			// deterministic way
			`/top_level_1/../top_level_2/`,
			`top_level_2`,
			`invalid path`,
		},
		{
			``,
			`-`,
			`empty string`,
		},
		{
			`/`,
			`-`,
			`root /`,
		},
		{
			`////////`,
			`-`,
			`root / unclean`,
		},
		{
			`-`,
			`\x2d`,
			`just dash`,
		},
		{
			`/run/ubuntu------data`,
			`run-ubuntu\x2d\x2d\x2d\x2d\x2d\x2ddata`,
			`consecutive dashes`,
		},
		{
			`/path-with-forward-slash\`,
			`path\x2dwith\x2dforward\x2dslash\x5c`,
			`forward slash in path element`,
		},
		{
			`/run/1000/numbers`,
			`run-1000-numbers`,
			`numbers`,
		},
		{
			`/silly/path/,!@#$%^&*()`,
			`silly-path-\x2c\x21\x40\x23\x24\x25\x5e\x26\x2a\x28\x29`,
			`ascii punctuation`,
		},
		{
			`/run/mnt/data`,
			`run-mnt-data`,
			`typical data initramfs mount`,
		},
		{
			`run/mnt/data`,
			`run-mnt-data`,
			`typical data initramfs mount w/o leading slash`,
		},
		{
			`/run/mnt/ubuntu-seed`,
			`run-mnt-ubuntu\x2dseed`,
			`typical ubuntu-seed initramfs mount`,
		},
		{
			`/run/mnt/ubuntu data`,
			`run-mnt-ubuntu\x20data`,
			`path with space in it`,
		},
		{
			`/run/mnt/ubuntu_data`,
			`run-mnt-ubuntu_data`,
			`path with underscore in it`,
		},
		{
			` `,
			`\x20`,
			"space character",
		},
		{
			`	`,
			`\x09`,
			`tab character`,
		},
		{
			`/home/日本語`,
			`home-\xe6\x97\xa5\xe6\x9c\xac\xe8\xaa\x9e`,
			`utf-8 characters`,
		},
		{
			`/path⌘place-of-interest/hello`,
			`path\xe2\x8c\x98place\x2dof\x2dinterest-hello`,
			`utf-8 character embedded in ascii path element`,
		},
	}

	for _, t := range tt {
		c.Assert(systemd.EscapeUnitNamePath(t.in), Equals, t.out, Commentf(t.comment+" (with input %q)", t.in))
	}
}

func (ts *SystemdTestSuite) TestUnitNameFromSecurityTag(c *C) {
	type test struct {
		tag  string
		unit string
		err  string
	}

	cases := []test{
		{
			tag:  `snap.name.app`,
			unit: `snap.name.app`,
		},
		{
			tag:  `snap.name_key.app`,
			unit: `snap.name_key.app`,
		},
		{
			tag:  `snap.name.APP`,
			unit: `snap.name.APP`,
		},
		{
			tag:  `snap.other-name.app`,
			unit: `snap.other-name.app`,
		},
		{
			tag:  `snap.name.hook.pre-refresh`,
			unit: `snap.name.hook.pre-refresh`,
		},
		{
			tag:  `snap.name+comp.hook.install`,
			unit: `snap.name\x2bcomp.hook.install`,
		},
		{
			tag: `snap.name/comp.hook.install`,
			err: `invalid character in security tag: '/'`,
		},
	}

	for _, t := range cases {
		unit, err := systemd.UnitNameFromSecurityTag(t.tag)
		if t.err == "" {
			c.Check(unit, Equals, t.unit)
			c.Check(err, IsNil)

			// if we don't expect the conversion to fail, check that the inverse
			// function works too
			tag := systemd.SecurityTagFromUnitName(unit)
			c.Check(tag, Equals, t.tag)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}
