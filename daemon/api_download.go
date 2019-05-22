// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/store"
)

var snapDownloadCmd = &Command{
	Path:     "/v2/download",
	UserOK:   true,
	PolkitOK: "io.snapcraft.snapd.manage",
	POST:     postSnapDownload,
}

// snapDownloadAction is used to request a snap download
type snapDownloadAction struct {
	Action string   `json:"action"`
	Snaps  []string `json:"snaps,omitempty"`
}

// See: https://forum.snapcraft.io/t/downloading-snaps-via-snapd/11449
func postSnapDownload(c *Command, r *http.Request, user *auth.UserState) Response {

	fmt.Println(">>> postSnapDownload()")

	var action snapDownloadAction
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&action); err != nil {
		return BadRequest("cannot decode request body into download operation: %v", err)
	}
	if decoder.More() {
		return BadRequest("extra content found after download operation")
	}

	if len(action.Snaps) == 0 {
		return BadRequest("download operation requires at least one snap name")
	}

	if action.Action == "" {
		return BadRequest("download operation requires action")
	}

	// st := c.d.overlord.State()
	// st.Lock()
	// defer st.Unlock()

	switch action.Action {
	case "download":
		fmt.Printf(">>> postSnapDownload(): download %v snap\n", action.Snaps)
	default:
		return BadRequest("unknown download operation %q", action.Action)
	}

	name := action.Snaps[0]
	theStore := getStore(c)

	info, err := theStore.SnapInfo(store.SnapSpec{Name: name}, user)
	if err != nil {
		return SnapNotFound(name, err)
	}
	fmt.Printf(">>> postSnapDownload(): found snap %#v", info.DownloadInfo)

	downloadInfo := info.DownloadInfo
	url := downloadInfo.AnonDownloadURL
	if url == "" {
		url = downloadInfo.DownloadURL
	}
	w := NewMemoryStream()
	// defer w.Close()

	pbar := progress.MakeProgressBar()
	defer pbar.Finished()

	// func DownloadImpl(ctx context.Context, name, sha3_384, downloadURL string, user *auth.UserState, s *Store, w io.ReadWriteSeeker, resume int64, pbar progress.Meter, dlOpts *DownloadOptions) error {
	store.DownloadImpl(context.TODO(), name, downloadInfo.Sha3_384, url, user, theStore.(*store.Store), w, 0, pbar, nil)

	return FileStream(w.Bytes())
}

type MemoryStream struct {
	buff []byte
	loc  int
}

// DefaultCapacity is the size in bytes of a new MemoryStream's backing buffer
const DefaultCapacity = 512

// New creates a new MemoryStream instance
func NewMemoryStream() *MemoryStream {
	return NewCapacity(DefaultCapacity)
}

// NewCapacity starts the returned MemoryStream with the given capacity
func NewCapacity(cap int) *MemoryStream {
	return &MemoryStream{buff: make([]byte, 0, DefaultCapacity), loc: 0}
}

// Seek sets the offset for the next Read or Write to offset, interpreted
// according to whence: 0 means relative to the origin of the file, 1 means
// relative to the current offset, and 2 means relative to the end. Seek
// returns the new offset and an error, if any.
//
// Seeking to a negative offset is an error. Seeking to any positive offset is
// legal. If the location is beyond the end of the current length, the position
// will be placed at length.
func (m *MemoryStream) Seek(offset int64, whence int) (int64, error) {
	newLoc := m.loc
	switch whence {
	case 0:
		newLoc = int(offset)
	case 1:
		newLoc += int(offset)
	case 2:
		newLoc = len(m.buff) - int(offset)
	}

	if newLoc < 0 {
		return int64(m.loc), errors.New("Unable to seek to a location <0")
	}

	if newLoc > len(m.buff) {
		newLoc = len(m.buff)
	}

	m.loc = newLoc

	return int64(m.loc), nil
}

// Read puts up to len(p) bytes into p. Will return the number of bytes read.
func (m *MemoryStream) Read(p []byte) (n int, err error) {
	n = copy(p, m.buff[m.loc:len(m.buff)])
	m.loc += n

	if m.loc == len(m.buff) {
		return n, io.EOF
	}

	return n, nil
}

// Write writes the given bytes into the memory stream. If needed, the underlying
// buffer will be expanded to fit the new bytes.
func (m *MemoryStream) Write(p []byte) (n int, err error) {
	// Do we have space?
	if available := cap(m.buff) - m.loc; available < len(p) {
		// How much should we expand by?
		addCap := cap(m.buff)
		if addCap < len(p) {
			addCap = len(p)
		}

		newBuff := make([]byte, len(m.buff), cap(m.buff)+addCap)

		copy(newBuff, m.buff)

		m.buff = newBuff
	}

	// Write
	n = copy(m.buff[m.loc:cap(m.buff)], p)
	m.loc += n
	if len(m.buff) < m.loc {
		m.buff = m.buff[:m.loc]
	}

	return n, nil
}

// Bytes returns a copy of ALL valid bytes in the stream, regardless of the current
// position.
func (m *MemoryStream) Bytes() []byte {
	b := make([]byte, len(m.buff))
	copy(b, m.buff)
	return b
}

// Rewind returns the stream to the beginning
func (m *MemoryStream) Rewind() (int64, error) {
	return m.Seek(0, 0)
}
