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

	"github.com/snapcore/snapd/overlord/state"
)

// Transaction is responsible for configuration transactions. Its config will
// never change outside the control of its owner, and any changes made to it
// doesn't affect the overall system config. Change it, query it, and if you
// really want to get any changes saved back into the config, Commit() it.
type Transaction struct {
	state *state.State

	data struct {
		Config     systemConfig `json:"config"`
		WriteCache systemConfig `json:"write-cache"`
	}
}

type snapConfig map[string]*json.RawMessage
type systemConfig map[string]snapConfig

func newTransaction(state *state.State) *Transaction {
	state.Lock()
	defer state.Unlock()

	transaction := &Transaction{state: state}
	transaction.data.WriteCache = make(systemConfig)

	// Record the current state of the map containing the config of every snap
	// in the system. We'll use it for this transaction.
	if err := state.Get("config", &transaction.data.Config); err != nil {
		transaction.data.Config = make(systemConfig)
	}

	return transaction
}

// Set associates the value with the key for the given snap. Note that this
// doesn't save any changes (see Commit()), so if the transaction is destroyed
// the changes have no effect. Also note that the value must properly marshal
// and unmarshal with encoding/json.
func (t *Transaction) Set(snapName, key string, value interface{}) error {
	config, ok := t.data.WriteCache[snapName]
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
	t.data.WriteCache[snapName] = config

	return nil
}

// Get extracts the stored value associated with the provided key for the snap's
// config into the value parameter. Note that this query is run against the
// cached copy of the config; you won't see updates from other transactions.
func (t *Transaction) Get(snapName, key string, value interface{}) error {
	// Extract the config for this specific snap. Check the cache first; if it's
	// not there, check the config from the state
	if err := t.get(t.data.WriteCache, snapName, key, value); err != nil {
		return t.get(t.data.Config, snapName, key, value)
	}

	return nil
}

// Commit actually saves the changes made to the config in the transaction to
// the state.
func (t *Transaction) Commit() {
	t.state.Lock()
	defer t.state.Unlock()

	// Update our copy of the config with the most recent one from the state.
	if err := t.state.Get("config", &t.data.Config); err != nil {
		// Make sure it's still a valid map, in case Get() modified it.
		t.data.Config = make(systemConfig)
	}

	// Iterate through the write cache and save each item.
	for snapName, snapConfigToWrite := range t.data.WriteCache {
		newSnapConfig, ok := t.data.Config[snapName]
		if !ok {
			newSnapConfig = make(snapConfig)
		}

		for key, value := range snapConfigToWrite {
			newSnapConfig[key] = value
		}

		t.data.Config[snapName] = newSnapConfig
	}

	t.state.Set("config", t.data.Config)

	// The cache has been flushed-- reset it
	t.data.WriteCache = make(systemConfig)
}

func (t *Transaction) MarshalJSON() ([]byte, error) {
	return json.Marshal(t.data)
}

func (t *Transaction) UnmarshalJSON(bytes []byte) error {
	return json.Unmarshal(bytes, &t.data)
}

func (t *Transaction) get(config systemConfig, snapName, key string, value interface{}) error {
	c, ok := config[snapName]
	if !ok {
		return fmt.Errorf("no config available for snap %q", snapName)
	}

	raw, ok := c[key]
	if !ok {
		return fmt.Errorf("snap %q has no config value for key %q", snapName, key)
	}

	err := json.Unmarshal([]byte(*raw), &value)
	if err != nil {
		return fmt.Errorf("cannot unmarshal snap %q config value for %q: %s", snapName, key, err)
	}

	return nil
}
