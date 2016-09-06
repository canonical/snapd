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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

type cmdGet struct {
	Positional struct {
		Snap string   `positional-arg-name:"<snap name>" description:"the snap whose conf is being requested"`
		Keys []string `positional-arg-name:"<keys>" description:"key of interest within the confuration"`
	} `positional-args:"yes" required:"yes"`

	Document bool `short:"d" description:"always return document, even with single key"`
}

func init() {
	addCommand("get",
		i18n.G("Get snap configuration"),
		i18n.G("Get confuration for the given snap."),
		func() flags.Commander {
			return &cmdGet{}
		})
}

func (x *cmdGet) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("too many arguments: %s", strings.Join(args, " "))
	}

	return getConf(x.Positional.Snap, x.Positional.Keys, x.Document)
}

func getConf(snapName string, confKeys []string, fullDocument bool) error {
	cli := Client()
	conf, err := cli.Conf(snapName, confKeys)
	if err != nil {
		return err
	}

	var confToPrint interface{} = conf
	if !fullDocument && len(confKeys) == 1 {
		confToPrint = conf[confKeys[0]]
	}

	bytes, err := json.MarshalIndent(confToPrint, "", "\t")
	if err != nil {
		return err
	}

	fmt.Fprintln(Stdout, string(bytes))
	return nil
}
