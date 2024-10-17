// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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

package daemon

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

var (
	snapConfCmd = &Command{
		Path:        "/v2/snaps/{name}/conf",
		GET:         getSnapConf,
		PUT:         setSnapConf,
		ReadAccess:  authenticatedAccess{Polkit: polkitActionManageConfiguration},
		WriteAccess: authenticatedAccess{Polkit: polkitActionManageConfiguration},
	}
)

func getSnapConf(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	snapName := configstate.RemapSnapFromRequest(vars["name"])

	keys := strutil.CommaSeparatedList(r.URL.Query().Get("keys"))

	s := c.d.overlord.State()
	s.Lock()
	tr := config.NewTransaction(s)
	s.Unlock()

	currentConfValues := make(map[string]interface{})
	// Special case - return root document
	if len(keys) == 0 {
		keys = []string{""}
	}
	for _, key := range keys {
		var value interface{}
		if err := tr.Get(snapName, key, &value); err != nil {
			if config.IsNoOption(err) {
				if key == "" {
					// no configuration - return empty document
					currentConfValues = make(map[string]interface{})
					break
				}
				return &apiError{
					Status:  400,
					Message: err.Error(),
					Kind:    client.ErrorKindConfigNoSuchOption,
					Value:   err,
				}
			} else {
				return InternalError("%v", err)
			}
		}

		// Hide old experimental features (that are no longer experimental)
		if snapName == "core" {
			var err *apiError
			value, err = cleanExperimentalFlags(key, value)
			if err != nil {
				return err
			}
		}
		if key == "" {
			if len(keys) > 1 {
				return BadRequest("keys contains zero-length string")
			}
			return SyncResponse(value)
		}

		currentConfValues[key] = value
	}

	return SyncResponse(currentConfValues)
}

// cleanExperimentalFlags returns an exact copy of val after hiding experimental
// features that are no longer supported from core.experimental configuration
// generic queries (i.e. key is "" or "experimental").
// For exact queries (e.g. core.experimental.old-flag) a client.ErrorKindConfigNoSuchOption
// error is returned.
//
// This helper should only be called for core configurations. Any errors when parsing
// core config are ignored and val is returned without modification.
func cleanExperimentalFlags(key string, val interface{}) (interface{}, *apiError) {
	if val == nil {
		return val, nil
	}

	if strings.HasPrefix(key, "experimental.") {
		flag := strings.TrimPrefix(key, "experimental.")
		if !configcore.IsSupportedExperimentalFlag(flag) {
			err := &config.NoOptionError{
				SnapName: "core",
				Key:      key,
			}
			return nil, &apiError{
				Status:  400,
				Message: err.Error(),
				Kind:    client.ErrorKindConfigNoSuchOption,
				Value:   err,
			}
		}
	}

	if key != "" && key != "experimental" {
		// We only care about config that might contain old experimental features
		return val, nil
	}

	var experimentalFlags map[string]interface{}
	if key == "" {
		rootConfig, ok := val.(map[string]interface{})
		if !ok {
			// XXX: This should never happen, skip cleaning
			return val, nil
		}
		experimentalFlags, ok = rootConfig["experimental"].(map[string]interface{})
		if !ok {
			// No experimental key, do nothing
			return val, nil
		}
	} else {
		var ok bool
		experimentalFlags, ok = val.(map[string]interface{})
		if !ok {
			// XXX: This should never happen, skip cleaning
			return val, nil
		}
	}

	for flag := range experimentalFlags {
		if !configcore.IsSupportedExperimentalFlag(flag) {
			// Hide the no longer supported experimental flag
			delete(experimentalFlags, flag)
		}
	}

	// Changes in experimentalFlags should reflect in values
	return val, nil
}

func setSnapConf(c *Command, r *http.Request, user *auth.UserState) Response {
	vars := muxVars(r)
	snapName := configstate.RemapSnapFromRequest(vars["name"])

	var patchValues map[string]interface{}
	if err := jsonutil.DecodeWithNumber(r.Body, &patchValues); err != nil {
		return BadRequest("cannot decode request body into patch values: %v", err)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	taskset, err := configstate.ConfigureInstalled(st, snapName, patchValues, 0)
	if err != nil {
		// TODO: just return snap-not-installed instead ?
		if _, ok := err.(*snap.NotInstalledError); ok {
			return SnapNotFound(snapName, err)
		}
		return errToResponse(err, []string{snapName}, InternalError, "%v")
	}

	summary := fmt.Sprintf("Change configuration of %q snap", snapName)
	change := newChange(st, "configure-snap", summary, []*state.TaskSet{taskset}, []string{snapName})

	st.EnsureBefore(0)

	return AsyncResponse(nil, change.ID())
}
