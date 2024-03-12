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
	"sort"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/i18n"
)

var shortGetHelp = i18n.G("Print configuration options")
var longGetHelp = i18n.G(`
The get command prints configuration options for the provided snap.

    $ snap get snap-name username
    frank

If multiple option names are provided, the corresponding values are returned:

    $ snap get snap-name username password
    Key       Value
    username  frank
    password  ...

Nested values may be retrieved via a dotted path:

    $ snap get snap-name author.name
    frank
`)

var longAspectGetHelp = i18n.G(`
If the first argument passed into get is an aspect identifier matching the
format <account-id>/<bundle>/<aspect>, get will use the aspects configuration
API. In this case, the command returns the data retrieved from the requested
dot-separated aspect paths.
`)

type cmdGet struct {
	clientMixin
	Positional struct {
		Snap installedSnapName `required:"yes"`
		Keys []string
	} `positional-args:"yes"`

	Typed    bool `short:"t"`
	Document bool `short:"d"`
	List     bool `short:"l"`
}

func init() {
	if err := validateAspectFeatureFlag(); err == nil {
		longGetHelp += longAspectGetHelp
	}

	addCommand("get", shortGetHelp, longGetHelp, func() flags.Commander { return &cmdGet{} },
		map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"d": i18n.G("Always return document, even with single key"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"l": i18n.G("Always return list, even with single key"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"t": i18n.G("Strict typing with nulls and quoted strings"),
		}, []argDesc{
			{
				name: "<snap>",
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("The snap whose conf is being requested"),
			},
			{
				// TRANSLATORS: This needs to begin with < and end with >
				name: i18n.G("<key>"),
				// TRANSLATORS: This should not start with a lowercase letter.
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

func rootRequested(confKeys []string) bool {
	return len(confKeys) == 0
}

// outputJson will be used when the user requested "document" output via
// the "-d" commandline switch.
func (c *cmdGet) outputJson(conf interface{}) error {
	bytes, err := json.MarshalIndent(conf, "", "\t")
	if err != nil {
		return err
	}

	fmt.Fprintln(Stdout, string(bytes))
	return nil
}

// outputList will be used when the user requested list output via the
// "-l" commandline switch.
func (x *cmdGet) outputList(conf map[string]interface{}) error {
	if rootRequested(x.Positional.Keys) && len(conf) == 0 {
		return fmt.Errorf("snap %q has no configuration", x.Positional.Snap)
	}

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintf(w, "Key\tValue\n")
	values := flattenConfig(conf, rootRequested(x.Positional.Keys))
	for _, v := range values {
		fmt.Fprintf(w, "%s\t%v\n", v.Path, v.Value)
	}
	return nil
}

// outputDefault will be used when no commandline switch to override the
// output where used. The output follows the following rules:
//   - a single key with a string value is printed directly
//   - multiple keys are printed as a list to the terminal (if there is one)
//     or as json if there is no terminal
//   - the option "typed" is honored
func (x *cmdGet) outputDefault(conf map[string]interface{}, snapName string, confKeys []string) error {
	if rootRequested(confKeys) && len(conf) == 0 {
		return fmt.Errorf("snap %q has no configuration", snapName)
	}

	var confToPrint interface{} = conf

	if len(confKeys) == 1 {
		// if single key was requested, then just output the
		// value unless it's a map, in which case it will be
		// printed as a list below.
		if _, ok := conf[confKeys[0]].(map[string]interface{}); !ok {
			confToPrint = conf[confKeys[0]]
		}
	}

	// conf looks like a map
	if cfg, ok := confToPrint.(map[string]interface{}); ok {
		if isStdinTTY {
			return x.outputList(cfg)
		}

		// TODO: remove this conditional and the warning below
		// after a transition period.
		fmt.Fprintf(Stderr, i18n.G(`WARNING: The output of 'snap get' will become a list with columns - use -d or -l to force the output format.\n`))
		return x.outputJson(confToPrint)
	}

	if s, ok := confToPrint.(string); ok && !x.Typed {
		fmt.Fprintln(Stdout, s)
		return nil
	}

	if confToPrint != nil || x.Typed {
		return x.outputJson(confToPrint)
	}

	fmt.Fprintln(Stdout, "")
	return nil

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

	var conf map[string]interface{}
	var err error
	if isAspectID(snapName) {
		if err := validateAspectFeatureFlag(); err != nil {
			return err
		}

		// first argument is an aspectID, use the aspects API
		aspectID := snapName
		if err := validateAspectID(aspectID); err != nil {
			return err
		}

		conf, err = x.client.AspectGet(aspectID, confKeys)
	} else {
		conf, err = x.client.Conf(snapName, confKeys)
	}

	if err != nil {
		return err
	}

	switch {
	case x.Document:
		return x.outputJson(conf)
	case x.List:
		return x.outputList(conf)
	default:
		return x.outputDefault(conf, snapName, confKeys)
	}
}

func validateAspectFeatureFlag() error {
	if !features.AspectsConfiguration.IsEnabled() {
		_, confName := features.AspectsConfiguration.ConfigOption()
		return fmt.Errorf(`aspect-based configuration is disabled: you must set '%s' to true`, confName)
	}
	return nil
}
