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

var shortGetHelp = i18n.G("Get snap configuration")
var longGetHelp = i18n.G(`
The get command prints the configuration for the given snap.`)

type cmdGet struct {
	Positional struct {
		Snap string
		Keys []string
	} `positional-args:"yes" required:"yes"`

	Document bool `short:"d"`
}

func init() {
	addCommand("get", shortGetHelp, longGetHelp, func() flags.Commander { return &cmdGet{} },
		map[string]string{
			"d": i18n.G("always return document, even with single key"),
		}, [][2]string{
			{i18n.G("<snap-name>"), i18n.G("the snap whose conf is being requested")},
			{i18n.G("keys"), i18n.G("key of interest within the configuration")},
		})
}

func (x *cmdGet) Execute(args []string) error {
	if len(args) > 0 {
		// TRANSLATORS: the %s is the list of extra arguments
		return fmt.Errorf(i18n.G("too many arguments: %s"), strings.Join(args, " "))
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

	var bytes []byte
	if confToPrint != nil {
		bytes, err = json.MarshalIndent(confToPrint, "", "\t")
		if err != nil {
			return err
		}
	}

	fmt.Fprintln(Stdout, string(bytes))
	return nil
}
