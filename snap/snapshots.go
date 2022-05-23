// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package snap

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/strutil"
)

var osOpen = os.Open

// SnapshotOptions contains the information provided by a snap package about
// what files should be included in the snapshots.
type SnapshotOptions struct {
	// ExcludePaths is the list of file and directory patterns that need to be
	// excluded from a snapshot. At the moment the only supported globbing
	// character is "*", which stands for any sequence of characters other than
	// "/".
	ExcludePaths []string `yaml:"exclude"`
}

const (
	snapshotManifestPath = "meta/snapshots.yaml"
)

// ReadSnapshotYaml reads the snapshot manifest file for the given snap.
func ReadSnapshotYaml(si *Info) (*SnapshotOptions, error) {
	file, err := osOpen(filepath.Join(si.MountDir(), snapshotManifestPath))
	if os.IsNotExist(err) {
		return &SnapshotOptions{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return readSnapshotYaml(file)
}

// ReadSnapshotYaml reads the snapshot manifest file for the given snap
// container.
func ReadSnapshotYamlFromSnapFile(snapf Container) (*SnapshotOptions, error) {
	sy, err := snapf.ReadFile(snapshotManifestPath)
	if os.IsNotExist(err) {
		return &SnapshotOptions{}, nil
	}
	if err != nil {
		return nil, err
	}
	return readSnapshotYaml(bytes.NewBuffer(sy))
}

func readSnapshotYaml(r io.Reader) (*SnapshotOptions, error) {
	var opts SnapshotOptions

	if err := yaml.NewDecoder(r).Decode(&opts); err != nil {
		return nil, fmt.Errorf("cannot read snapshot manifest: %v", err)
	}

	// Validate the exclude list; note that this is an *exclusion* list, so
	// even if the manifest specified paths starting with ../ this would not
	// cause tar to navigate into those directories and pose a security risk.
	// Still, let's have a minimal validation on them being sensible.
	validFirstComponents := []string{
		"$SNAP_DATA", "$SNAP_COMMON", "$SNAP_USER_DATA", "$SNAP_USER_COMMON",
	}
	const invalidChars = "[]{}?"

	for _, excludePath := range opts.ExcludePaths {
		firstComponent := strings.SplitN(excludePath, "/", 2)[0]
		if !strutil.ListContains(validFirstComponents, firstComponent) {
			return nil, fmt.Errorf("snapshot exclude path must start with one of %q (got: %q)", validFirstComponents, excludePath)
		}

		cleanPath := filepath.Clean(excludePath)
		if cleanPath != excludePath {
			return nil, fmt.Errorf("snapshot exclude path not clean: %q", excludePath)
		}

		// We could use a regexp to do this validation, but an explicit check
		// is more readable and less error-prone
		if strings.ContainsAny(excludePath, invalidChars) ||
			strings.Contains(excludePath, "**") {
			return nil, fmt.Errorf("snapshot exclude path contains invalid characters: %q", excludePath)
		}
	}

	return &opts, nil
}
