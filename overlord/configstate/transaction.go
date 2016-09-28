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

package configstate

import (
	"encoding/json"
	"fmt"
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
	pristine systemConfig
	changes  systemConfig
}

type snapConfig map[string]*json.RawMessage
type systemConfig map[string]snapConfig

// NewTransaction creates a new configuration transaction initialized with the given state.
//
// The provided state must be locked by the caller.
func NewTransaction(st *state.State) *Transaction {
	transaction := &Transaction{state: st}
	transaction.changes = make(systemConfig)

	// Record the current state of the map containing the config of every snap
	// in the system. We'll use it for this transaction.
	err := st.Get("config", &transaction.pristine)
	if err == state.ErrNoState {
		transaction.pristine = make(systemConfig)
	} else if err != nil {
		panic(fmt.Errorf("internal error: cannot unmarshal configuration: %v", err))
	}
	return transaction
}

// Set sets the provided snap's configuration key to the given value.
//
// The provided value must marshal properly by encoding/json.
// Changes are not persisted until Commit is called.
func (t *Transaction) Set(snapName, key string, value interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	config, ok := t.changes[snapName]
	if !ok {
		config = make(snapConfig)
	}

	// Place the new config value into the snap config
	marshalledValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot marshal snap %q config value for %q: %s", snapName, key, err)
	}
	raw := json.RawMessage(marshalledValue)
	config[key] = &raw

	// Put that config into the write cache
	t.changes[snapName] = config

	return nil
}

// Get unmarshals into result the cached value of the provided snap's configuration key.
// If the key does not exist, an error of type *NoOptionError is returned.
//
// Transactions do not see updates from the current state or from other transactions.
func (t *Transaction) Get(snapName, key string, result interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	err := t.get(t.changes, snapName, key, result)
	if IsNoOption(err) {
		err = t.get(t.pristine, snapName, key, result)
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

// Commit saves to the state the configuration changes made in the transaction.
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
		t.pristine = make(systemConfig)
	} else if err != nil {
		panic(fmt.Errorf("internal error: cannot unmarshal configuration: %v", err))
	}

	// Iterate through the write cache and save each item.
	for snapName, snapChanges := range t.changes {
		newConfig, ok := t.pristine[snapName]
		if !ok {
			newConfig = make(snapConfig)
		}

		for key, value := range snapChanges {
			newConfig[key] = value
		}

		t.pristine[snapName] = newConfig
	}

	t.state.Set("config", t.pristine)

	// The cache has been flushed, reset it.
	t.changes = make(systemConfig)
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

// NoOptionError indicates that a config option is not set.
type NoOptionError struct {
	SnapName string
	Key      string
}

func (e *NoOptionError) Error() string {
	return fmt.Sprintf("snap %q has no %q configuration option", e.SnapName, e.Key)
}

func (t *Transaction) get(config systemConfig, snapName, key string, value interface{}) error {
	raw, ok := config[snapName][key]
	if !ok {
		return &NoOptionError{SnapName: snapName, Key: key}
	}

	err := json.Unmarshal([]byte(*raw), &value)
	if err != nil {
		return fmt.Errorf("internal error: cannot unmarshal snap %q option %q into %T: %s, json: %s", snapName, key, value, err, *raw)
	}
	return nil
}
