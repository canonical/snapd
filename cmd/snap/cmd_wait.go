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
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdWait struct {
	Positional struct {
		Snap installedSnapName `required:"yes"`
		Key  string
	} `positional-args:"yes"`
}

func init() {
	addCommand("wait",
		"Wait for configuration",
		"The wait command waits until a configration becomes true.",
		func() flags.Commander {
			return &cmdWait{}
		}, nil, []argDesc{
			{
				name: "<snap>",
				// TRANSLATORS: This should probably not start with a lowercase letter.
				desc: i18n.G("The snap for which configuration will be checked"),
			}, {
				// TRANSLATORS: This needs to be wrapped in <>s.
				name: i18n.G("<key>"),
				// TRANSLATORS: This should probably not start with a lowercase letter.
				desc: i18n.G("Key of interest within the configuration"),
			},
		})
}

var waitConfTimeout = 500 * time.Millisecond

func isNoOption(err error) bool {
	if e, ok := err.(*client.Error); ok && e.Kind == client.ErrorKindConfigNoSuchOption {
		return true
	}
	return false
}

// trueishJSON takes an interface{} and returns true if the interface value
// looks "true". For strings thats if len(string) > 0 for numbers that
// they are != 0 and for maps/slices/arrays that they have elements.
//
// Note that *only* types that the json package decode with the
// "UseNumber()" options turned on are handled here. If this ever
// needs to becomes a generic "trueish" helper we need to resurrect
// the code in 306ba60edfba8d6501060c6f773235d8c994a319 (and add nil
// to it).
func trueishJSON(vi interface{}) (bool, error) {
	switch v := vi.(type) {
	// limited to the types that json unmarhal can produce
	case nil:
		return false, nil
	case bool:
		return v, nil
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return i != 0, nil
		}
		if f, err := v.Float64(); err == nil {
			return f != 0.0, nil
		}
	case string:
		return v != "", nil
	}
	// arrays/slices/maps
	typ := reflect.TypeOf(vi)
	switch typ.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		s := reflect.ValueOf(vi)
		switch s.Kind() {
		case reflect.Array, reflect.Slice, reflect.Map:
			return s.Len() > 0, nil
		}
	}

	return false, fmt.Errorf("cannot test type %T for truth", vi)
}

func (x *cmdWait) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapName := string(x.Positional.Snap)
	confKey := x.Positional.Key

	// This is fine because not providing a confKey is unsupported so this
	// won't interfere with supported uses of `snap wait`.
	if snapName == "godot" && confKey == "" {
		switch rand.Intn(10) {
		case 0:
			fmt.Fprintln(Stdout, `The tears of the world are a constant quantity.
For each one who begins to weep somewhere else another stops.
The same is true of the laugh.`)
		case 1:
			fmt.Fprintln(Stdout, "Nothing happens. Nobody comes, nobody goes. It's awful.")
		default:
			fmt.Fprintln(Stdout, `"Let's go." "We can't." "Why not?" "We're waiting for Godot."`)
		}
		return nil
	}
	if confKey == "" {
		return fmt.Errorf("the required argument `<key>` was not provided")
	}

	cli := Client()
	for {
		conf, err := cli.Conf(snapName, []string{confKey})
		if err != nil && !isNoOption(err) {
			return err
		}
		res, err := trueishJSON(conf[confKey])
		if err != nil {
			return err
		}
		if res {
			break
		}
		time.Sleep(waitConfTimeout)
	}

	return nil
}
