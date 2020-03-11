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

func (env Environment) EscapeUnsafeVariables() {
	for key, value := range env {
		if unsafeEnv[key] {
			newKey := preservedUnsafePrefix + key
			delete(env, key)
			env[newKey] = value
		}
	}
}

func (env Environment) UnescapeSaved() {
	for key, value := range env {
		if newKey := strings.TrimPrefix(key, preservedUnsafePrefix); key != newKey {
			delete(env, key)
			env[newKey] = value
		}
	}
}

// unsafeEnv is a set of unsafe environment variables.
//
// Environment variables glibc strips out when running a setuid binary.
// Taken from https://sourceware.org/git/?p=glibc.git;a=blob_plain;f=sysdeps/generic/unsecvars.h;hb=HEAD
// TODO: use go generate to obtain this list at build time.
var unsafeEnv = map[string]bool{
	"GCONV_PATH":       true,
	"GETCONF_DIR":      true,
	"GLIBC_TUNABLES":   true,
	"HOSTALIASES":      true,
	"LD_AUDIT":         true,
	"LD_DEBUG":         true,
	"LD_DEBUG_OUTPUT":  true,
	"LD_DYNAMIC_WEAK":  true,
	"LD_HWCAP_MASK":    true,
	"LD_LIBRARY_PATH":  true,
	"LD_ORIGIN_PATH":   true,
	"LD_PRELOAD":       true,
	"LD_PROFILE":       true,
	"LD_SHOW_AUXV":     true,
	"LD_USE_LOAD_BIAS": true,
	"LOCALDOMAIN":      true,
	"LOCPATH":          true,
	"MALLOC_TRACE":     true,
	"NIS_PATH":         true,
	"NLSPATH":          true,
	"RESOLV_HOST_CONF": true,
	"RES_OPTIONS":      true,
	"TMPDIR":           true,
	"TZDIR":            true,
}

const preservedUnsafePrefix = "SNAP_SAVED_"
