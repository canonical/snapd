// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package seedwriter

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
)

type policy16 struct {
	model *asserts.Model
	opts  *Options

	needsCore   []string
	needsCore16 []string
}

func (pol *policy16) systemSnap() *asserts.ModelSnap {
	if pol.model.Classic() {
		// no predefined system snap, infer later
		return nil
	}
	snapName := "core"
	snapType := "core"
	if pol.model.Base() != "" {
		snapName = "snapd"
		snapType = "snapd"
	}
	// TODO: set SnapID too
	return &asserts.ModelSnap{
		Name:           snapName,
		SnapType:       snapType,
		Modes:          []string{"run"},
		DefaultChannel: "stable",
		Presence:       "required",
	}
}

func (pol *policy16) checkBase(info *snap.Info, availableSnaps *naming.SnapSet) error {
	// Sanity check, note that we could support this case
	// if we have a use-case but it requires changes in the
	// devicestate/firstboot.go ordering code.
	if info.GetType() == snap.TypeGadget && !pol.model.Classic() && info.Base != pol.model.Base() {
		return fmt.Errorf("cannot use gadget snap because its base %q is different from model base %q", info.Base, pol.model.Base())
	}

	// snap needs no base (or it simply needs core which is never listed explicitly): nothing to do
	if info.Base == "" {
		if info.GetType() == snap.TypeGadget || info.GetType() == snap.TypeApp {
			// remember to make sure we have core installed
			pol.needsCore = append(pol.needsCore, info.SnapName())
		}
		return nil
	}

	// snap explicitly listed as not needing a base snap (e.g. a content-only snap)
	if info.Base == "none" {
		return nil
	}

	if availableSnaps.Contains(naming.Snap(info.Base)) {
		return nil
	}

	if info.Base == "core16" {
		// check at the end
		pol.needsCore16 = append(pol.needsCore16, info.SnapName())
		return nil
	}

	return fmt.Errorf("cannot add snap %q without also adding its base %q explicitly", info.SnapName(), info.Base)
}

type tree16 struct {
	opts *Options

	snapsDirPath string
}

func (tr *tree16) mkFixedDirs() error {
	tr.snapsDirPath = filepath.Join(tr.opts.SeedDir, "snaps")
	return os.MkdirAll(tr.snapsDirPath, 0755)
}

func (tr *tree16) snapsDir() string {
	return tr.snapsDirPath
}
