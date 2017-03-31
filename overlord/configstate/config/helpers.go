// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var validKey = regexp.MustCompile("^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$")

func ParseKey(key string) (subkeys []string, err error) {
	subkeys = strings.Split(key, ".")
	for _, subkey := range subkeys {
		if !validKey.MatchString(subkey) {
			return nil, fmt.Errorf("invalid option name: %q", subkey)
		}
	}
	return subkeys, nil
}

func PatchConfig(snapName string, subkeys []string, pos int, config interface{}, value *json.RawMessage) (interface{}, error) {

	switch config := config.(type) {
	case nil:
		// Missing update map. Create and nest final value under it.
		configm := make(map[string]interface{})
		_, err := PatchConfig(snapName, subkeys, pos, configm, value)
		if err != nil {
			return nil, err
		}
		return configm, nil

	case *json.RawMessage:
		// Raw replaces pristine on commit. Unpack, update, and repack.
		var configm map[string]interface{}
		err := json.Unmarshal([]byte(*config), &configm)
		if err != nil {
			return nil, fmt.Errorf("snap %q option %q is not a map", snapName, strings.Join(subkeys[:pos], "."))
		}
		_, err = PatchConfig(snapName, subkeys, pos, configm, value)
		if err != nil {
			return nil, err
		}
		return jsonRaw(configm), nil

	case map[string]interface{}:
		// Update map to apply against pristine on commit.
		if pos+1 == len(subkeys) {
			config[subkeys[pos]] = value
			return config, nil
		} else {
			result, err := PatchConfig(snapName, subkeys, pos+1, config[subkeys[pos]], value)
			if err != nil {
				return nil, err
			}
			config[subkeys[pos]] = result
			return config, nil
		}
	}
	panic(fmt.Errorf("internal error: unexpected configuration type %T", config))
}

// Get unmarshals into result the value of the provided snap's configuration key.
// If the key does not exist, an error of type *NoOptionError is returned.
// The provided key may be formed as a dotted key path through nested maps.
// For example, the "a.b.c" key describes the {a: {b: {c: value}}} map.
func GetFromChange(snapName string, subkeys []string, pos int, config map[string]interface{}, result interface{}) error {
	value, ok := config[subkeys[pos]]
	if !ok {
		return &NoOptionError{SnapName: snapName, Key: strings.Join(subkeys[:pos+1], ".")}
	}

	if pos+1 == len(subkeys) {
		raw, ok := value.(*json.RawMessage)
		if !ok {
			raw = jsonRaw(value)
		}
		err := json.Unmarshal([]byte(*raw), result)
		if err != nil {
			key := strings.Join(subkeys, ".")
			return fmt.Errorf("internal error: cannot unmarshal snap %q option %q into %T: %s, json: %s", snapName, key, result, err, *raw)
		}
		return nil
	}

	configm, ok := value.(map[string]interface{})
	if !ok {
		raw, ok := value.(*json.RawMessage)
		if !ok {
			raw = jsonRaw(value)
		}
		err := json.Unmarshal([]byte(*raw), &configm)
		if err != nil {
			return fmt.Errorf("snap %q option %q is not a map", snapName, strings.Join(subkeys[:pos+1], "."))
		}
	}
	return GetFromChange(snapName, subkeys, pos+1, configm, result)
}

// StoreConfigSnapshotMaybe makes a copy of config -> snapSnape configuration into the versioned config.
// It doesn't do anything if there is no configuration for given snap in the state.
// The caller is responsible for locking the state.
func StoreConfigSnapshotMaybe(st *state.State, snapName string, rev snap.Revision) error {
	var config map[string]interface{}                     // snap => configuration
	var configSnapshots map[string]map[string]interface{} // snap => revision => configuration

	// Get current configuration of the snap from state
	err := st.Get("config", &config)
	if err == state.ErrNoState {
		return nil
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal configuration: %v", err)
	}
	snapcfg, ok := config[snapName]
	if !ok {
		return nil
	}

	err = st.Get("config-snapshots", &configSnapshots)
	if err == state.ErrNoState {
		configSnapshots = make(map[string]map[string]interface{})
	} else if err != nil {
		return err
	}
	cfgs := configSnapshots[snapName]
	if cfgs == nil {
		cfgs = make(map[string]interface{})
	}
	cfgs[rev.String()] = snapcfg
	configSnapshots[snapName] = cfgs
	st.Set("config-snapshots", configSnapshots)
	return nil
}

// RestoreConfigSnapshotMaybe restores a given revision of snap configuration into config -> snapName.
// If no configuration exists for given revision it does nothing (no error).
// The caller is responsible for locking the state.
func RestoreConfigSnapshotMaybe(st *state.State, snapName string, rev snap.Revision) error {
	var config map[string]interface{}                     // snap => configuration
	var configSnapshots map[string]map[string]interface{} // snap => revision => configuration

	err := st.Get("config-snapshots", &configSnapshots)
	if err == state.ErrNoState {
		return nil
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal config-snapshots: %v", err)
	}

	err = st.Get("config", &config)
	if err == state.ErrNoState {
		config = make(map[string]interface{})
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal configuration: %v", err)
	}

	if cfg, ok := configSnapshots[snapName]; ok {
		if revCfg, ok := cfg[rev.String()]; ok {
			config[snapName] = revCfg
			st.Set("config", config)
		}
	}

	return nil
}

// DeleteConfigSnapshotMaybe removes configuration snapshot of given snap/revision.
// If no configuration exists for given revision it does nothing (no error).
// The caller is responsible for locking the state.
func DeleteConfigSnapshotMaybe(st *state.State, snapName string, rev snap.Revision) error {
	var configSnapshots map[string]map[string]interface{} // snap => revision => configuration
	err := st.Get("config-snapshots", &configSnapshots)
	if err == state.ErrNoState {
		return nil
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal config-snapshots: %v", err)
	}

	if revCfgs, ok := configSnapshots[snapName]; ok {
		delete(revCfgs, rev.String())
		if len(revCfgs) == 0 {
			delete(configSnapshots, snapName)
		} else {
			configSnapshots[snapName] = revCfgs
		}
		st.Set("config-snapshots", configSnapshots)
	}
	return nil
}

// DeleteSnapConfig removed configuration of given snap from the state.
func DeleteSnapConfig(st *state.State, snapName string) error {
	var config map[string]map[string]*json.RawMessage // snap => key => value

	err := st.Get("config", &config)
	if err == state.ErrNoState {
		return nil
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal configuration: %v", err)
	}
	if _, ok := config[snapName]; ok {
		delete(config, snapName)
		st.Set("config", config)
	}
	return nil
}
