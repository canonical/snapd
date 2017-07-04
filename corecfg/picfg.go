// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package corecfg

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// valid pi config keys
var piConfigKeys = map[string]bool{
	"disable_overscan":         true,
	"framebuffer_width":        true,
	"framebuffer_height":       true,
	"framebuffer_depth":        true,
	"framebuffer_ignore_alpha": true,
	"overscan_left":            true,
	"overscan_right":           true,
	"overscan_top":             true,
	"overscan_bottom":          true,
	"overscan_scale":           true,
	"display_rotate":           true,
	"hdmi_group":               true,
	"hdmi_mode":                true,
	"hdmi_drive":               true,
	"avoid_warnings":           true,
	"gpu_mem_256":              true,
	"gpu_mem_512":              true,
	"gpu_mem":                  true,
	"sdtv_aspect":              true,
	"config_hdmi_boost":        true,
	"hdmi_force_hotplug":       true,
}

func updatePiConfig(path string, config map[string]string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	// build regexp
	cfgKeys := make([]string, len(config))
	i := 0
	for k := range config {
		cfgKeys[i] = k
		i++
	}
	reStr := fmt.Sprintf(`^[ \t]*?(?P<is_comment>#?)[ \t#]*?(?P<key>%s)=(?P<old_value>.*)$`, strings.Join(cfgKeys, "|"))
	rx := regexp.MustCompile(reStr)

	// now go over the content
	found := map[string]bool{}
	needsWrite := false
	var toWrite []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		matches := rx.FindStringSubmatch(line)
		if len(matches) > 0 {
			wasComment := (matches[1] == "#")
			key := matches[2]
			oldValue := matches[3]
			found[key] = true
			if config[key] != "" {
				if wasComment || oldValue != config[key] {
					line = fmt.Sprintf("%s=%s", key, config[key])
					needsWrite = true
				}
			} else {
				line = fmt.Sprintf("#%s=%s", key, oldValue)
				if !wasComment {
					needsWrite = true
				}

			}
		}
		toWrite = append(toWrite, line)
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	// write anything that is missing
	for key := range config {
		if !found[key] && config[key] != "" {
			needsWrite = true
			toWrite = append(toWrite, fmt.Sprintf("%s=%s", key, config[key]))
		}
	}

	if needsWrite {
		s := strings.Join(toWrite, "\n")
		return osutil.AtomicWriteFile(path, []byte(s), 0644, 0)
	}

	return nil
}
