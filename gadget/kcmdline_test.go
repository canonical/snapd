// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package gadget_test

import (
	"github.com/snapcore/snapd/gadget"
	. "gopkg.in/check.v1"
)

func (s *gadgetYamlTestSuite) TestCheckCmdlineAllowed(c *C) {
	const yaml = `
volumes:
  pc:
    bootloader: grub
kernel-cmdline:
  allow:
    - par1
    - par2=val
    - par3=*
    - par-4=val_1-2
`
	tests := []struct {
		cmdline string
		err     string
	}{
		// Allowed
		{"par1", ""},
		{"par2=val", ""},
		{"par3=random_stuff", ""},
		{"par3", ""},
		{"par3=", ""},
		{`par3=""`, ""},
		{"", ""},
		{"par2=val par1 par3=.1.32", ""},
		{"par_4=val_1-2", ""},
		// Not allowed
		{"foo", `"foo=" is not an allowed kernel argument`},
		{"par2=other", `"par2=other" is not an allowed kernel argument`},
		{"par2=val par1 par3=.1.32 par2=other", `"par2=other" is not an allowed kernel argument`},
		{"par-4=val_1_2", `"par_4=val_1_2" is not an allowed kernel argument`},
	}

	for i, t := range tests {
		c.Logf("%v: cmdline %q", i, t.cmdline)
		gi, err := gadget.InfoFromGadgetYaml([]byte(yaml), uc20Mod)
		c.Assert(err, IsNil)
		err = gadget.CheckCmdlineAllowed(t.cmdline, gi.KernelCmdline.Allow)
		if t.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, t.err)
		}
	}
}

func (s *gadgetYamlTestSuite) TestCheckCmdlineAllowedStarLiteral(c *C) {
	const yaml = `
volumes:
  pc:
    bootloader: grub
kernel-cmdline:
  allow:
    - par1="*"
`
	tests := []struct {
		cmdline string
		err     string
	}{
		// Allowed
		{"par1=*", ""},
		{`par1="*"`, ""},
		// Not allowed as only a literal '*' is allowed
		{"par1=val", `\"par1=val\" is not an allowed kernel argument`},
	}

	for i, t := range tests {
		c.Logf("%v: cmdline %q", i, t.cmdline)
		gi, err := gadget.InfoFromGadgetYaml([]byte(yaml), uc20Mod)
		c.Assert(err, IsNil)
		err = gadget.CheckCmdlineAllowed(t.cmdline, gi.KernelCmdline.Allow)
		if t.err == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, t.err)
		}
	}
}
