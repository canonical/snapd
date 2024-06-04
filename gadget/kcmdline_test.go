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
	"fmt"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/snap/snaptest"
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
    - par_4=val_1-2
    - par5="foo bar"
`
	tests := []struct {
		cmdline   string
		allowed   string
		forbidden string
	}{
		// Allowed
		{"par1", "par1", ""},
		{"par2=val", "par2=val", ""},
		{"par3=random_stuff", "par3=random_stuff", ""},
		{"par3", "par3", ""},
		{"par3=", "par3", ""},
		{`par3=""`, "par3", ""},
		{`par3="foo bar"`, `par3="foo bar"`, ""},
		{"", "", ""},
		{"par2=val par1 par3=.1.32", "par2=val par1 par3=.1.32", ""},
		{"par_4=val_1-2", "par_4=val_1-2", ""},
		{`par2="val"`, `par2="val"`, ""},
		{`par5="foo bar"`, `par5="foo bar"`, ""},
		// Not allowed
		{"foo", "", "foo"},
		{"par2=other", "", "par2=other"},
		{"par2=val par1 par3=.1.32 par2=other", "par2=val par1 par3=.1.32", "par2=other"},
	}

	for i, t := range tests {
		c.Logf("%v: cmdline %q", i, t.cmdline)
		gi, err := gadget.InfoFromGadgetYaml([]byte(yaml), uc20Mod)
		c.Assert(err, IsNil)
		allowed, forbidden := gadget.FilterKernelCmdline(t.cmdline, gi.KernelCmdline.Allow)
		c.Check(allowed, Equals, t.allowed)
		c.Check(forbidden, Equals, t.forbidden)
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
		cmdline   string
		allowed   string
		forbidden string
	}{
		// Allowed
		{"par1=*", "par1=*", ""},
		{`par1="*"`, `par1="*"`, ""},
		// Not allowed as only a literal '*' is allowed
		{"par1=val", "", "par1=val"},
	}

	for i, t := range tests {
		c.Logf("%v: cmdline %q", i, t.cmdline)
		gi, err := gadget.InfoFromGadgetYaml([]byte(yaml), uc20Mod)
		c.Assert(err, IsNil)
		allowed, forbidden := gadget.FilterKernelCmdline(t.cmdline, gi.KernelCmdline.Allow)
		c.Check(allowed, Equals, t.allowed)
		c.Check(forbidden, Equals, t.forbidden)
	}
}

func (s *gadgetYamlTestSuite) TestCheckCmdlineAppend(c *C) {
	const gadgetSnapYaml = `name: gadget
version: 1.0
type: gadget
`

	const yamlTemplate = `
volumes:
  pc:
    bootloader: grub
kernel-cmdline:
  append:
    - '%s'
`

	tests := []string{
		`par`,
		`"par with spaces"`,
		`par=value`,
		`par="value with spaces"`,
		`par=*`,
	}

	for _, t := range tests {
		yaml := fmt.Sprintf(yamlTemplate, t)

		snap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
			{"meta/gadget.yaml", yaml},
		})
		cmdline, full, removeArgs, err := gadget.KernelCommandLineFromGadget(snap, uc20Mod)

		c.Assert(err, IsNil)
		c.Check(cmdline, Equals, t)
		c.Check(len(removeArgs), Equals, 0)
		c.Check(full, Equals, false)
	}
}

func (s *gadgetYamlTestSuite) TestCheckCmdlineAppendForbidden(c *C) {
	const gadgetSnapYaml = `name: gadget
version: 1.0
type: gadget
`

	const yamlTemplate = `
volumes:
  pc:
    bootloader: grub
kernel-cmdline:
  append:
    - '%s'
`
	tests := []string{
		`init`,
		`init=/myevilinit`,
	}

	for _, t := range tests {
		yaml := fmt.Sprintf(yamlTemplate, t)

		snap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
			{"meta/gadget.yaml", yaml},
		})
		_, _, _, err := gadget.KernelCommandLineFromGadget(snap, uc20Mod)

		c.Check(err, ErrorMatches, fmt.Sprintf(`kernel parameter '%s' is not allowed`, t))
	}
}

func qka(param, value string) kcmdline.Argument {
	return kcmdline.Argument{Param: param, Value: value, Quoted: true}
}

func ka(param, value string) kcmdline.Argument {
	return kcmdline.Argument{Param: param, Value: value, Quoted: false}
}

func (s *gadgetYamlTestSuite) TestCheckCmdlineRemove(c *C) {
	const gadgetSnapYaml = `name: gadget
version: 1.0
type: gadget
`

	const yamlTemplate = `
volumes:
  pc:
    bootloader: grub
kernel-cmdline:
  remove:
%s
`
	const yamlRemoveLineTemplate = `
   - %s
`
	tests := []struct {
		matches            []string
		expectedRemoved    []kcmdline.Argument
		expectedNotRemoved []kcmdline.Argument
	}{
		{[]string{`mock`},
			[]kcmdline.Argument{ka(`mock`, ``)},
			[]kcmdline.Argument{ka(`static`, ``)}},
		{[]string{`something=*`},
			[]kcmdline.Argument{ka(`something`, `foo`), ka(`something`, ``), qka(`something`, `*`)},
			[]kcmdline.Argument{ka(`somethingelse`, `value`), ka(`something else`, ``)}},
		{[]string{`something="*"`},
			[]kcmdline.Argument{ka(`something`, `*`), qka(`something`, `*`)},
			[]kcmdline.Argument{ka(`something`, `foo`)}},
		{[]string{`something="something with value"`},
			[]kcmdline.Argument{qka(`something`, `something with value`)},
			[]kcmdline.Argument{qka(`something`, `something with other value`)}},
	}

	for _, t := range tests {
		lines := ""
		for _, m := range t.matches {
			lines += fmt.Sprintf(yamlRemoveLineTemplate, m)
		}
		yaml := fmt.Sprintf(yamlTemplate, lines)

		snap := snaptest.MakeTestSnapWithFiles(c, gadgetSnapYaml, [][]string{
			{"meta/gadget.yaml", yaml},
		})
		cmdline, full, removeArgs, err := gadget.KernelCommandLineFromGadget(snap, uc20Mod)
		c.Assert(err, IsNil)
		c.Check(cmdline, Equals, "")
		c.Check(full, Equals, false)

		matcher := kcmdline.NewMatcher(removeArgs)
		for _, e := range t.expectedRemoved {
			c.Check(matcher.Match(e), Equals, true)
		}
		for _, e := range t.expectedNotRemoved {
			c.Check(matcher.Match(e), Equals, false)
		}
	}
}
