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

package systemd_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil/systemd"
)

func Test(t *testing.T) { TestingT(t) }

type escapeSuite struct{}

var _ = Suite(&escapeSuite{})

func (ts *escapeSuite) TestEscapePath(c *C) {
	tt := []struct {
		in      string
		out     string
		comment string
	}{
		{
			``,
			``,
			`empty string`,
		},
		{
			`/`,
			`-`,
			`root /`,
		},
		{
			`-`,
			`\x2d`,
			`just dash`,
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
		c.Assert(systemd.EscapePath(t.in), Equals, t.out, Commentf(t.comment+" (with input %s)", t.in))
	}
}
