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

package patch

import (
	"io/ioutil"
	"path/filepath"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
)

func init() {
	patches[1] = patch1
}

type patch1SideInfo struct {
	OfficialName      string        `yaml:"name,omitempty" json:"name,omitempty"`
	SnapID            string        `yaml:"snap-id" json:"snap-id"`
	Revision          snap.Revision `yaml:"revision" json:"revision"`
	Channel           string        `yaml:"channel,omitempty" json:"channel,omitempty"`
	Developer         string        `yaml:"developer,omitempty" json:"developer,omitempty"`
	EditedSummary     string        `yaml:"summary,omitempty" json:"summary,omitempty"`
	EditedDescription string        `yaml:"description,omitempty" json:"description,omitempty"`
	Size              int64         `yaml:"size,omitempty" json:"size,omitempty"`
	Sha512            string        `yaml:"sha512,omitempty" json:"sha512,omitempty"`
	Private           bool          `yaml:"private,omitempty" json:"private,omitempty"`
}

var patch1ReadType = func(name string, rev snap.Revision) (snap.Type, error) {
	snapYamlFn := filepath.Join(snap.MountDir(name, rev), "meta", "snap.yaml")
	meta, err := ioutil.ReadFile(snapYamlFn)
	if err != nil {
		return snap.TypeApp, err
	}
	info, err := snap.InfoFromSnapYaml(meta)
	if err != nil {
		return snap.TypeApp, err
	}

	return info.Type, nil
}

type patch1Flags int

const (
	// DevMode switches confinement to non-enforcing mode.
	patch1DevMode = 1 << iota
	// TryMode is set for snaps installed to try directly from a local directory.
	patch1TryMode
)

type patch1SnapSetup struct {
	Name     string        `json:"name,omitempty"`
	Revision snap.Revision `json:"revision,omitempty"`
	Channel  string        `json:"channel,omitempty"`
	UserID   int           `json:"user-id,omitempty"`

	Flags patch1Flags `json:"flags,omitempty"`

	SnapPath string `json:"snap-path,omitempty"`
}

type patch1SnapState struct {
	SnapType  string            `json:"type"`
	Sequence  []*patch1SideInfo `json:"sequence"`
	Current   snap.Revision     `json:"current"`
	Candidate *patch1SideInfo   `json:"candidate,omitempty"`
	Active    bool              `json:"active,omitempty"`
	Channel   string            `json:"channel,omitempty"`
	Flags     patch1Flags       `json:"flags,omitempty"`
	// incremented revision used for local installs
	LocalRevision snap.Revision `json:"local-revision,omitempty"`
}

// patch1 adds the snap type and the current revision to the snap state.
func patch1(s *state.State) error {
	var stateMap map[string]*patch1SnapState

	err := s.Get("snaps", &stateMap)
	if err == state.ErrNoState {
		return nil
	}
	if err != nil {
		return err
	}

	for snapName, snapst := range stateMap {
		seq := snapst.Sequence
		if len(seq) == 0 {
			continue
		}
		snapst.Current = seq[len(seq)-1].Revision
		typ, err := patch1ReadType(snapName, snapst.Current)
		if err != nil {
			logger.Noticef("Recording type for snap %q: cannot retrieve info, assuming it's a app: %v", snapName, err)
		} else {
			logger.Noticef("Recording type for snap %q: setting to %q", snapName, typ)
		}
		snapst.SnapType = string(typ)
	}

	s.Set("snaps", stateMap)
	return nil
}
