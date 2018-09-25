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
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func init() {
	for _, flag := range experimentalFlags {
		supportedConfigurations["core.experimental."+flag] = true
	}
}

var experimentalFlags = []string{"hotplug", "layouts", "parallel-instances", "snapd-snap"}

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
	var buf bytes.Buffer
	for _, flag := range experimentalFlags {
		value, err := coreCfg(tr, "experimental."+flag)
		if err != nil {
			return err
		}
		if value == "true" {
			fmt.Fprintf(&buf, "%s=%s\n", strings.TrimPrefix(flag, "core.experimental."), value)
		}
	}

	if !osutil.IsDirectory(dirs.FactsDir) {
		if err := os.MkdirAll(dirs.FactsDir, 0755); err != nil {
			return err
		}
	}

	return osutil.AtomicWriteFile(filepath.Join(dirs.FactsDir, "experimental"), buf.Bytes(), 0644, 0)
}
