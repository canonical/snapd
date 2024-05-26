// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package patch

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

type patch62SideInfo struct {
	RealName          string        `yaml:"name,omitempty" json:"name,omitempty"`
	SnapID            string        `yaml:"snap-id" json:"snap-id"`
	Revision          snap.Revision `yaml:"revision" json:"revision"`
	Channel           string        `yaml:"channel,omitempty" json:"channel,omitempty"`
	Contact           string        `yaml:"contact,omitempty" json:"contact,omitempty"`
	EditedTitle       string        `yaml:"title,omitempty" json:"title,omitempty"`
	EditedSummary     string        `yaml:"summary,omitempty" json:"summary,omitempty"`
	EditedDescription string        `yaml:"description,omitempty" json:"description,omitempty"`
	Private           bool          `yaml:"private,omitempty" json:"private,omitempty"`
	Paid              bool          `yaml:"paid,omitempty" json:"paid,omitempty"`
}

type patch62Flags struct {
	DevMode          bool `json:"devmode,omitempty"`
	JailMode         bool `json:"jailmode,omitempty"`
	Classic          bool `json:"classic,omitempty"`
	TryMode          bool `json:"trymode,omitempty"`
	Revert           bool `json:"revert,omitempty"`
	RemoveSnapPath   bool `json:"remove-snap-path,omitempty"`
	IgnoreValidation bool `json:"ignore-validation,omitempty"`
	Required         bool `json:"required,omitempty"`
	SkipConfigure    bool `json:"skip-configure,omitempty"`
	Unaliased        bool `json:"unaliased,omitempty"`
	Amend            bool `json:"amend,omitempty"`
	IsAutoRefresh    bool `json:"is-auto-refresh,omitempty"`
	NoReRefresh      bool `json:"no-rerefresh,omitempty"`
	RequireTypeBase  bool `json:"require-base-type,omitempty"`
}

type patch62SnapState struct {
	SnapType string             `json:"type"`
	Sequence []*patch62SideInfo `json:"sequence"`
	Active   bool               `json:"active,omitempty"`
	Current  snap.Revision      `json:"current"`
	Channel  string             `json:"channel,omitempty"`
	patch62Flags
	Aliases              interface{} `json:"aliases,omitempty"`
	AutoAliasesDisabled  bool        `json:"auto-aliases-disabled,omitempty"`
	AliasesPending       bool        `json:"aliases-pending,omitempty"`
	UserID               int         `json:"user-id,omitempty"`
	InstanceKey          string      `json:"instance-key,omitempty"`
	CohortKey            string      `json:"cohort-key,omitempty"`
	RefreshInhibitedTime *time.Time  `json:"refresh-inhibited-time,omitempty"`
}

type patch62SnapSetup struct {
	Channel   string    `json:"channel,omitempty"`
	UserID    int       `json:"user-id,omitempty"`
	Base      string    `json:"base,omitempty"`
	Type      snap.Type `json:"type,omitempty"`
	PlugsOnly bool      `json:"plugs-only,omitempty"`
	CohortKey string    `json:"cohort-key,omitempty"`
	Prereq    []string  `json:"prereq,omitempty"`
	patch62Flags
	SnapPath     string           `json:"snap-path,omitempty"`
	DownloadInfo interface{}      `json:"download-info,omitempty"`
	SideInfo     *patch62SideInfo `json:"side-info,omitempty"`
	patch62auxStoreInfo
	InstanceKey string `json:"instance-key,omitempty"`
}

type patch62auxStoreInfo struct {
	Media interface{} `json:"media,omitempty"`
}

func hasSnapdSnapID(snapst patch62SnapState) bool {
	for _, seq := range snapst.Sequence {
		if snap.IsSnapd(seq.SnapID) {
			return true
		}
	}
	return false
}

// patch6_2:
//   - ensure snapd snaps in the snapstate have TypeSnapd for backward compatibility with old snapd snap releases.
//   - ensure snapd snaps have TypeSnapd in pending install tasks.
func patch6_2(st *state.State) error {
	var snaps map[string]*json.RawMessage
	if mylog.Check(st.Get("snaps", &snaps)); err != nil && !errors.Is(err, state.ErrNoState) {
		return fmt.Errorf("internal error: cannot get snaps: %s", err)
	}

	var hasSnapdSnap bool
	// check if we have snapd snap with TypeSnapd already in state, in such case
	// we shouldn't try to migrate any other snaps because we can have at most
	// one snapd snap.
	for _, raw := range snaps {
		var snapst patch62SnapState
		mylog.Check(json.Unmarshal([]byte(*raw), &snapst))

		if hasSnapdSnapID(snapst) && snapst.SnapType == string(snap.TypeSnapd) {
			hasSnapdSnap = true
			break
		}
	}

	// Migrate snapstate unless we have a snapd snap with TypeSnapd already set.
	if !hasSnapdSnap {
		for name, raw := range snaps {
			var snapst patch62SnapState
			mylog.Check(json.Unmarshal([]byte(*raw), &snapst))

			if hasSnapdSnapID(snapst) {
				snapst.SnapType = string(snap.TypeSnapd)
				data := mylog.Check2(json.Marshal(snapst))

				newRaw := json.RawMessage(data)
				snaps[name] = &newRaw
				st.Set("snaps", snaps)
				// We can have at most one snapd snap
				break
			}
		}
	}

	// migrate tasks' snap setup
	for _, task := range st.Tasks() {
		chg := task.Change()
		if chg != nil && chg.Status().Ready() {
			continue
		}

		var snapsup patch62SnapSetup
		mylog.Check(task.Get("snap-setup", &snapsup))
		if err != nil && !errors.Is(err, state.ErrNoState) {
			return fmt.Errorf("internal error: cannot get snap-setup of task %s: %s", task.ID(), err)
		}

		if err == nil && snapsup.SideInfo != nil {
			if snapsup.Type != snap.TypeSnapd && snap.IsSnapd(snapsup.SideInfo.SnapID) {
				snapsup.Type = snap.TypeSnapd
				task.Set("snap-setup", snapsup)
			}
		}
	}
	return nil
}
