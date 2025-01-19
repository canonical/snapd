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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

var validKey = regexp.MustCompile("^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$")

// ValidateKey checks if the provided key matches the validKey regex pattern.
// It returns an error if the key is invalid, otherwise nil.
func ValidateKey(key string) error {
	if !validKey.MatchString(key) {
		return fmt.Errorf("invalid option name: %q", key)
	}
	return nil
}

func ParseKey(key string) (subkeys []string, err error) {
	if key == "" {
		return []string{}, nil
	}
	subkeys = strings.Split(key, ".")
	for _, subkey := range subkeys {
		if err = ValidateKey(subkey); err != nil {
			return nil, err
		}
	}
	return subkeys, nil
}

func purgeNulls(config interface{}) interface{} {
	switch config := config.(type) {
	// map of json raw messages is the starting point for purgeNulls, this is the configuration we receive
	case map[string]*json.RawMessage:
		for k, v := range config {
			if cfg := purgeNulls(v); cfg != nil {
				config[k] = cfg.(*json.RawMessage)
			} else {
				delete(config, k)
			}
		}
	case map[string]interface{}:
		for k, v := range config {
			if cfg := purgeNulls(v); cfg != nil {
				config[k] = cfg
			} else {
				delete(config, k)
			}
		}
	case *json.RawMessage:
		if config == nil {
			return nil
		}
		var configm interface{}
		if err := jsonutil.DecodeWithNumber(bytes.NewReader(*config), &configm); err != nil {
			panic(fmt.Errorf("internal error: cannot unmarshal configuration: %v", err))
		}
		if cfg := purgeNulls(configm); cfg != nil {
			return jsonRaw(cfg)
		}
		return nil
	}
	return config
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

		if err := jsonutil.DecodeWithNumber(bytes.NewReader(*config), &configm); err != nil {
			return nil, fmt.Errorf("snap %q option %q is not a map", snapName, strings.Join(subkeys[:pos], "."))
		}
		// preserve the invariant that if PatchConfig is
		// passed a map[string]interface{} it is not nil.
		// If the value was set to null in the same
		// transaction use (interface{})(nil) which is handled
		// by the first case here.
		// (see LP: #1920773)
		var cfg interface{}
		if configm != nil {
			cfg = configm
		}
		result, err := PatchConfig(snapName, subkeys, pos, cfg, value)
		if err != nil {
			return nil, err
		}

		// PatchConfig may have recreated higher level element that was previously set to null in same
		// transaction; returning the result for PatchConfig rather than just configm ensures we do
		// support cases where a previously unset path is set back.
		return jsonRaw(result), nil

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
	return nil, fmt.Errorf("internal error: unexpected configuration type %T", config)
}

// GetSnapConfig retrieves the raw configuration of a given snap.
func GetSnapConfig(st *state.State, snapName string) (*json.RawMessage, error) {
	var config map[string]*json.RawMessage
	err := st.Get("config", &config)
	if errors.Is(err, state.ErrNoState) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapcfg, ok := config[snapName]
	if !ok {
		return nil, nil
	}
	return snapcfg, nil
}

// SetSnapConfig replaces the configuration of a given snap.
func SetSnapConfig(st *state.State, snapName string, snapcfg *json.RawMessage) error {
	var config map[string]*json.RawMessage
	err := st.Get("config", &config)
	// empty nil snapcfg should be an empty message, but deal with "null" as well.
	isNil := snapcfg == nil || len(*snapcfg) == 0 || bytes.Compare(*snapcfg, []byte("null")) == 0
	if errors.Is(err, state.ErrNoState) {
		if isNil {
			// bail out early
			return nil
		}
		config = make(map[string]*json.RawMessage, 1)
	} else if err != nil {
		return err
	}
	if isNil {
		delete(config, snapName)
	} else {
		config[snapName] = snapcfg
	}
	st.Set("config", config)
	return nil
}

