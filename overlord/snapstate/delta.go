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
// Invalid stored entries are logged and skipped, falling back to xdelta3 if none
// remain or the option cannot be read. When absent, the default is
// xdelta3, preceded by snap-1-1-xdelta3 on amd64, ppc64el and suitable arm64 CPUs.
// ARM suitability is checked using each processor's implementer and part IDs.
// The caller must hold the state lock when using a config transaction.
func RefreshDeltaFormats(tr deltaFormatConfig) []string {
	var value any
	err := tr.Get("core", "refresh.delta-formats", &value)
	if err != nil && !config.IsNoOption(err) {
		logger.Noticef("cannot read refresh.delta-formats, using xdelta3: %v", err)
		return []string{"xdelta3"}
	}
	if err == nil && value != nil {
		entries, ok := value.([]any)
		if !ok {
			logger.Noticef("invalid refresh.delta-formats value %v: must be a list of delta formats, using xdelta3", value)
			return []string{"xdelta3"}
		}
		supported := squashfs.SupportedDeltaFormats(squashfs.DeltaFormatOpts{WithSnapDeltaFormat: true})
		var formats []string
		for _, entry := range entries {
			format, ok := entry.(string)
			if !ok || !strutil.ListContains(supported, format) {
				logger.Noticef("skipping unsupported delta format %v in refresh.delta-formats", entry)
				continue
			}
			if strutil.ListContains(formats, format) {
				logger.Noticef("skipping duplicate delta format %q in refresh.delta-formats", format)
				continue
			}
			formats = append(formats, format)
		}
		if len(formats) == 0 {
			logger.Noticef("refresh.delta-formats contains no supported delta formats, using xdelta3")
			return []string{"xdelta3"}
		}
		return formats
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
	return squashfs.SupportedDeltaFormats(squashfs.DeltaFormatOpts{WithSnapDeltaFormat: enabled})
}

func armCPUAllowsSnapDelta() (bool, error) {
	data, err := os.ReadFile(filepath.Join(dirs.GlobalRootDir, "/proc/cpuinfo"))
	if err != nil {
		return false, err
	}
	found := false
	hasProcessor, hasPart, hasImplementer := false, false, false
	var part, implementer uint64
	for _, line := range strings.Split(string(data)+"\n", "\n") {
		if strings.TrimSpace(line) == "" {
			if hasProcessor || hasPart || hasImplementer {
				if !hasPart {
					return false, fmt.Errorf("missing CPU part for a processor")
				}
				if !hasImplementer {
					return false, fmt.Errorf("missing CPU implementer for a processor")
				}
				// Cortex part IDs are scoped to Arm's implementer ID.
				if implementer == 0x41 {
					switch part {
					case 0xc05,
						0xc07,
						0xc08,
						0xd01,
						0xd02,
						0xd03,
						0xd04,
						0xd05,
						0xd46,
						0xd80,
						0xd8f:
						return false, nil
					}
				}
				found = true
			}
			hasProcessor, hasPart, hasImplementer = false, false, false
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "processor":
			hasProcessor = true
		case "cpu part", "cpu implementer":
			field, digits := "CPU part", 3
			if strings.EqualFold(strings.TrimSpace(key), "cpu implementer") {
				field, digits = "CPU implementer", 2
			}
			value = strings.TrimSpace(value)
			if len(value) != digits+2 || !strings.EqualFold(value[:2], "0x") {
				return false, fmt.Errorf("invalid %s %q", field, value)
			}
			id, err := strconv.ParseUint(value[2:], 16, digits*4)
			if err != nil {
				return false, fmt.Errorf("invalid %s %q: %w", field, value, err)
			}
			if field == "CPU part" {
				part, hasPart = id, true
			} else {
				implementer, hasImplementer = id, true
			}
		}
	}
	if !found {
		return false, fmt.Errorf("no CPU parts found")
	}
	return true, nil
}
