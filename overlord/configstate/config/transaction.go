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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/jsonutil"
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
	mylog.

		// Record the current state of the map containing the config of every snap
		// in the system. We'll use it for this transaction.
		Check(st.Get("config", &transaction.pristine))
	if errors.Is(err, state.ErrNoState) {
		transaction.pristine = make(map[string]map[string]*json.RawMessage)
	}

	return transaction
}

// State returns the system State
func (t *Transaction) State() *state.State {
	return t.state
}

func changes(cfgStr string, cfg map[string]interface{}) []string {
	var out []string
	for k := range cfg {
		switch subCfg := cfg[k].(type) {
		case map[string]interface{}:
			out = append(out, changes(cfgStr+"."+k, subCfg)...)
		case *json.RawMessage:
			// check if we need to dive into a sub-config
			var configm map[string]interface{}
			if mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(*subCfg), &configm)); err == nil {
				// curiously, json decoder decodes json.RawMessage("null") into a nil map, so no change is
				// reported when we recurse into it. This happens when unsetting a key and the underlying
				// config path doesn't exist.
				if len(configm) > 0 {
					out = append(out, changes(cfgStr+"."+k, configm)...)
					continue
				}
			}
			out = append(out, []string{cfgStr + "." + k}...)
		default:
			out = append(out, []string{cfgStr + "." + k}...)
		}
	}
	return out
}

// Changes returns the changing keys associated with this transaction
func (t *Transaction) Changes() []string {
	var out []string
	for k := range t.changes {
		out = append(out, changes(k, t.changes[k])...)
	}
	sort.Strings(out)
	return out
}

// shadowsExternalConfig checks that the given subkeys/value does not
// "block" the path to a external config with a non-map type. E.g. if
// "network.netplan" is external it must be impossible to set
// "network=false" or getting the document under "network" would be
// wrong.
func shadowsExternalConfig(instanceName string, key string, value interface{}) error {
	// maps never block the path
	if v := reflect.ValueOf(value); v.Kind() == reflect.Map {
		return nil
	}
	// be paranoid: this should never happen but if it does we need to know
	if _, ok := value.(*json.RawMessage); ok {
		return fmt.Errorf("internal error: shadowsExternalConfig called with *json.RawMessage for snap %q with key %q: %q please report as a bug", instanceName, key, value)
	}

	externalConfigMu.Lock()
	km := externalConfigMap[instanceName]
	externalConfigMu.Unlock()

	for externalKey := range km {
		if strings.HasPrefix(externalKey, key+".") {
			return fmt.Errorf("cannot set %q for %q to non-map value because %q is a external configuration", key, instanceName, externalKey)
		}
	}

	return nil
}

// Set sets the provided snap's configuration key to the given value.
// The provided key may be formed as a dotted key path through nested maps.
// For example, the "a.b.c" key describes the {a: {b: {c: value}}} map.
// When the key is provided in that form, intermediate maps are mutated
// rather than replaced, and created when necessary.
//
// The provided value must marshal properly by encoding/json.
// Changes are not persisted until Commit is called.
func (t *Transaction) Set(instanceName, key string, value interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	config := t.changes[instanceName]
	if config == nil {
		config = make(map[string]interface{})
	}

	data := mylog.Check2(json.Marshal(value))

	raw := json.RawMessage(data)

	subkeys := mylog.Check2(ParseKey(key))

	// Check whether it's trying to traverse a non-map from pristine. This
	// would go unperceived by the configuration patching below.
	if len(subkeys) > 1 {
		var result interface{}
		mylog.Check(getFromConfig(instanceName, subkeys, 0, t.pristine[instanceName], &result))
		if err != nil && !IsNoOption(err) {
			return err
		}
	}
	mylog.Check(
		// check that we do not "block" a path to external config with non-maps
		shadowsExternalConfig(instanceName, key, value))

	// config here is never nil and PatchConfig always operates
	// directly on and returns config if it's a
	// map[string]interface{}
	_ = mylog.Check2(PatchConfig(instanceName, subkeys, 0, config, &raw))

	t.changes[instanceName] = config
	return nil
}

func (t *Transaction) copyPristine(snapName string) map[string]*json.RawMessage {
	out := make(map[string]*json.RawMessage)
	if config, ok := t.pristine[snapName]; ok {
		for k, v := range config {
			out[k] = v
		}
	}
	return out
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

	subkeys := mylog.Check2(ParseKey(key))

	// merge external config and then commit changes onto a copy of pristine configuration, so that get has a complete view of the config.
	config := t.copyPristine(snapName)
	mylog.Check(mergeConfigWithExternal(snapName, key, &config))

	applyChanges(config, t.changes[snapName])

	purgeNulls(config)
	return getFromConfig(snapName, subkeys, 0, config, result)
}

