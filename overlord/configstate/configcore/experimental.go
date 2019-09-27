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
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
)

func init() {
	for _, feature := range features.KnownFeatures() {
		snapName, confName := feature.ConfigOption()
		supportedConfigurations[snapName+"."+confName] = true
	}
}

func validateExperimentalSettings(tr config.Conf) error {
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

func ExportExperimentalFlags(tr config.Conf) error {
	dir := dirs.FeaturesDir
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	features := features.KnownFeatures()
	content := make(map[string]osutil.FileState, len(features))
	for _, feature := range features {
		if !feature.IsExported() {
			continue
		}
		isEnabled, err := config.GetFeatureFlag(tr, feature)
		if err != nil {
			return err
		}
		if isEnabled {
			content[feature.String()] = &osutil.MemoryFileState{Mode: 0644}
		}
	}
	_, _, err := osutil.EnsureDirState(dir, "*", content)
	return err
}
