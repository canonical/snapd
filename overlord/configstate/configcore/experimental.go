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

package configcore

import (
	"os"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func init() {
	for _, flag := range experimentalFlags {
		supportedConfigurations["core.experimental."+flag] = true
	}
}

var experimentalFlags = []string{"hotplug", "layouts", "parallel-instances", "snapd-snap", "per-user-mount-namespace"}

func validateExperimentalSettings(tr Conf) error {
	for k := range supportedConfigurations {
		if !strings.HasPrefix(k, "core.experimental.") {
			continue
		}
		if err := validateBoolFlag(tr, strings.TrimPrefix(k, "core.")); err != nil {
			return err
		}
	}
	return nil
}

func handleExperimentalFlags(tr Conf) error {
	dir := dirs.FeaturesDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	content := make(map[string]*osutil.FileState, len(experimentalFlags))
	for _, flag := range experimentalFlags {
		value, err := coreCfg(tr, "experimental."+flag)
		if err != nil {
			return err
		}
		if value == "true" {
			content[flag] = &osutil.FileState{Mode: 0644}
		}
	}
	_, _, err := osutil.EnsureDirState(dir, "*", content)
	return err
}
