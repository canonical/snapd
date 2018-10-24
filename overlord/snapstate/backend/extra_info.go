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

package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

var (
	ErrExtraInfoExtraContent = errors.New("extra content found after extra info")
	ErrNoExtraInfo           = errors.New("extra info not found")
)

// ExtraInfo is information about a snap that is not in the snap.yaml,
// not needed in the state, but may be cached to augment the
// information returned for locally-installed snaps
type ExtraInfo struct {
	Media snap.MediaInfos `json:"media,omitempty"`
}

func extraFilename(snapName string) string {
	return filepath.Join(dirs.SnapExtraInfoDir, snapName) + ".json"
}

func LoadExtraInfo(info *snap.Info) error {
	if info.SnapID == "" {
		return nil
	}
	f, err := os.Open(extraFilename(info.InstanceName()))
	if err != nil {
		if os.IsNotExist(err) {
			return ErrNoExtraInfo
		}
		return err
	}
	defer f.Close()

	var extra ExtraInfo
	dec := json.NewDecoder(f)
	if err := dec.Decode(&extra); err != nil {
		return fmt.Errorf("unable to decode extra snap info: %v", err)
	}
	if dec.More() {
		return ErrExtraInfoExtraContent
	}

	info.Media = extra.Media

	return nil
}

func (Backend) SaveExtraInfo(snapName string, extra *ExtraInfo) error {
	if snapName == "" {
		return nil
	}
	if err := os.MkdirAll(dirs.SnapExtraInfoDir, 0755); err != nil {
		return fmt.Errorf("unable to create directory for extra snap info cache: %v", err)
	}

	af, err := osutil.NewAtomicFile(extraFilename(snapName), 0644, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return fmt.Errorf("unable to create file for extra snap info cache: %v", err)
	}
	// on success, Cancel becomes a nop
	defer af.Cancel()

	if err := json.NewEncoder(af).Encode(extra); err != nil {
		return fmt.Errorf("unable to encode extra info: %v", err)
	}

	if err := af.Commit(); err != nil {
		return fmt.Errorf("unable to commit extra snap info file: %v", err)
	}
	return nil
}

func (Backend) DeleteExtraInfo(snapName string) error {
	if snapName == "" {
		return nil
	}
	if err := os.Remove(extraFilename(snapName)); err != nil {
		if os.IsNotExist(err) {
			return ErrNoExtraInfo
		}
		return fmt.Errorf("unable to remove extra info file: %v", err)
	}
	return nil
}
