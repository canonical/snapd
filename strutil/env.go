// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package strutil

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// RawEnvironment is a slice of key=value strings.
//
// Using this type is required for low-level interactions, like invoking
// execve. In other cases using Environment is both sufficient and safer.
type RawEnvironment []string

// String returns the readable, quoted representation of the raw environment.
func (raw RawEnvironment) String() string {
	return Quoted(raw)
}

// Environment is an unordered map of key=value strings.
//
// Environment can be manipulated with available methods and eventually
// converted to RawEnvironment. This approach discourages operations that could
// result in duplicate environment variable definitions from being constructed.
type Environment struct {
	entries map[string]string
}

// NewEnvironment returns an environment with a copy of given entries.
func NewEnvironment(entries map[string]string) *Environment {
	env := &Environment{entries: make(map[string]string, len(entries))}
	for key, value := range entries {
		env.entries[key] = value
	}
	return env
}

func parseEnvEntry(entry string) (string, string, error) {
	parts := strings.SplitN(entry, "=", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("cannot parse environment entry: %q", entry)
	}
	key, value := parts[0], parts[1]
	if key == "" {
		return "", "", fmt.Errorf("environment variable name cannot be empty: %q", entry)
	}
	return key, value, nil
}

// ParseRawEnvironment parsers raw environment.
//
// This function fails if any of the provided values are not in the form of
// key=value or if there are duplicate keys.
func ParseRawEnvironment(raw RawEnvironment) (*Environment, error) {
	env := &Environment{entries: make(map[string]string, len(raw))}
	for _, entry := range raw {
		key, value, err := parseEnvEntry(entry)
		if err != nil {
			return nil, err
		}
		if _, ok := env.entries[key]; ok {
			return nil, fmt.Errorf("cannot overwrite earlier value of %q", key)
		}
		env.entries[key] = value
	}
	return env, nil
}

// OSEnvironment returns the environment of the calling process.
func OSEnvironment() (*Environment, error) {
	return ParseRawEnvironment(os.Environ())
}

// Transform programmatically replaces all keys and values.
//
// If multiple keys are transformed into the same key the value of the last
// (lexicographically) key is used. If the transformed key is empty then the
// corresponding entry is removed.
func (env *Environment) Transform(tr func(key, value string) (string, string)) {
	newEntries := make(map[string]string, len(env.entries))
	keys := make([]string, 0, len(env.entries))
	for key := range env.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		newKey, newValue := tr(key, env.entries[key])
		if newKey != "" {
			newEntries[newKey] = newValue
		}
	}
	env.entries = newEntries
}

// Get returns the value of a given environment variable.
func (env *Environment) Get(key string) string {
	return env.entries[key]
}

// Contains returns true if given environment variable is set.
func (env *Environment) Contains(key string) bool {
	_, ok := env.entries[key]
	return ok
}

// Del removes the given environment variable.
func (env *Environment) Del(key string) {
	delete(env.entries, key)
}

// Set adds or replaces the given environment variable.
func (env *Environment) Set(key, value string) {
	if env.entries == nil {
		env.entries = make(map[string]string)
	}
	env.entries[key] = value
}

// RawEnvironment returns the raw equivalent of the given environment.
// The returned environment is sorted lexicographically by variable name.
func (env *Environment) RawEnvironment() RawEnvironment {
	raw := make([]string, 0, len(env.entries))
	keys := make([]string, 0, len(env.entries))
	for key := range env.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw = append(raw, fmt.Sprintf("%s=%s", key, env.entries[key]))
	}
	return raw
}

// EnvironmentDelta describes alterations to an environment.
//
// Differential environment can refer to existing entries by using shell-like
// syntax $KEY or ${KEY}. Entries inside an environment delta are ordered.
type EnvironmentDelta struct {
	OrderedMap
}

// NewEnvironmentDelta returns a new environment delta comprised of given pairs.
func NewEnvironmentDelta(pairs ...string) *EnvironmentDelta {
	return &EnvironmentDelta{OrderedMap: *NewOrderedMap(pairs...)}
}

// Copy returns a copy of the environment delta.
func (delta *EnvironmentDelta) Copy() *EnvironmentDelta {
	return &EnvironmentDelta{OrderedMap: *delta.OrderedMap.Copy()}
}

// Merge combines two environment deltas.
//
// Clashing environment variables are overwritten and the value from the
// "other" delta prevails.
func (delta *EnvironmentDelta) Merge(other *EnvironmentDelta) {
	for _, key := range other.Keys() {
		value := other.Get(key)
		delta.Set(key, value)
	}
}

// ApplyDelta applies a delta to the environment.
//
// Environment is modified in place. Delta acts like an environment that can
// contain additional key=value pairs, where values can refer to any key set in
// the environment or eventually computed by applying the delta.
//
// The return value is the ordered list of variables that were referenced by
// the delta but were never defined. They are expanded to an empty string.
func (env *Environment) ApplyDelta(delta *EnvironmentDelta) []string {
	keys := delta.Keys()
	applied := make(map[string]bool, len(keys))

	// Keep trying to expand variables for as long as we are making progress.
	changed := true
	for changed {
		changed = false
		for _, key := range keys {
			if !applied[key] {
				// Attempt to expand each value and if successful, update the
				// environment and record this .
				value := delta.Get(key)
				good := true
				valueExp := os.Expand(value, func(varName string) string {
					if !env.Contains(varName) {
						good = false
					}
					return env.Get(varName)
				})
				if !good {
					// If we cannot expand the value yet then just continue.
					continue
				}
				applied[key] = true
				changed = true
				env.Set(key, valueExp)
			}
		}
	}

	// If we've expanded all the values then there's no need to continue.
	if len(applied) == len(keys) {
		return nil
	}

	// We've reached a stable state but were unable to expand some variables.
	// Expand them to the empty string and collect their names.
	undefined := make(map[string]bool, len(keys)-len(applied))
	for _, key := range keys {
		if !applied[key] {
			value := delta.Get(key)
			valueExp := os.Expand(value, func(varName string) string {
				if !env.Contains(varName) {
					undefined[varName] = true
					return ""
				}
				return env.Get(varName)
			})
			env.Set(key, valueExp)
		}
	}

	// Return the information about undefined variables.
	vars := make([]string, 0, len(undefined))
	for name, ok := range undefined {
		if ok {
			vars = append(vars, name)
		}
	}
	sort.Strings(vars)
	return vars
}
