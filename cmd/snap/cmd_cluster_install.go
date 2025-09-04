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

type cmdClusterInstall struct {
	clientMixin
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"true" required:"true"`
}

var shortClusterInstallHelp = i18n.G("Add a snap to the uncommitted cluster state")
var longClusterInstallHelp = i18n.G(`
The cluster install command adds a snap to the uncommitted cluster state.

This command modifies the cluster configuration that will be applied when
the cluster assertion is committed. The snap will be installed on all
devices in the cluster once the configuration is committed and applied.

Example:
  snap cluster install hello-world
`)

func init() {
	addClusterCommand("install", shortClusterInstallHelp, longClusterInstallHelp, func() flags.Commander {
		return &cmdClusterInstall{}
	}, nil, []argDesc{{
		// TRANSLATORS: This should not start with a lowercase letter.
		name: "<snap>",
		desc: i18n.G("Name of the snap to add to the cluster"),
	}})
}

func (x *cmdClusterInstall) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if x.Positional.Snap == "" {
		return fmt.Errorf("snap name is required")
	}

	if err := x.client.ClusterInstall(x.Positional.Snap); err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("Marked %q as clustered in uncommitted cluster state.\n"), x.Positional.Snap)
	return nil
}
