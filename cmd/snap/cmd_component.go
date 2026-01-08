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

package main

import (
	"errors"
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var shortComponentHelp = i18n.G("Show information about installed snap components")
var longComponentHelp = i18n.G(`
The component command shows information about installed snap components.
You must specify exactly one snap and its component(s) in the form
<snap>+<component1>...+<componentN>.
`)

type cmdComponent struct {
	colorMixin
	waitMixin

	Positional struct {
		SnapsAndComponents []remoteSnapName `positional-arg-name:"<snap+component>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("component", shortComponentHelp, longComponentHelp, func() flags.Commander { return &cmdComponent{} }, nil, nil)
}

func (x *cmdComponent) showComponent() error {
	snapName, comps := snap.SplitSnapInstanceAndComponents(string(x.Positional.SnapsAndComponents[0]))
	if snapName == "" {
		return errors.New(i18n.G("no snap for the component(s) was specified"))
	}

	if len(comps) == 0 {
		return errors.New(i18n.G("no components specified"))
	}

	names := []string{snapName}
	snaps, err := x.client.List(names, nil)
	if err != nil {
		if err == client.ErrNoSnapsInstalled {
			return errors.New(i18n.G("no matching snaps installed"))
		}
		return err
	}

	for i := 0; i < len(comps); i++ {
		comp := compByName(comps[i], snaps[0])

		if comp == nil {
			return fmt.Errorf(i18n.G("component %q for snap %q is not installed"), comps[i], snaps[0].Name)
		}

		fmt.Fprintf(Stdout, "component: %s+%s\n", snaps[0].Name, comp.Name)
		fmt.Fprintf(Stdout, "type: %s\n", comp.Type)
		fmt.Fprintf(Stdout, "summary: %s\n", comp.Summary)
		fmt.Fprintf(Stdout, "description: |\n  %s\n", comp.Description)
		fmt.Fprintf(Stdout, "installed: %s (%s) %s\n", comp.Version, comp.Revision.String(), strutil.SizeToStr(comp.InstalledSize))

		if i < len(comps)-1 {
			fmt.Fprintln(Stdout)
		}
	}

	return nil
}

func (c *cmdComponent) Execute([]string) error {
	if len(c.Positional.SnapsAndComponents) != 1 {
		return errors.New(i18n.G("exactly one snap and its components must be specified"))
	}
	return c.showComponent()
}
