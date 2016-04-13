// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snappy

import (
	"fmt"
	"reflect"
	"regexp"
)

func verifyAppYaml(app *AppYaml) error {
	contains := func(needle string, haystack []string) bool {
		for _, h := range haystack {
			if needle == h {
				return true
			}
		}
		return false
	}
	valid := []string{"", "simple", "forking", "oneshot", "dbus"}
	if !contains(app.Daemon, valid) {
		return fmt.Errorf(`"daemon" field contains invalid value %q`, app.Daemon)
	}

	return verifyStructStringsAgainstWhitelist(*app, servicesBinariesStringsWhitelist)
}

func verifyPlugYaml(plug *plugYaml) error {
	if err := verifyStructStringsAgainstWhitelist(*plug, servicesBinariesStringsWhitelist); err != nil {
		return err
	}

	return nil
}

// FIXME: too much magic, just do explicit validation of the few
//        fields we have
// verifyStructStringsAgainstWhitelist takes a struct and ensures that
// the given whitelist regexp matches all string fields of the struct
func verifyStructStringsAgainstWhitelist(s interface{}, whitelist *regexp.Regexp) error {
	// check all members of the services struct against our whitelist
	t := reflect.TypeOf(s)
	v := reflect.ValueOf(s)
	for i := 0; i < t.NumField(); i++ {

		// PkgPath means its a unexported field and we can ignore it
		if t.Field(i).PkgPath != "" {
			continue
		}
		if v.Field(i).Kind() == reflect.Ptr {
			vi := v.Field(i).Elem()
			if vi.Kind() == reflect.Struct {
				return verifyStructStringsAgainstWhitelist(vi.Interface(), whitelist)
			}
		}
		if v.Field(i).Kind() == reflect.Struct {
			vi := v.Field(i).Interface()
			return verifyStructStringsAgainstWhitelist(vi, whitelist)
		}
		if v.Field(i).Kind() == reflect.String {
			key := t.Field(i).Name
			value := v.Field(i).String()
			if !whitelist.MatchString(value) {
				return &ErrStructIllegalContent{
					Field:     key,
					Content:   value,
					Whitelist: whitelist.String(),
				}
			}
		}
	}

	return nil
}