// GetMaybe unmarshals into result the cached value of the provided snap's configuration key.
// If the key does not exist, no error is returned.
//
// Transactions do not see updates from the current state or from other transactions.
func (t *Transaction) GetMaybe(instanceName, key string, result interface{}) error {
	mylog.Check(t.Get(instanceName, key, result))
	if err != nil && !IsNoOption(err) {
		return err
	}
	return nil
}

// GetPristine unmarshals the cached pristine (before applying any
// changes) value of the provided snap's configuration key into
// result.
//
// If the key does not exist, an error of type *NoOptionError is returned.
func (t *Transaction) GetPristine(snapName, key string, result interface{}) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	subkeys := mylog.Check2(ParseKey(key))

	return getFromConfig(snapName, subkeys, 0, t.pristine[snapName], result)
}

// GetPristineMaybe unmarshals the cached pristine (before applying any
// changes) value of the provided snap's configuration key into
// result.
//
// If the key does not exist, no error is returned.
func (t *Transaction) GetPristineMaybe(instanceName, key string, result interface{}) error {
	mylog.Check(t.GetPristine(instanceName, key, result))
	if err != nil && !IsNoOption(err) {
		return err
	}
	return nil
}

func getFromConfig(instanceName string, subkeys []string, pos int, config map[string]*json.RawMessage, result interface{}) error {
	// special case - get root document
	if len(subkeys) == 0 {
		if len(config) == 0 {
			return &NoOptionError{SnapName: instanceName}
		}
		raw := jsonRaw(config)
		mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &result))

		return nil
	}

	raw, ok := config[subkeys[pos]]
	if !ok {
		return &NoOptionError{SnapName: instanceName, Key: strings.Join(subkeys[:pos+1], ".")}
	}

	// There is a known problem with json raw messages representing nulls when they are stored in nested structures, such as
	// config map inside our state. These are turned into nils and need to be handled explicitly.
	if raw == nil {
		m := json.RawMessage("null")
		raw = &m
	}

	if pos+1 == len(subkeys) {
		mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &result))

		return nil
	}

	var configm map[string]*json.RawMessage
	mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(*raw), &configm))

	return getFromConfig(instanceName, subkeys, pos+1, configm, result)
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
	mylog.

		// Update our copy of the config with the most recent one from the state.
		Check(t.state.Get("config", &t.pristine))
	if errors.Is(err, state.ErrNoState) {
		t.pristine = make(map[string]map[string]*json.RawMessage)
	}

	// Iterate through the write cache and save each item but exclude external configuration
	for instanceName, snapChanges := range t.changes {
		clearExternalConfig(instanceName, snapChanges)

		config := t.pristine[instanceName]
		// due to LP #1917870 we might have a hook configure task in flight
		// that tries to apply config over nil map, create it if nil.
		if config == nil {
			config = make(map[string]*json.RawMessage)
		}
		applyChanges(config, snapChanges)
		purgeNulls(config)
		t.pristine[instanceName] = config
	}

	t.state.Set("config", t.pristine)

	// The cache has been flushed, reset it.
	t.changes = make(map[string]map[string]interface{})
}

func applyChanges(config map[string]*json.RawMessage, changes map[string]interface{}) {
	for k, v := range changes {
		config[k] = commitChange(config[k], v)
	}
}

func jsonRaw(v interface{}) *json.RawMessage {
	data := mylog.Check2(json.Marshal(v))

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
		mylog.Check(jsonutil.DecodeWithNumber(bytes.NewReader(*pristine), &pristinem))
		// Not a map. Overwrite with the change.

		for k, v := range change {
			pristinem[k] = commitChange(pristinem[k], v)
		}
		return jsonRaw(pristinem)
	}
	panic(fmt.Errorf("internal error: unexpected configuration type %T", change))
}

// overlapsWithExternalConfig() return true if the requested key overlaps with
// the given external key. E.g.
// true: for requested key "a" and external key "a.external"
// false for requested key "z" and external key "a.external"
func overlapsWithExternalConfig(requestedKey, externalKey string) (bool, error) {
	requestedSubkeys := mylog.Check2(ParseKey(requestedKey))

	externalSubkeys := mylog.Check2(ParseKey(externalKey))

	for i := range requestedSubkeys {
		if i >= len(externalSubkeys) {
			return true, nil
		}
		if externalSubkeys[i] != requestedSubkeys[i] {
			return false, nil
		}
	}
	return true, nil
}

