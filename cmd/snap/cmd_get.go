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
	"os"
	"sort"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"golang.org/x/crypto/ssh/terminal"
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
		Snap installedSnapName `required:"yes"`
		Keys []string
	} `positional-args:"yes"`

	Typed    bool `short:"t"`
	Document bool `short:"d"`
	List     bool `short:"l"`
}

func init() {
	addCommand("get", shortGetHelp, longGetHelp, func() flags.Commander { return &cmdGet{} },
		map[string]string{
			"d": i18n.G("Always return document, even with single key"),
			"l": i18n.G("Always return list, even with single key"),
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

type ConfigValue struct {
	Path  string
	Value interface{}
}

type byConfigPath []ConfigValue

func (s byConfigPath) Len() int      { return len(s) }
func (s byConfigPath) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byConfigPath) Less(i, j int) bool {
	other := s[j].Path
	for k, c := range s[i].Path {
		if len(other) <= k {
			return false
		}

		switch {
		case c == rune(other[k]):
			continue
		case c == '.':
			return true
		case other[k] == '.' || c > rune(other[k]):
			return false
		}
		return true
	}
	return true
}

func sortByPath(config []ConfigValue) {
	sort.Sort(byConfigPath(config))
}

func flattenConfig(cfg map[string]interface{}, root bool) (values []ConfigValue) {
	const docstr = "{...}"
	for k, v := range cfg {
		if input, ok := v.(map[string]interface{}); ok {
			if root {
				values = append(values, ConfigValue{k, docstr})
			} else {
				for kk, vv := range input {
					p := k + "." + kk
					if _, ok := vv.(map[string]interface{}); ok {
						values = append(values, ConfigValue{p, docstr})
					} else {
						values = append(values, ConfigValue{p, vv})
					}
				}
			}
		} else {
			values = append(values, ConfigValue{k, v})
		}
	}
	sortByPath(values)
	return values
}

func (x *cmdGet) Execute(args []string) error {
	if len(args) > 0 {
		// TRANSLATORS: the %s is the list of extra arguments
		return fmt.Errorf(i18n.G("too many arguments: %s"), strings.Join(args, " "))
	}

	if x.Document && x.Typed {
		return fmt.Errorf("cannot use -d and -t together")
	}

	if x.Document && x.List {
		return fmt.Errorf("cannot use -d and -l together")
	}

	snapName := string(x.Positional.Snap)
	confKeys := x.Positional.Keys

	cli := Client()
	conf, err := cli.Conf(snapName, confKeys)
	if err != nil {
		return err
	}

	isTerminal := terminal.IsTerminal(int(os.Stdin.Fd()))

	var confToPrint interface{} = conf
	if !x.Document && !x.List && len(confKeys) == 1 {
		// if single key was requested, then just output the value unless it's a map,
		// in which case it will be printed as a list below.
		if _, ok := conf[confKeys[0]].(map[string]interface{}); !ok {
			confToPrint = conf[confKeys[0]]
		}
	}

	if cfg, ok := confToPrint.(map[string]interface{}); ok && !x.Document {
		// TODO: remove this conditional and the warning below after a transition period.
		if isTerminal || x.List {
			w := tabWriter()
			defer w.Flush()

			rootRequested := len(confKeys) == 0
			fmt.Fprintf(w, "Key\tValue\n")
			values := flattenConfig(cfg, rootRequested)
			for _, v := range values {
				fmt.Fprintf(w, "%s\t%v\n", v.Path, v.Value)
			}
			return nil
		} else {
			fmt.Fprintf(Stderr, i18n.G(`WARNING: The output of "snap get" will become a list with columns - use -d or -l to force the output format.\n`))
		}
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
