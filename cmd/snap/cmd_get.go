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

var shortGetHelp = i18n.G("Prints configuration options")
var longGetHelp = i18n.G(`
The get command prints configuration options for the provided snap.

    $ snap get snap-name username
    frank

If multiple option names are provided, a document is returned:

    $ snap get snap-name username password
    {
        "username": "frank",
        "password": "..."
    }

Nested values may be retrieved via a dotted path:

    $ snap get snap-name author.name
    frank
`)

type cmdGet struct {
	Positional struct {
		Snap installedSnapName
		Keys []string
	} `positional-args:"yes" required:"yes"`

	Typed    bool `short:"t"`
	Document bool `short:"d"`
}

func init() {
	addCommand("get", shortGetHelp, longGetHelp, func() flags.Commander { return &cmdGet{} },
		map[string]string{
			"d": i18n.G("Always return document, even with single key"),
			"t": i18n.G("Strict typing with nulls and quoted strings"),
		}, []argDesc{
			{
				name: "<snap>",
				desc: i18n.G("The snap whose conf is being requested"),
			},
			{
				name: i18n.G("<key>"),
				desc: i18n.G("Key of interest within the configuration"),
			},
		})
}

func (x *cmdGet) Execute(args []string) error {
	if len(args) > 0 {
		// TRANSLATORS: the %s is the list of extra arguments
		return fmt.Errorf(i18n.G("too many arguments: %s"), strings.Join(args, " "))
	}

	if x.Document && x.Typed {
		return fmt.Errorf("cannot use -d and -t together")
	}

	snapName := string(x.Positional.Snap)
	confKeys := x.Positional.Keys

	cli := Client()
	conf, err := cli.Conf(snapName, confKeys)
	if err != nil {
		return err
	}

	var confToPrint interface{} = conf
	if !x.Document && len(confKeys) == 1 {
		confToPrint = conf[confKeys[0]]
	}

	if x.Typed && confToPrint == nil {
		fmt.Fprintln(Stdout, "null")
		return nil
	}

	if s, ok := confToPrint.(string); ok && !x.Typed {
		fmt.Fprintln(Stdout, s)
		return nil
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
