// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type cmdAddCap struct {
	Name  string            `short:"n" long:"name" required:"true" description:"unique capability name"`
	Label string            `short:"l" long:"label" required:"true" description:"human-friendly label"`
	Type  string            `short:"t" long:"type" required:"true" description:"type of the capability to add"`
	Attrs map[string]string `short:"a" long:"attr" description:"additional key:value attributes"`
}

var (
	shortAddCapHelp = i18n.G("Add a capability to the system")
	longAddCapHelp  = i18n.G("This command adds a capability to the system")
)

func init() {
	_, err := parser.AddCommand("add-cap", shortAddCapHelp, longAddCapHelp, &cmdAddCap{})
	if err != nil {
		logger.Panicf("unable to add command %q: %v", "add-cap", err)
	}
}

func (x *cmdAddCap) Execute(args []string) error {
	cap := &client.Capability{
		Name:  x.Name,
		Label: x.Label,
		Type:  x.Type,
		Attrs: x.Attrs,
	}
	return client.New().AddCapability(cap)
}
