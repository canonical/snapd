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
	"fmt"
	"io"
	"net/http"

	"github.com/snapcore/snapd/overlord/auth"
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
	Action string `json:"action"`
	Snaps []snapDownloadInfo `json:"snaps,omitempty"`
}

type snapDownloadInfo struct {
	Name   string `json:"name"`
	Resume int64  `json:"resume"`
}

func postSnapDownload(c *Command, r *http.Request, user *auth.UserState) Response {
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

	if len(action.Snaps) != 1 {
		return BadRequest("download operation supports only one snap")
	}

	if action.Action == "" {
		return BadRequest("download operation requires action")
	}

	switch action.Action {
	case "download":
		theStore := getStore(c)
		snap := action.Snaps[0]
		return streamOne(snap, theStore.(*store.Store), user)
	default:
		return BadRequest("unknown download operation %q", action.Action)
	}
}

func streamOne(snap snapDownloadInfo, theStore *store.Store, user *auth.UserState) Response {
	info, err := theStore.SnapInfo(store.SnapSpec{Name: snap.Name}, user)
	if err != nil {
		return SnapNotFound(snap.Name, err)
	}

	downloadInfo := info.DownloadInfo
	memStream := NewMemoryStream()
	go func() {
		err := store.DownloadStream(context.TODO(), theStore, snap.Name, snap.Resume, &downloadInfo, user, memStream)
		if err != nil {
			memStream.PipeWriter.CloseWithError(err)
		}
	}()

	return FileStream{
		FileName: snap.Name,
		Info:     downloadInfo,
		stream:   memStream.PipeReader,
	}
}

// MemoryStream is a wrapper over io.Pipe with a empty Seek implementation
type MemoryStream struct {
	*io.PipeReader
	*io.PipeWriter
	FakeSeeker
}

func NewMemoryStream() *MemoryStream {
	pr, pw := io.Pipe()
	return &MemoryStream{pr, pw, FakeSeeker{}}
}

type FakeSeeker struct{}

func (f *FakeSeeker) Seek(offset int64, whence int) (int64, error) {
	return 0, fmt.Errorf("Seek is not implemented")
}
