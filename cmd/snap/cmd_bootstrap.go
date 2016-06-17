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

package main

import (
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/bootstrap"
	"github.com/snapcore/snapd/i18n"
)

type cmdBootstrap struct {
	Positional struct {
		BootstrapYamlFile string `positional-arg-name:"bootstrap-yaml" description:"The bootstrap yaml"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	cmd := addCommand("bootstrap",
		i18n.G("Bootstrap a snappy system"),
		i18n.G("Bootstrap a snappy system"),
		func() flags.Commander {
			return &cmdBootstrap{}
		})
	cmd.hidden = true
}

func (x *cmdBootstrap) Execute(args []string) error {
	return bootstrap.Bootstrap(x.Positional.BootstrapYamlFile)
}
