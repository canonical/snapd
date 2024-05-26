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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

var osOpen = os.Open

// SnapshotOptions describes the options available for snapshots.
// The initial source of these options is a file in the snap package.
// In addition, options can be modified with dynamic requests via REST API.
type SnapshotOptions struct {
	// Exclude is the list of file and directory patterns that need to be
	// excluded from a snapshot. At the moment the only supported globbing
	// character is "*", which stands for any sequence of characters other than
	// "/".
	Exclude []string `yaml:"exclude" json:"exclude,omitempty"`
}

const (
	snapshotManifestPath = "meta/snapshots.yaml"
)

// Unset determines if the SnapshotOptions object contains meaningful values.
//
// It can be used, for example, to determine if the SnapshotOptions object should be
// serialized to metadata.
func (opts *SnapshotOptions) Unset() bool {
	return len(opts.Exclude) == 0
}

// MergeDynamicExcludes combines dynamic excludes with existing excludes.
func (opts *SnapshotOptions) MergeDynamicExcludes(dynamicExcludes []string) error {
	mergedExcludes := append(opts.Exclude, dynamicExcludes...)
	dryRunOptions := SnapshotOptions{Exclude: mergedExcludes}
	mylog.Check(dryRunOptions.Validate())

	opts.Exclude = mergedExcludes

	return nil
}

// Validate checks the validity of all snapshot options.
func (opts *SnapshotOptions) Validate() error {
	// Validate the exclude list; note that this is an *exclusion* list, so
	// even if the manifest specified paths starting with ../ this would not
	// cause tar to navigate into those directories and pose a security risk.
	// Still, let's have a minimal validation on them being sensible.
	validFirstComponents := []string{
		"$SNAP_DATA", "$SNAP_COMMON", "$SNAP_USER_DATA", "$SNAP_USER_COMMON",
	}
	const invalidChars = "[]{}?"
	for _, excludePath := range opts.Exclude {
		firstComponent := strings.SplitN(excludePath, "/", 2)[0]
		if !strutil.ListContains(validFirstComponents, firstComponent) {
			return fmt.Errorf("snapshot exclude path must start with one of %q (got: %q)", validFirstComponents, excludePath)
		}

		cleanPath := filepath.Clean(excludePath)
		if cleanPath != excludePath {
			return fmt.Errorf("snapshot exclude path not clean: %q", excludePath)
		}

		// We could use a regexp to do this validation, but an explicit check
		// is more readable and less error-prone
		if strings.ContainsAny(excludePath, invalidChars) || strings.Contains(excludePath, "**") {
			return fmt.Errorf("snapshot exclude path contains invalid characters: %q", excludePath)
		}
	}

	return nil
}

// ReadSnapshotYaml reads the snapshot manifest file for the given snap.
func ReadSnapshotYaml(si *Info) (*SnapshotOptions, error) {
	file := mylog.Check2(osOpen(filepath.Join(si.MountDir(), snapshotManifestPath)))
	if os.IsNotExist(err) {
		return &SnapshotOptions{}, nil
	}

	defer file.Close()

	return readSnapshotYaml(file)
}

// ReadSnapshotYaml reads the snapshot manifest file for the given snap
// container.
func ReadSnapshotYamlFromSnapFile(snapf Container) (*SnapshotOptions, error) {
	sy := mylog.Check2(snapf.ReadFile(snapshotManifestPath))
	if os.IsNotExist(err) {
		return &SnapshotOptions{}, nil
	}

	return readSnapshotYaml(bytes.NewBuffer(sy))
}

func readSnapshotYaml(r io.Reader) (*SnapshotOptions, error) {
	var opts SnapshotOptions
	mylog.Check(yaml.NewDecoder(r).Decode(&opts))
	mylog.Check(opts.Validate())

	return &opts, nil
}
