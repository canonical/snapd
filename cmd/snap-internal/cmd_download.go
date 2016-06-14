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

package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
)

type cmdDownload struct {
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`

	TargetDir string `long:"targetdir"`
	Channel   string `long:"channel"`
	Series    string `long:"series"`
	StoreID   string `long:"store-id"`
}

func (x *cmdDownload) downloadSnapWithSideInfo() (string, error) {
	if x.Series != "" {
		release.Series = x.Series
	}

	pwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	targetDir := x.TargetDir
	if targetDir == "" {
		targetDir = pwd
	}

	m := store.NewUbuntuStoreSnapRepository(nil, x.StoreID)
	snap, err := m.Snap(x.Positional.Snap, x.Channel, nil)
	if err != nil {
		return "", fmt.Errorf("failed to find snap: %s", err)
	}
	pb := progress.NewTextProgress()
	tmpName, err := m.Download(snap, pb, nil)
	if err != nil {
		return "", err
	}
	baseName := filepath.Base(snap.MountFile())

	path := filepath.Join(targetDir, baseName)
	if err := os.Rename(tmpName, path); err != nil {
		return "", err
	}

	out, err := json.Marshal(snap)
	if err != nil {
		return "", err
	}
	if err := ioutil.WriteFile(path+".sideinfo", []byte(out), 0644); err != nil {
		return "", err
	}

	return path, nil
}

func (x *cmdDownload) Execute([]string) error {
	path, err := x.downloadSnapWithSideInfo()
	if err != nil {
		return err
	}
	fmt.Printf("Downloaded snap to %q and %q\n", path, path+".sideinfo")

	return nil
}