// mergeConfigWithExternal takes the given configuration and merges it
// with the external configuration values by calling the registered
// external configuration function of the given snap. The merged config
// is returned.
func mergeConfigWithExternal(instanceName, requestedKey string, origConfig *map[string]*json.RawMessage) error {
	externalConfigMu.Lock()
	km, ok := externalConfigMap[instanceName]
	externalConfigMu.Unlock()
	if !ok {
		return nil
	}

	// create a "patch" from the external entries
	patch := make(map[string]interface{})
	for externalKey, externalFn := range km {
		if externalFn == nil {
			continue
		}
		// check if the requested key is part of the external
		// configuration
		partOf := mylog.Check2(overlapsWithExternalConfig(requestedKey, externalKey))

		if !partOf {
			continue
		}

		// Pass the right key to the externalFn(), this can
		// either be a subtree of the external-tree or the
		// other externalKey itself.
		k := requestedKey
		if len(requestedKey) < len(externalKey) {
			k = externalKey
		}
		res := mylog.Check2(externalFn(k))

		patch[externalKey] = jsonRaw(res)
	}
	if len(patch) == 0 {
		return nil
	}

	// create a "working copy" of the config and apply the patches on top
	config := jsonRaw(*origConfig)
	patchKeys := sortPatchKeysByDepth(patch)
	for _, subkeys := range patchKeys {
		// patch[key] above got assigned jsonRaw() so this cast is ok
		raw := patch[subkeys].(*json.RawMessage)
		mergedConfig := mylog.Check2(PatchConfig(instanceName, strings.Split(subkeys, "."), 0, config, raw))

		// PatchConfig got *json.RawMessage as input and
		// returns the same type so this cast is ok (but be defensive)
		config, ok = mergedConfig.(*json.RawMessage)
		if !ok {
			return fmt.Errorf("internal error: PatchConfig in mergeConfigWithExternal did not return a *json.RawMessage please report this as a bug")
		}
	}
	mylog.Check(

		// XXX: unmarshaling on top of something leaves values in place
		// (no problem here because we only add external things)
		// convert back to the original config
		jsonutil.DecodeWithNumber(bytes.NewReader(*config), origConfig))

	return nil
}

// clearExternalConfig iterates over a given config and removes any values
// that come from external configuration. This is used before committing a
// config to disk.
func clearExternalConfig(instanceName string, snapChanges map[string]interface{}) {
	externalConfigMu.Lock()
	km := externalConfigMap[instanceName]
	externalConfigMu.Unlock()

	clearExternalConfigRecursive(km, snapChanges, "")
}

func clearExternalConfigRecursive(km map[string]ExternalCfgFunc, config map[string]interface{}, keyprefix string) {
	if len(keyprefix) > 0 {
		keyprefix += "."
	}
	for key, value := range config {
		// any top-level external keys are removed
		if _, ok := km[keyprefix+key]; ok {
			delete(config, key)
			// we can skip looking for nested config if we
			// removed the top-level
			continue
		}
		// and nested configs are inspected
		if m, ok := value.(map[string]interface{}); ok {
			clearExternalConfigRecursive(km, m, keyprefix+key)
		}
	}
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
	if e.Key == "" {
		return fmt.Sprintf("snap %q has no configuration", e.SnapName)
	}
	return fmt.Sprintf("snap %q has no %q configuration option", e.SnapName, e.Key)
}

// ExternalCfgFunc can be used for external "transaction.Get()" calls
type ExternalCfgFunc func(key string) (result interface{}, err error)

// externalConfigMap contain hook functions for "external" configuration. The
// first level of the map is the snapName and then the external keys in
// dotted notation e.g. "network.netplan".  Any data under a external
// configuration option is never stored directly in the state.
var (
	externalConfigMap map[string]map[string]ExternalCfgFunc
	externalConfigMu  sync.Mutex
)

// RegisterExternalConfig allows to register a function that is called
// when the configuration for the given config key for a given
// snapname is requested.
//
// This is useful when the configuration is stored externally,
// e.g. the systems hostname is stored via `hostnamectl` and it does not
// make sense to store it the snapd state. Examples are:
// - system.timezone
// - system.hostname
func RegisterExternalConfig(snapName, key string, vf ExternalCfgFunc) error {
	externalConfigMu.Lock()
	defer externalConfigMu.Unlock()
	mylog.Check2(ParseKey(key))

	if externalConfigMap == nil {
		externalConfigMap = make(map[string]map[string]ExternalCfgFunc)
	}
	if _, ok := externalConfigMap[snapName]; !ok {
		externalConfigMap[snapName] = make(map[string]ExternalCfgFunc)
	}
	externalConfigMap[snapName][key] = vf
	return nil
}
