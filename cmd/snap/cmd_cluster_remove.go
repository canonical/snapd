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

package main

import (
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

type cmdClusterRemove struct {
	clientMixin
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"true" required:"true"`
}

var shortClusterRemoveHelp = i18n.G("Remove a snap from the uncommitted cluster state")
var longClusterRemoveHelp = i18n.G(`
The cluster remove command removes a snap from the uncommitted cluster state.

This command modifies the cluster configuration that will be applied when
the cluster assertion is committed. The snap will be marked as removed and
will not be installed on devices in the cluster once the configuration is
committed and applied.

Example:
  snap cluster remove hello-world
`)

func init() {
	addClusterCommand("remove", shortClusterRemoveHelp, longClusterRemoveHelp, func() flags.Commander {
		return &cmdClusterRemove{}
	}, nil, []argDesc{{
		// TRANSLATORS: This should not start with a lowercase letter.
		name: "<snap>",
		desc: i18n.G("Name of the snap to remove from the cluster"),
	}})
}

func (x *cmdClusterRemove) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if x.Positional.Snap == "" {
		return fmt.Errorf("snap name is required")
	}

	if err := x.client.ClusterRemove(x.Positional.Snap); err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Marked %q as removed in uncommitted cluster state.\n"), x.Positional.Snap)
	return nil
}
