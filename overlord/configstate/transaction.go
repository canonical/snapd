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

// Transaction is responsible for configuration transactions. Its config will
// never change outside the control of its owner, and any changes made to it
// doesn't affect the overall system config. Change it, query it, and if you
// really want to get any changes saved back into the config, Commit it.
//
// Note that Transactions are safe to access concurrently.
type Transaction struct {
	state           *state.State
	config          systemConfig
	configMutex     sync.RWMutex
	writeCache      systemConfig
	writeCacheMutex sync.RWMutex
}

type snapConfig map[string]*json.RawMessage
type systemConfig map[string]snapConfig

// NewTransaction creates a new config transaction initialized with the given
// state. Note that the state should be locked/unlocked by the caller.
func NewTransaction(st *state.State) (*Transaction, error) {
	transaction := &Transaction{state: st}
	transaction.writeCache = make(systemConfig)

	// Record the current state of the map containing the config of every snap
	// in the system. We'll use it for this transaction.
	if err := st.Get("config", &transaction.config); err != nil {
		if err != state.ErrNoState {
			return nil, err
		}

		transaction.config = make(systemConfig)
	}

	return transaction, nil
}

// Set associates the value with the key for the given snap. Note that this
// doesn't save any changes (see Commit), so if the transaction is destroyed
// the changes have no effect. Also note that the value must properly marshal
// and unmarshal with encoding/json.
func (t *Transaction) Set(snapName, key string, value interface{}) error {
	t.writeCacheMutex.Lock()
	defer t.writeCacheMutex.Unlock()

	config, ok := t.writeCache[snapName]
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
	t.writeCache[snapName] = config

	return nil
}

// Get extracts the stored value associated with the provided key for the snap's
// config into the value parameter. Note that this query is run against the
// cached copy of the config; you won't see updates from other transactions.
func (t *Transaction) Get(snapName, key string, value interface{}) error {
	// Extract the config for this specific snap. Check the cache first; if it's
	// not there, check the config from the state
	t.writeCacheMutex.RLock()
	defer t.writeCacheMutex.RUnlock()
	if err := t.get(t.writeCache, snapName, key, value); err != nil {
		t.configMutex.RLock()
		defer t.configMutex.RUnlock()
		return t.get(t.config, snapName, key, value)
	}

	return nil
}

// Commit actually saves the changes made to the config in the transaction to
// the state. Note that the state should be locked/unlocked by the caller.
func (t *Transaction) Commit() {
	t.writeCacheMutex.Lock()
	defer t.writeCacheMutex.Unlock()

	// Do nothing if there's nothing to commit.
	if len(t.writeCache) == 0 {
		return
	}

	t.configMutex.Lock()
	defer t.configMutex.Unlock()

	// Update our copy of the config with the most recent one from the state.
	if err := t.state.Get("config", &t.config); err != nil {
		// Make sure it's still a valid map, in case Get modified it.
		t.config = make(systemConfig)
	}

	// Iterate through the write cache and save each item.
	for snapName, snapConfigToWrite := range t.writeCache {
		newSnapConfig, ok := t.config[snapName]
		if !ok {
			newSnapConfig = make(snapConfig)
		}

		for key, value := range snapConfigToWrite {
			newSnapConfig[key] = value
		}

		t.config[snapName] = newSnapConfig
	}

	t.state.Set("config", t.config)

	// The cache has been flushed-- reset it
	t.writeCache = make(systemConfig)
}

func (t *Transaction) get(config systemConfig, snapName, key string, value interface{}) error {
	c, ok := config[snapName]
	if !ok {
		return fmt.Errorf("snap %q has no %q configuration option", snapName, key)
	}

	raw, ok := c[key]
	if !ok {
		return fmt.Errorf("snap %q has no %q configuration option", snapName, key)
	}

	err := json.Unmarshal([]byte(*raw), &value)
	if err != nil {
		return fmt.Errorf("cannot unmarshal snap %q config value for %q: %s", snapName, key, err)
	}

	return nil
}
