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

package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/snapcore/snapd/overlord/state"
)

// Transaction holds a copy of the configuration originally present in the
// provided state which can be queried and mutated in isolation from
// concurrent logic. All changes performed into it are persisted back into
// the state at once when Commit is called.
//
// Transactions are safe to access and modify concurrently.
type Transaction struct {
	mu       sync.Mutex
	state    *state.State
	pristine map[string]map[string]*json.RawMessage // snap => key => value
	changes  map[string]map[string]interface{}
}

// NewTransaction creates a new configuration transaction initialized with the given state.
//
// The provided state must be locked by the caller.
func NewTransaction(st *state.State) *Transaction {
	transaction := &Transaction{state: st}
	transaction.changes = make(map[string]map[string]interface{})

	// Record the current state of the map containing the config of every snap
	// in the system. We'll use it for this transaction.
	err := st.Get("config", &transaction.pristine)
	if err == state.ErrNoState {
		transaction.pristine = make(map[string]map[string]*json.RawMessage)
	} else if err != nil {
		panic(fmt.Errorf("internal error: cannot unmarshal configuration: %v", err))
	}
	return transaction
}

var validKey = regexp.MustCompile("^(?:[a-z0-9]+-?)*[a-z](?:-?[a-z0-9])*$")

func parseKey(key string) (subkeys []string, err error) {
	subkeys = strings.Split(key, ".")
	for _, subkey := range subkeys {
		if !validKey.MatchString(subkey) {
			return nil, fmt.Errorf("invalid option name: %q", subkey)
		}
	}
	return subkeys, nil
}

// Set sets the provided snap's configuration key to the given value.
// The provided key may be formed as a dotted key path through nested maps.
// For example, the "a.b.c" key describes the {a: {b: {c: value}}} map.
// When the key is provided in that form, intermediate maps are mutated
// rather than replaced, and created when necessary.
//
// The provided value must marshal properly by encoding/json.
// Changes are not persisted until Commit is called.
func (t *Transaction) Set(snapName, key string, value interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	config, ok := t.changes[snapName]
	if !ok {
		config = make(map[string]interface{})
	}

	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot marshal snap %q option %q: %s", snapName, key, err)
	}
	raw := json.RawMessage(data)

	subkeys, err := parseKey(key)
	if err != nil {
		return err
	}

	// Check whether it's trying to traverse a non-map from pristine. This
	// would go unperceived by the configuration patching below.
	if len(subkeys) > 1 {
		var result interface{}
		err = getFromPristine(snapName, subkeys, 0, t.pristine[snapName], &result)
		if err != nil && !IsNoOption(err) {
			return err
		}
	}
	_, err = patchConfig(snapName, subkeys, 0, config, &raw)
	if err != nil {
		return err
	}

	t.changes[snapName] = config
	return nil
}

// Get unmarshals into result the cached value of the provided snap's configuration key.
// If the key does not exist, an error of type *NoOptionError is returned.
// The provided key may be formed as a dotted key path through nested maps.
// For example, the "a.b.c" key describes the {a: {b: {c: value}}} map.
//
// Transactions do not see updates from the current state or from other transactions.
func (t *Transaction) Get(snapName, key string, result interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	subkeys, err := parseKey(key)
	if err != nil {
		return err
	}

	err = getFromChange(snapName, subkeys, 0, t.changes[snapName], result)
	if IsNoOption(err) {
		err = getFromPristine(snapName, subkeys, 0, t.pristine[snapName], result)
	}
	return err
}

// GetMaybe unmarshals into result the cached value of the provided snap's configuration key.
// If the key does not exist, no error is returned.
//
// Transactions do not see updates from the current state or from other transactions.
func (t *Transaction) GetMaybe(snapName, key string, result interface{}) error {
	err := t.Get(snapName, key, result)
	if err != nil && !IsNoOption(err) {
		return err
	}
	return nil
}

