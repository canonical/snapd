// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !linux

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
	"fmt"
	"net/http"
	"os"
	"runtime"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type otherDoer struct{}

func (otherDoer) Do(*http.Request) (*http.Response, error) {
	fmt.Fprintf(Stderr, i18n.G(`Interacting with snapd is not yet supported on %s.
This command has been left available for documentation purposes only.
`), runtime.GOOS)
	os.Exit(1)
	panic("execution continued past call to exit")
}

// Client returns a new client using ClientConfig as configuration.
func Client() *client.Client {
	cli := client.New(&ClientConfig)
	cli.SetDoer(otherDoer{})
	return cli
}
