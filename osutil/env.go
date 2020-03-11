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

package osutil

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/strutil"
)

// GetenvBool returns whether the given key may be considered "set" in the
// environment (i.e. it is set to one of "1", "true", etc).
//
// An optional second argument can be provided, which determines how to
// treat missing or unparsable values; default is to treat them as false.
func GetenvBool(key string, dflt ...bool) bool {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}

	if len(dflt) > 0 {
		return dflt[0]
	}

	return false
}

// GetenvInt64 interprets the value of the given environment variable
// as an int64 and returns the corresponding value. The base can be
// implied via the prefix (0x for 16, 0 for 8; otherwise 10).
//
// An optional second argument can be provided, which determines how to
// treat missing or unparsable values; default is to treat them as 0.
func GetenvInt64(key string, dflt ...int64) int64 {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		if b, err := strconv.ParseInt(val, 0, 64); err == nil {
			return b
		}
	}

	if len(dflt) > 0 {
		return dflt[0]
	}

	return 0
}

// Environment is an unordered map of key=value strings.
//
// Environment can be manipulated with available methods and eventually
// converted to low-level representation necessary when executing programs.
// This approach discourages operations that could result in duplicate
// environment variable definitions from being constructed.
type Environment map[string]string

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

// parseRawEnvironment parsers raw environment.
//
// This function fails if any of the provided values are not in the form of
// key=value or if there are duplicate keys.
func parseRawEnvironment(raw []string) (Environment, error) {
	env := make(Environment, len(raw))
	for _, entry := range raw {
		key, value, err := parseEnvEntry(entry)
		if err != nil {
			return nil, err
		}
		if _, ok := env[key]; ok {
			return nil, fmt.Errorf("cannot overwrite earlier value of %q", key)
		}
		env[key] = value
	}
	return env, nil
}

// OSEnvironment returns the environment of the calling process.
func OSEnvironment() (Environment, error) {
	return parseRawEnvironment(os.Environ())
}

// Transform programmatically replaces all keys and values.
//
// If multiple keys are transformed into the same key the value of the last
// (lexicographically) key is used. If the transformed key is empty then the
// corresponding entry is removed.
func (env *Environment) Transform(tr func(key, value string) (string, string)) {
	newEntries := make(map[string]string, len(*env))
	keys := make([]string, 0, len(*env))
	for key := range *env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		newKey, newValue := tr(key, (*env)[key])
		if newKey != "" {
			newEntries[newKey] = newValue
		}
	}
	*env = newEntries
}

// ForExec returns environment suitable for using with exec family of functions.
//
// The returned environment is sorted lexicographically by variable name.
func (env Environment) ForExec() []string {
	raw := make([]string, 0, len(env))
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		raw = append(raw, fmt.Sprintf("%s=%s", key, env[key]))
	}
	return raw
}

// ExpandableEnv describes alterations to an environment.
//
// Differential environment can refer to existing entries by using shell-like
// syntax $KEY or ${KEY}. Entries are ordered.
type ExpandableEnv struct {
	*strutil.OrderedMap
}

// NewExpandableEnv returns a new expandable environment comprised of given pairs.
func NewExpandableEnv(pairs ...string) ExpandableEnv {
	return ExpandableEnv{OrderedMap: strutil.NewOrderedMap(pairs...)}
}

// SetExpandableEnv sets variables defined by expandable environment
//
// Environment is modified in place. Each variable defined by expandable
// environment is expanded according to os.Expand, using the environment itself
// as data source. Undefined variables expand to an empty string.
func (env *Environment) SetExpandableEnv(eenv ExpandableEnv) {
	if *env == nil {
		*env = make(Environment)
	}

	for _, key := range eenv.Keys() {
		(*env)[key] = os.Expand(eenv.Get(key), func(varName string) string {
			return (*env)[varName]
		})
	}
}
