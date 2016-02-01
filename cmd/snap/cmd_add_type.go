// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type cmdAddType struct {
	Positionals struct {
		Name string `positional-arg-name:"name" description:"name of the skill type"`
	} `positional-args:"true" required:"true"`
}

var (
	shortAddTypeHelp = i18n.G("Add a skill type to the system")
	longAddTypeHelp  = i18n.G(`This command adds a new skill type to the system.

The added type does not grant any additional permissions to snaps providing or
consuming skills using it.`)
)

func init() {
	var err error
	if develCommand == nil {
		err = fmt.Errorf("devel command not found")
	} else {
		_, err = develCommand.AddCommand("add-type", shortAddTypeHelp, longAddTypeHelp, &cmdAddType{})
	}
	if err != nil {
		logger.Panicf("unable to add add-type command: %v", err)
	}
}

func (x *cmdAddType) Execute(args []string) error {
	return client.New().AddType(x.Positionals.Name)
}
