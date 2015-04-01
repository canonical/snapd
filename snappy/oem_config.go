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

package snappy

import (
	"errors"
	"reflect"

	"gopkg.in/yaml.v2"
)

const oemConfigRemovable = "removable"

var (
	errNoConf         = errors.New("no conf")
	errNoSnapToConfig = errors.New("configuring an invalid snappy package")
)

// sanitize config removes the system config elements which are
// to be consumed by the snappy system and not the snap's configuration
// hook
func sanitizeConfig(conf interface{}) (interface{}, error) {
	v := reflect.ValueOf(conf)

	if kind := v.Kind(); kind != reflect.Slice && kind != reflect.Map && kind != reflect.Array {
		return conf, nil
	}

	if v.IsNil() {
		return nil, errNoConf
	}

	if configMap, ok := conf.(map[interface{}]interface{}); ok {
		delete(configMap, oemConfigRemovable)
		if len(configMap) == 0 {
			return nil, errNoConf
		}

		return interface{}(configMap), nil
	}

	return conf, nil
}

func wrapConfig(pkgName string, conf interface{}) ([]byte, error) {
	configWrap := map[string]map[string]interface{}{
		"config": map[string]interface{}{
			pkgName: conf,
		},
	}

	return yaml.Marshal(configWrap)
}

// OemConfig checks for an oem snap and if found applies the configuration
// set there to the system flagging that it run so it is effectively only
// run once
func OemConfig() error {
	oemSnap, err := InstalledSnapsByType(SnapTypeOem)
	if err != nil {
		return err
	}

	if len(oemSnap) < 1 {
		return errors.New("no oem snap")
	}

	snap, ok := oemSnap[0].(Configuration)
	if !ok {
		return errors.New("no config")
	}

	for pkgName, v := range snap.OemConfig() {
		if v == nil {
			continue
		}

		if conf, err := sanitizeConfig(v); err == errNoConf {
			continue
		} else if err != nil {
			return err
		} else {
			configData, err := wrapConfig(pkgName, conf)
			if err != nil {
				return err
			}

			snap := ActiveSnapByName(pkgName)
			if snap == nil {
				return errNoSnapToConfig
			}

			if _, err := snap.Config(configData); err != nil {
				return err
			}
		}
	}

	return nil
}
