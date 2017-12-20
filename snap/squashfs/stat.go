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

package squashfs

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type SnapFileOwner struct {
	UID uint32
	GID uint32
}

type stat struct {
	path  string
	size  int64
	mode  os.FileMode
	mtime time.Time
	user  string
	group string
}

func (s stat) Name() string       { return filepath.Base(s.path) }
func (s stat) Size() int64        { return s.size }
func (s stat) Mode() os.FileMode  { return s.mode }
func (s stat) ModTime() time.Time { return s.mtime }
func (s stat) IsDir() bool        { return s.mode.IsDir() }
func (s stat) Sys() interface{}   { return nil }
func (s stat) Path() string       { return s.path } // not path of os.FileInfo

const minLen = 57 // "drwxrwxr-x user/user             53595 2017-12-08 11:19 ."

func fromRaw(raw []byte) (*stat, error) {
	if len(raw) < minLen {
		return nil, errBadLine(raw)
	}

	st := &stat{}

	// first, the file mode, e.g. "-rwxr-xr-x"; always that length (1+3*3==10)
	if err := st.parseMode(raw[:10]); err != nil {
		return nil, err
	}

	// next, user/group info
	p := 10
	if n, err := st.parseOwner(raw[p:]); err != nil {
		return nil, err
	} else {
		p += n
	}

	// next'll come the size or the node type
	if n, err := st.parseSize(raw[p:]); err != nil {
		return nil, err
	} else {
		p += n
	}

	if err := st.parseTime(raw[p : p+16]); err != nil {
		return nil, err
	}

	p += 16

	if raw[p] != ' ' {
		return nil, errBadLine(raw)
	}
	p++
	if raw[p] != '.' {
		return nil, errBadLine(raw)
	}
	p++
	if len(raw[p:]) == 0 {
		st.path = "/"
	} else {
		st.path = string(raw[p:])
	}

	return st, nil
}

type statError struct {
	part string
	raw  []byte
}

func (e statError) Error() string {
	return fmt.Sprintf("cannot parse %s: %q", e.part, e.raw)
}

func errBadLine(raw []byte) statError {
	return statError{
		part: "line",
		raw:  raw,
	}
}

func errBadMode(raw []byte) statError {
	return statError{
		part: "mode",
		raw:  raw,
	}
}

func errBadOwner(raw []byte) statError {
	return statError{
		part: "owner",
		raw:  raw,
	}
}

func errBadNode(raw []byte) statError {
	return statError{
		part: "node",
		raw:  raw,
	}
}

func errBadSize(raw []byte) statError {
	return statError{
		part: "size",
		raw:  raw,
	}
}

func (st *stat) parseTime(raw []byte) error {
	t, err := time.ParseInLocation("2006-01-02 15:04", string(raw), time.Local)
	if err != nil {
		return err
	}

	st.mtime = t

	return nil
}

func (st *stat) parseMode(raw []byte) error {
	switch raw[0] {
	case '-':
		// 0
	case 'd':
		st.mode |= os.ModeDir
	case 's':
		st.mode |= os.ModeSocket
	case 'c':
		st.mode |= os.ModeCharDevice
	case 'b':
		st.mode |= os.ModeDevice
	case 'p':
		st.mode |= os.ModeNamedPipe
	case 'l':
		st.mode |= os.ModeSymlink
	default:
		return errBadMode(raw)
	}

	for i := 0; i < 3; i++ {
		m, me := modeFromTriplet(raw[1+3*i:4+3*i], uint(2-i))
		if me != nil {
			return me
		}
		st.mode |= m
	}

	return nil
}

func (st *stat) parseOwner(raw []byte) (int, error) {
	var p, ui, uj, gi, gj int

	// first check it's sane (starts with space, and then at least two non-space chars
	if raw[0] != ' ' || raw[1] == ' ' || raw[2] == ' ' {
		return 0, errBadLine(raw)
	}

	ui = 1
out:
	for p = ui; p < 40; p++ {
		switch raw[p] {
		case '/':
			uj = p
			gi = p + 1
		case ' ':
			gj = p
			break out
		}
	}

	if uj == gj || gi >= p {
		return 0, errBadOwner(raw)
	}
	st.user, st.group = string(raw[ui:uj]), string(raw[gi:gj])

	return p, nil
}

func modeFromTriplet(trip []byte, shift uint) (os.FileMode, error) {
	var mode os.FileMode
	high := false
	if len(trip) != 3 {
		panic("bad triplet length")
	}
	switch trip[0] {
	case '-':
		// 0
	case 'r':
		mode |= 4
	default:
		return 0, errBadMode(trip)
	}
	switch trip[1] {
	case '-':
		// 0
	case 'w':
		mode |= 2
	default:
		return 0, errBadMode(trip)
	}
	switch trip[2] {
	case '-':
		// 0
	case 'x':
		mode |= 1
	case 'S', 'T':
		high = true
	case 's', 't':
		mode |= 1
		high = true
	default:
		return 0, errBadMode(trip)
	}

	mode <<= 3 * shift
	if high {
		mode |= (01000 << shift)
	}
	return mode, nil
}

func (st *stat) parseSize(raw []byte) (int, error) {
	isNode := st.mode&(os.ModeDevice|os.ModeCharDevice) != 0
	p := 0
	maxP := len(raw) - len("2006-01-02 15:04 .")

	for raw[p] == ' ' {
		if p >= maxP {
			return 0, errBadLine(raw)
		}
		p++
	}

	ni := p

	for raw[p] >= '0' && raw[p] <= '9' {
		if p >= minLen {
			return 0, errBadLine(raw)
		}
		p++
	}

	if p == ni {
		if isNode {
			return 0, errBadNode(raw)
		}
		return 0, errBadSize(raw)
	}

	if isNode {
		if raw[p] != ',' {
			return 0, errBadNode(raw)
		}

		p++

		// drop the space before the minor mode
		for raw[p] == ' ' {
			p++
		}
		// drop the minor mode
		for raw[p] >= '0' && raw[p] <= '9' {
			p++
		}

		if raw[p] != ' ' {
			return 0, errBadNode(raw)
		}
		p++
	} else {
		if raw[p] != ' ' {
			return 0, errBadSize(raw)
		}
		// note that, much as it makes very little sense, the arch-
		// dependent st_size is never an unsigned 64 bit quantity.
		// It's one of unsigned long, long long, or just off_t.
		//
		// Also note os.FileInfo's Size needs to return an int64, and
		// squashfs's inode->data (where it stores sizes for regular
		// files) is a long long.
		sz, err := strconv.ParseInt(string(raw[ni:p]), 10, 64)
		if err != nil {
			return 0, err
		}
		st.size = sz
		p++
	}

	return p, nil
}
