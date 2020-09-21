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

package assertstate

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/snapcore/snapd/overlord/state"
)

// ValidationMode reflects the mode of respective validation set, which is
// either monitoring or enforcing.
type ValidationMode int

const (
	Monitor ValidationMode = iota
	Enforce
)

// ValidationTrackingKey is a key for tracking the given validation set.
type ValidationTrackingKey struct {
	AccoundID string
	Name      string
}

func (v *ValidationTrackingKey) String() string {
	return fmt.Sprintf("%s/%s", v.AccoundID, v.Name)
}

// MarshalText implements TextMarshaler and allows for using ValidationTrackingKey
// as map key when marshaling into JSON.
func (v ValidationTrackingKey) MarshalText() (text []byte, err error) {
	return []byte(v.String()), nil
}

// UnmarshalText implements TextUnmarshaler and allows for using ValidationTrackingKey
// as map key when unmarshaling from JSON.
func (v *ValidationTrackingKey) UnmarshalText(text []byte) error {
	parts := strings.Split(string(text), "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid validation set tracking key: %q", string(text))
	}
	v.AccoundID = parts[0]
	v.Name = parts[1]
	return nil
}

// ValidationSetTracking holds tracking parameters for associated validation set.
type ValidationSetTracking struct {
	Mode ValidationMode `json:"mode"`

	// PinnedSeq is an optional pinned sequence point, or 0 if not pinned.
	PinnedSeq int `json:"pinned-seq,omitempty"`

	// LastSeq is the last known sequence point obtained from the store.
	LastSeq int `json:"last-seq,omitempty"`
}

// SetValidationTracking sets ValidationSetTracking for the given key. Passing nil
// for ValidationSetTracking removes the given key from state.
func SetValidationTracking(st *state.State, key ValidationTrackingKey, tr *ValidationSetTracking) {
	var vsmap map[ValidationTrackingKey]*json.RawMessage
	err := st.Get("validation-set-tracking", &vsmap)
	if err != nil && err != state.ErrNoState {
		panic("internal error: cannot unmarshal validation-set-tracking state: " + err.Error())
	}
	if vsmap == nil {
		vsmap = make(map[ValidationTrackingKey]*json.RawMessage)
	}
	if tr == nil {
		delete(vsmap, key)
	} else {
		data, err := json.Marshal(tr)
		if err != nil {
			panic("internal error: cannot marshal validation set tracking data: " + err.Error())
		}
		raw := json.RawMessage(data)
		vsmap[key] = &raw
	}
	st.Set("validation-set-tracking", vsmap)
}

// GetValidationTracking retrieves the ValidationSetTracking for the given key.
func GetValidationTracking(st *state.State, key ValidationTrackingKey, tr *ValidationSetTracking) error {
	if tr == nil {
		return fmt.Errorf("internal error: tr is nil")
	}

	*tr = ValidationSetTracking{}

	var vset map[ValidationTrackingKey]*json.RawMessage
	err := st.Get("validation-set-tracking", &vset)
	if err != nil {
		return err
	}
	raw, ok := vset[key]
	if !ok {
		return state.ErrNoState
	}
	err = json.Unmarshal([]byte(*raw), &tr)
	if err != nil {
		return fmt.Errorf("cannot unmarshal validation set tracking: %v", err)
	}
	return nil
}

// All retrieves all ValidationSetTracking data.
func All(st *state.State) (map[ValidationTrackingKey]*ValidationSetTracking, error) {
	var vsmap map[ValidationTrackingKey]*ValidationSetTracking
	if err := st.Get("validation-set-tracking", &vsmap); err != nil && err != state.ErrNoState {
		return nil, err
	}

	validationSetMap := make(map[ValidationTrackingKey]*ValidationSetTracking, len(vsmap))
	for key, trackingInfo := range vsmap {
		validationSetMap[key] = trackingInfo
	}
	return validationSetMap, nil
}
