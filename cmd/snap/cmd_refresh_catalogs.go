// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
)

var shortRefreshCatalogsHelp = i18n.G("Refresh the catalog from the store")
var longRefreshCatalogsHelp = i18n.G(`
The refresh-catalogs command fetches section names and other data from the store
ands stores it locally on the system.
`)

type cmdRefreshCatalogs struct {
	clientMixin
}

func init() {
	addDebugCommand("refresh-catalogs", shortRefreshCatalogsHelp, longRefreshCatalogsHelp, func() flags.Commander {
		return &cmdRefreshCatalogs{}
	}, nil, nil)
}

func (cmd cmdRefreshCatalogs) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	return cmd.client.Debug("refresh-catalogs", nil, nil)
}
