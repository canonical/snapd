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
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

/*
findWildcard invokes foundCb once for each parent directory of regular files matching:

<top>/<descendantWithWildcard[0]>/<descendantWithWildcard[1]>...

where each descendantWithWildcard component can contain the * wildcard.

One of the descendantWithWildcard components except the last
can be "#>" or "#<", in which case that level is assumed to have names
that can be parsed as positive integers, which will be enumerated in
ascending (#>) or descending order respectively (#<); if seqnum != -1
then only the values >seqnum or respectively <seqnum will be
considered.

foundCb is invoked with the paths of the found regular files relative to top (that means top/ is excluded).

Unlike filepath.Glob any I/O operation error stops the walking and bottoms out, so does a foundCb invocation that returns an error.
*/
func findWildcard(top string, descendantWithWildcard []string, seqnum int, foundCb func(relpath []string) error) error {
	return findWildcardDescend(top, top, descendantWithWildcard, seqnum, foundCb)
}

func findWildcardBottom(top, current string, pat string, names []string, foundCb func(relpath []string) error) error {
	var hits []string
	for _, name := range names {
		ok := mylog.Check2(filepath.Match(pat, name))

		if !ok {
			continue
		}
		fn := filepath.Join(current, name)
		finfo := mylog.Check2(os.Stat(fn))
		if os.IsNotExist(err) {
			continue
		}

		if !finfo.Mode().IsRegular() {
			return fmt.Errorf("expected a regular file: %v", fn)
		}
		relpath := mylog.Check2(filepath.Rel(top, fn))

		hits = append(hits, relpath)
	}
	if len(hits) == 0 {
		return nil
	}
	return foundCb(hits)
}

func findWildcardDescend(top, current string, descendantWithWildcard []string, seqnum int, foundCb func(relpath []string) error) error {
	k := descendantWithWildcard[0]
	if k == "#>" || k == "#<" {
		if len(descendantWithWildcard) == 1 {
			return fmt.Errorf("findWildcard: sequence wildcard (#>|<#) cannot be the last component")
		}
		return findWildcardSequence(top, current, k, descendantWithWildcard[1:], seqnum, foundCb)
	}
	if len(descendantWithWildcard) > 1 && strings.IndexByte(k, '*') == -1 {
		return findWildcardDescend(top, filepath.Join(current, k), descendantWithWildcard[1:], seqnum, foundCb)
	}

	d := mylog.Check2(os.Open(current))
	// ignore missing directory, higher level will produce
	// NotFoundError as needed
	if os.IsNotExist(err) {
		return nil
	}

	defer d.Close()
	names := mylog.Check2(d.Readdirnames(-1))

	if len(descendantWithWildcard) == 1 {
		return findWildcardBottom(top, current, k, names, foundCb)
	}
	for _, name := range names {
		ok := mylog.Check2(filepath.Match(k, name))

		if ok {
			mylog.Check(findWildcardDescend(top, filepath.Join(current, name), descendantWithWildcard[1:], seqnum, foundCb))
		}
	}
	return nil
}

func findWildcardSequence(top, current, seqWildcard string, descendantWithWildcard []string, seqnum int, foundCb func(relpath []string) error) error {
	filter := func(i int) bool { return true }
	if seqnum != -1 {
		if seqWildcard == "#>" {
			filter = func(i int) bool { return i > seqnum }
		} else { // "#<", guaranteed by the caller
			filter = func(i int) bool { return i < seqnum }
		}
	}

	d := mylog.Check2(os.Open(current))
	// ignore missing directory, higher level will produce
	// NotFoundError as needed
	if os.IsNotExist(err) {
		return nil
	}

	defer d.Close()
	var seq []int
	for {
		names, err := d.Readdirnames(100)
		if err == io.EOF {
			break
		}

		for _, n := range names {
			sqn := mylog.Check2(strconv.Atoi(n))
			if err != nil || sqn < 0 || prefixZeros(n) {
				return fmt.Errorf("cannot parse %q name as a valid sequence number", filepath.Join(current, n))
			}
			if filter(sqn) {
				seq = append(seq, sqn)
			}
		}
	}
	sort.Ints(seq)

	var start, direction int
	if seqWildcard == "#>" {
		start = 0
		direction = 1
	} else {
		start = len(seq) - 1
		direction = -1
	}
	for i := start; i >= 0 && i < len(seq); i += direction {
		mylog.Check(findWildcardDescend(top, filepath.Join(current, strconv.Itoa(seq[i])), descendantWithWildcard, -1, foundCb))
	}
	return nil
}
