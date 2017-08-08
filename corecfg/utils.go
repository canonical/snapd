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
	"io"
	"regexp"
)

// updateKeyValueStream updates simple key=value files with comments.
// Example for such formats are: /etc/environment or /boot/uboot/config.txt
//
// An r io.Reader and a configuration "patch" is taken as input, the r is
// read line-by-line and any line and any required configuration change from
// the "config" input is applied. If changes need to be written a []string
// that contains the full file is returned. On error an error is returned.
func updateKeyValueStream(r io.Reader, allConfig map[string]bool, newConfig map[string]string) (toWrite []string, err error) {
	// build regexp
	cfgKeys := make([]string, len(newConfig))
	i := 0
	for k := range newConfig {
		cfgKeys[i] = k
		i++
	}
	reStr := `^[ \t]*?(?P<is_comment>#?)[ \t#]*?(?P<key>[a-z_]+)=(?P<old_value>.*)$`
	rx := regexp.MustCompile(reStr)

	// now go over the content
	found := map[string]bool{}
	needsWrite := false

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		matches := rx.FindStringSubmatch(line)
		if len(matches) > 0 && allConfig[matches[2]] {
			wasComment := (matches[1] == "#")
			key := matches[2]
			oldValue := matches[3]
			found[key] = true
			if newConfig[key] != "" {
				if wasComment || oldValue != newConfig[key] {
					line = fmt.Sprintf("%s=%s", key, newConfig[key])
					needsWrite = true
				}
			} else {
				if !wasComment {
					line = fmt.Sprintf("#%s=%s", key, oldValue)
					needsWrite = true
				}
			}
		}
		toWrite = append(toWrite, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// write anything that is missing
	for key := range newConfig {
		if !found[key] && newConfig[key] != "" {
			needsWrite = true
			toWrite = append(toWrite, fmt.Sprintf("%s=%s", key, newConfig[key]))
		}
	}

	if needsWrite {
		return toWrite, nil
	}

	return nil, nil
}
