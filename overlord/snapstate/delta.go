// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package snapstate

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/strutil"
)

type deltaFormatConfig interface {
	Get(snapName, key string, result any) error
}

// RefreshDeltaFormats returns the ordered, non-empty refresh.delta-formats list.
// Explicit values must be supported by squashfs. When absent, the default is
// xdelta3, preceded by snap-1-1-xdelta3 on amd64, ppc64el and suitable arm64 CPUs.
// The caller must hold the state lock when using a config transaction.
func RefreshDeltaFormats(tr deltaFormatConfig) ([]string, error) {
	var formats []string
	err := tr.Get("core", "refresh.delta-formats", &formats)
	if err != nil && !config.IsNoOption(err) {
		return nil, err
	}
	if formats != nil {
		if len(formats) == 0 {
			return nil, fmt.Errorf("refresh.delta-formats must contain at least one delta format")
		}
		supported := squashfs.SupportedDeltaFormats(squashfs.DeltaFormatOpts{WithSnapDeltaFormat: true})
		for _, format := range formats {
			if !strutil.ListContains(supported, format) {
				return nil, fmt.Errorf("unsupported delta format %q in refresh.delta-formats", format)
			}
		}
		return formats, nil
	}

	enabled := false
	switch arch.DpkgArchitecture() {
	case "amd64", "ppc64el":
		enabled = true
	case "arm64":
		enabled, err = armCPUAllowsSnapDelta()
		if err != nil {
			logger.Noticef("cannot determine ARM CPU suitability for snap deltas, using xdelta3: %v", err)
		}
	}
	return squashfs.SupportedDeltaFormats(squashfs.DeltaFormatOpts{WithSnapDeltaFormat: enabled}), nil
}

func armCPUAllowsSnapDelta() (bool, error) {
	data, err := os.ReadFile(filepath.Join(dirs.GlobalRootDir, "/proc/cpuinfo"))
	if err != nil {
		return false, err
	}
	found := false
	hasProcessor, hasPart := false, false
	for _, line := range strings.Split(string(data)+"\n", "\n") {
		if strings.TrimSpace(line) == "" {
			if hasProcessor && !hasPart {
				return false, fmt.Errorf("missing CPU part for a processor")
			}
			hasProcessor, hasPart = false, false
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "processor":
			hasProcessor = true
		case "cpu part":
			value = strings.TrimSpace(value)
			if len(value) != 5 || !strings.EqualFold(value[:2], "0x") {
				return false, fmt.Errorf("invalid CPU part %q", value)
			}
			part, err := strconv.ParseUint(value[2:], 16, 12)
			if err != nil {
				return false, fmt.Errorf("invalid CPU part %q: %w", value, err)
			}
			hasPart, found = true, true
			// In-order Cortex A cores are unsuitable for streaming deltas.
			switch part {
			case 0xc05, 0xc07, 0xc08, 0xd01, 0xd03, 0xd04, 0xd05, 0xd46, 0xd80:
				return false, nil
			}
		}
	}
	if !found {
		return false, fmt.Errorf("no CPU parts found")
	}
	return true, nil
}
