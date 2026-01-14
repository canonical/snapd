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

var shortComponentHelp = i18n.G("Show detailed information about snap components")
var longComponentHelp = i18n.G(`
The component command shows detailed information about snap components.

You must specify exactly one snap and one or more of its components in the form
<snap>+<component>+[<component>...]. names are looked for both in the
store and in the installed snaps.
`)

type cmdComponent struct {
	colorMixin
	waitMixin

	Positional struct {
		SnapsAndComponents []remoteSnapName `positional-arg-name:"<snap+component+[component...]>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("component", shortComponentHelp, longComponentHelp, func() flags.Commander { return &cmdComponent{} }, nil, nil)
}

func (x *cmdComponent) showComponents() error {
	snapName, comps := snap.SplitSnapInstanceAndComponents(string(x.Positional.SnapsAndComponents[0]))
	if x.Positional.SnapsAndComponents[0] == "" {
		return errors.New(i18n.G("argument cannot be empty"))
	}

	if len(comps) == 0 {
		return errors.New(i18n.G("no component specified"))
	}

	if snapName == "" {
		return errors.New(i18n.G("no snap for the component(s) was specified"))
	}

	for _, compName := range comps {
		if compName == "" {
			return errors.New(i18n.G("component name cannot be empty"))
		}
	}

	names := []string{snapName}
	snaps, err := x.client.List(names, nil)
	if err != nil {
		if err == client.ErrNoSnapsInstalled {
			return ErrNoMatchingSnaps
		}
		return err
	}

	matchingSnap := snaps[0]

	validPrinted := false

	for i, compName := range comps {
		comp := compByName(compName, matchingSnap)
		if comp == nil && len(comps) == 1 {
			return fmt.Errorf(i18n.G("no component %q found for snap %q"), compName, matchingSnap.Name)
		} else if comp == nil {
			fmt.Fprintf(Stdout, "warning: no component %q found for snap %q\n", compName, matchingSnap.Name)
			continue
		}

		fmt.Fprintf(Stdout, "component: %s+%s\n", matchingSnap.Name, comp.Name)
		fmt.Fprintf(Stdout, "type: %s\n", comp.Type)
		fmt.Fprintf(Stdout, "summary: %s\n", comp.Summary)
		fmt.Fprintf(Stdout, "description: |\n  %s\n", comp.Description)
		if comp.Version != "" {
			fmt.Fprintf(Stdout, "installed: %s (%s) %s\n", comp.Version, comp.Revision.String(), strutil.SizeToStr(comp.InstalledSize))
		}

		if i < len(comps)-1 {
			fmt.Fprintln(Stdout, "---")
		}

		validPrinted = true
	}

	if !validPrinted {
		return errors.New(i18n.G("no valid components given"))
	}

	return nil
}

func (c *cmdComponent) Execute([]string) error {
	if len(c.Positional.SnapsAndComponents) != 1 {
		return errors.New(i18n.G("exactly one snap and one or more of its components must be specified"))
	}
	return c.showComponents()
}