func getFromPristine(snapName string, subkeys []string, pos int, config map[string]*json.RawMessage, result interface{}) error {
	raw, ok := config[subkeys[pos]]
	if !ok {
		return &NoOptionError{SnapName: snapName, Key: strings.Join(subkeys[:pos+1], ".")}
	}

	if pos+1 == len(subkeys) {
		err := json.Unmarshal([]byte(*raw), result)
		if err != nil {
			key := strings.Join(subkeys, ".")
			return fmt.Errorf("internal error: cannot unmarshal snap %q option %q into %T: %s, json: %s", snapName, key, result, err, *raw)
		}
		return nil
	}

	var configm map[string]*json.RawMessage
	err := json.Unmarshal([]byte(*raw), &configm)
	if err != nil {
		return fmt.Errorf("snap %q option %q is not a map", snapName, strings.Join(subkeys[:pos+1], "."))
	}
	return getFromPristine(snapName, subkeys, pos+1, configm, result)
}

func getFromChange(snapName string, subkeys []string, pos int, config map[string]interface{}, result interface{}) error {
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
	return getFromChange(snapName, subkeys, pos+1, configm, result)
}

func patchConfig(snapName string, subkeys []string, pos int, config interface{}, value *json.RawMessage) (interface{}, error) {

	switch config := config.(type) {
	case nil:
		// Missing update map. Create and nest final value under it.
		configm := make(map[string]interface{})
		_, err := patchConfig(snapName, subkeys, pos, configm, value)
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
		_, err = patchConfig(snapName, subkeys, pos, configm, value)
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
			result, err := patchConfig(snapName, subkeys, pos+1, config[subkeys[pos]], value)
			if err != nil {
				return nil, err
			}
			config[subkeys[pos]] = result
			return config, nil
		}
	}
	panic(fmt.Errorf("internal error: unexpected configuration type %T", config))
}

// Commit applies to the state the configuration changes made in the transaction
// and updates the observed configuration to the result of the operation.
//
// The state associated with the transaction must be locked by the caller.
func (t *Transaction) Commit() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.changes) == 0 {
		return
	}

	// Update our copy of the config with the most recent one from the state.
	err := t.state.Get("config", &t.pristine)
	if err == state.ErrNoState {
		t.pristine = make(map[string]map[string]*json.RawMessage)
	} else if err != nil {
		panic(fmt.Errorf("internal error: cannot unmarshal configuration: %v", err))
	}

	// Iterate through the write cache and save each item.
	for snapName, snapChanges := range t.changes {
		config, ok := t.pristine[snapName]
		if !ok {
			config = make(map[string]*json.RawMessage)
		}
		for k, v := range snapChanges {
			config[k] = commitChange(config[k], v)
		}
		t.pristine[snapName] = config
	}

	t.state.Set("config", t.pristine)

	// The cache has been flushed, reset it.
	t.changes = make(map[string]map[string]interface{})
}

func jsonRaw(v interface{}) *json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("internal error: cannot marshal configuration: %v", err))
	}
	raw := json.RawMessage(data)
	return &raw
}

func commitChange(pristine *json.RawMessage, change interface{}) *json.RawMessage {
	switch change := change.(type) {
	case *json.RawMessage:
		return change
	case map[string]interface{}:
		if pristine == nil {
			return jsonRaw(change)
		}
		var pristinem map[string]*json.RawMessage
		if err := json.Unmarshal([]byte(*pristine), &pristinem); err != nil {
			// Not a map. Overwrite with the change.
			return jsonRaw(change)
		}
		for k, v := range change {
			pristinem[k] = commitChange(pristinem[k], v)
		}
		return jsonRaw(pristinem)
	}
	panic(fmt.Errorf("internal error: unexpected configuration type %T", change))
}

// IsNoOption returns whether the provided error is a *NoOptionError.
func IsNoOption(err error) bool {
	_, ok := err.(*NoOptionError)
	return ok
}

// NoOptionError indicates that a config option is not set.
type NoOptionError struct {
	SnapName string
	Key      string
}

func (e *NoOptionError) Error() string {
	return fmt.Sprintf("snap %q has no %q configuration option", e.SnapName, e.Key)
}
