// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package asserts

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/*
findWildcard invokes foundCb for each regular file matching:

<top>/<descendantWithWildcard[0]>/<descendantWithWildcard[1]...

where each descendantWithWildcard component can contain the * wildcard;

foundCb is invoked with the path of the file relative to top (that means top/
 is excluded).

Unlike filepath.Glob any I/O operation error stops the walking and bottoms out, so does a foundCb invocation that returns an error.
*/
func findWildcard(top string, descendantWithWildcard []string, foundCb func(relpath string) error) error {
	return findWildcardDescend(top, top, descendantWithWildcard, foundCb)
}

func findWildcardDescend(top, current string, descendantWithWildcard []string, foundCb func(relpath string) error) error {
	if len(descendantWithWildcard) == 0 {
		finfo, err := os.Stat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !finfo.Mode().IsRegular() {
			return fmt.Errorf("expected a regular file: %v", current)
		}
		relpath, err := filepath.Rel(top, current)
		if err != nil {
			return fmt.Errorf("findWildcard: unexpected to fail at computing rel path of descendant")
		}
		return foundCb(relpath)
	}

	k := descendantWithWildcard[0]
	if strings.IndexByte(k, '*') == -1 {
		return findWildcardDescend(top, filepath.Join(current, k), descendantWithWildcard[1:], foundCb)
	}

	d, err := os.Open(current)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		ok, err := filepath.Match(k, name)
		if err != nil {
			return fmt.Errorf("findWildcard: invoked with malformed wildcard: %v", err)
		}
		if ok {
			err = findWildcardDescend(top, filepath.Join(current, name), descendantWithWildcard[1:], foundCb)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