// SaveRevisionConfig makes a copy of config -> snapSnape configuration into the versioned config.
// It doesn't do anything if there is no configuration for given snap in the state.
// The caller is responsible for locking the state.
func SaveRevisionConfig(st *state.State, snapName string, rev snap.Revision) error {
	var config map[string]*json.RawMessage                    // snap => configuration
	var revisionConfig map[string]map[string]*json.RawMessage // snap => revision => configuration

	// Get current configuration of the snap from state
	err := st.Get("config", &config)
	if errors.Is(err, state.ErrNoState) {
		return nil
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal configuration: %v", err)
	}
	snapcfg, ok := config[snapName]
	if !ok {
		return nil
	}

	err = st.Get("revision-config", &revisionConfig)
	if errors.Is(err, state.ErrNoState) {
		revisionConfig = make(map[string]map[string]*json.RawMessage)
	} else if err != nil {
		return err
	}
	cfgs := revisionConfig[snapName]
	if cfgs == nil {
		cfgs = make(map[string]*json.RawMessage)
	}
	cfgs[rev.String()] = snapcfg
	revisionConfig[snapName] = cfgs
	st.Set("revision-config", revisionConfig)
	return nil
}

// RestoreRevisionConfig restores a given revision of snap configuration into config -> snapName.
// If no configuration exists for given revision it does nothing (no error).
// The caller is responsible for locking the state.
func RestoreRevisionConfig(st *state.State, snapName string, rev snap.Revision) error {
	var config map[string]*json.RawMessage                    // snap => configuration
	var revisionConfig map[string]map[string]*json.RawMessage // snap => revision => configuration

	err := st.Get("revision-config", &revisionConfig)
	if errors.Is(err, state.ErrNoState) {
		return nil
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal revision-config: %v", err)
	}

	err = st.Get("config", &config)
	if errors.Is(err, state.ErrNoState) {
		config = make(map[string]*json.RawMessage)
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal configuration: %v", err)
	}

	if cfg, ok := revisionConfig[snapName]; ok {
		if revCfg, ok := cfg[rev.String()]; ok {
			config[snapName] = revCfg
			st.Set("config", config)
		}
	}

	return nil
}

// DiscardRevisionConfig removes configuration snapshot of given snap/revision.
// If no configuration exists for given revision it does nothing (no error).
// The caller is responsible for locking the state.
func DiscardRevisionConfig(st *state.State, snapName string, rev snap.Revision) error {
	var revisionConfig map[string]map[string]*json.RawMessage // snap => revision => configuration
	err := st.Get("revision-config", &revisionConfig)
	if errors.Is(err, state.ErrNoState) {
		return nil
	} else if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal revision-config: %v", err)
	}

	if revCfgs, ok := revisionConfig[snapName]; ok {
		delete(revCfgs, rev.String())
		if len(revCfgs) == 0 {
			delete(revisionConfig, snapName)
		} else {
			revisionConfig[snapName] = revCfgs
		}
		st.Set("revision-config", revisionConfig)
	}
	return nil
}

// DeleteSnapConfig removed configuration of given snap from the state.
func DeleteSnapConfig(st *state.State, snapName string) error {
	var config map[string]map[string]*json.RawMessage // snap => key => value

	err := st.Get("config", &config)
	if errors.Is(err, state.ErrNoState) {
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

// ConfSetter is an interface for setting of config values.
type ConfSetter interface {
	Set(snapName, key string, value interface{}) error
}

// Patch sets values in cfg for the provided snap's configuration
// based on patch.
// patch keys can be dotted as the key argument to Set.
// The patch is applied according to the order of its keys sorted by depth,
// with top keys sorted first.
func Patch(cfg ConfSetter, snapName string, patch map[string]interface{}) error {
	patchKeys := sortPatchKeysByDepth(patch)
	for _, key := range patchKeys {
		if err := cfg.Set(snapName, key, patch[key]); err != nil {
			return err
		}
	}
	return nil
}

func sortPatchKeysByDepth(patch map[string]interface{}) []string {
	if len(patch) == 0 {
		return nil
	}
	depths := make(map[string]int, len(patch))
	keys := make([]string, 0, len(patch))
	for k := range patch {
		depths[k] = strings.Count(k, ".")
		keys = append(keys, k)
	}

	sort.Slice(keys, func(i, j int) bool {
		return depths[keys[i]] < depths[keys[j]]
	})
	return keys
}
