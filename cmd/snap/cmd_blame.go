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
	"fmt"
	"math/rand"

	"github.com/jessevdk/go-flags"
)

type cmdBlame struct{}

var authors []string

func init() {
	cmd := addCommand("blame",
		"",
		"",
		func() flags.Commander {
			return &cmdBlame{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdBlame) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	if len(authors) == 0 {
		return nil
	}

	fmt.Fprintf(Stdout, "It's all %s's fault.\n", authors[rand.Intn(len(authors))])
	return nil
}
