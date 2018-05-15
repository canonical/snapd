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
		"Wait for configuration.",
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

func trueish(vi interface{}) bool {
	switch v := vi.(type) {
	case bool:
		if v == true {
			return true
		}
	case int:
		if v > 0 {
			return true
		}
	case int8:
		if v > 0 {
			return true
		}
	case int16:
		if v > 0 {
			return true
		}
	case int32:
		if v > 0 {
			return true
		}
	case int64:
		if v > 0 {
			return true
		}
	case uint:
		if v > 0 {
			return true
		}
	case uint8:
		if v > 0 {
			return true
		}
	case uint16:
		if v > 0 {
			return true
		}
	case uint32:
		if v > 0 {
			return true
		}
	case uint64:
		if v > 0 {
			return true
		}
	case uintptr:
		if v > 0 {
			return true
		}
	case float32:
		if v > 0 {
			return true
		}
	case float64:
		if v > 0 {
			return true
		}
	case json.Number:
		if i, err := v.Int64(); err == nil && i > 0 {
			return true
		}
		if f, err := v.Float64(); err == nil && f != 0.0 {
			return true
		}
	case string:
		if v != "" {
			return true
		}
	}
	// arrays/slices/maps
	typ := reflect.TypeOf(vi)
	switch typ.Kind() {
	case reflect.Array, reflect.Slice, reflect.Map:
		s := reflect.ValueOf(vi)
		if s.Kind() == reflect.Ptr {
			s = reflect.Indirect(s)
		}
		switch s.Kind() {
		case reflect.Array, reflect.Slice, reflect.Map:
			if s.Len() > 0 {
				return true
			}
		}
	}

	return false
}

func (x *cmdWait) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapName := string(x.Positional.Snap)
	confKey := x.Positional.Key

	cli := Client()
	for {
		conf, err := cli.Conf(snapName, []string{confKey})
		if err != nil && !isNoOption(err) {
			return err
		}
		if trueish(conf[confKey]) {
			break
		}
		time.Sleep(waitConfTimeout)
	}

	return nil
}
