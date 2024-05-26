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

package configcore

import (
	"bufio"
	"fmt"
	"io"
	"regexp"

	"github.com/ddkwork/golibrary/mylog"
)

// first match is if it is comment, second is key, third value
var rx = regexp.MustCompile(`^[ \t]*(#?)[ \t#]*([a-z0-9_]+)=(.*)$`)

// updateKeyValueStream updates simple key=value files with comments.
// Example for such formats are: /etc/environment or /boot/uboot/config.txt
//
// An r io.Reader, map of supported config keys and a configuration
// "patch" is taken as input, the r is read line-by-line and any line
// and any required configuration change from the "config" input is
// applied.
//
// If changes need to be written a []string
// that contains the full file is returned. On error an error is returned.
func updateKeyValueStream(r io.Reader, supportedConfigKeys map[string]bool, newConfig map[string]string) (toWrite []string, err error) {
	cfgKeys := make([]string, len(newConfig))
	i := 0
	for k := range newConfig {
		if !supportedConfigKeys[k] {
			return nil, fmt.Errorf("cannot set unsupported configuration value %q", k)
		}
		cfgKeys[i] = k
		i++
	}

	// now go over the content
	found := map[string]bool{}
	needsWrite := false

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		matches := rx.FindStringSubmatch(line)
		if len(matches) > 0 && supportedConfigKeys[matches[2]] {
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
	mylog.Check(scanner.Err())

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

type filterFunc func(values, filtered map[string]interface{})

func applyFilters(flagFilter func(f flags) filterFunc, values map[string]interface{}) (map[string]interface{}, []configHandler) {
	// filter the values to only keep values that use the preinstall flag
	filteredValues := make(map[string]interface{})

	// find the handlers which have the specified flag set
	var filteredHandlers []configHandler
	for _, h := range handlers {
		filter := flagFilter(h.flags())
		if filter != nil {
			// check to see if the length of the filtered values goes up after
			// running the filter, if it does then we added a value and should
			// return it
			currentPreInstallConfigNum := len(filteredValues)
			filter(values, filteredValues)
			if len(filteredValues) > currentPreInstallConfigNum {
				filteredHandlers = append(filteredHandlers, h)
			}
		}
	}

	return filteredValues, filteredHandlers
}
