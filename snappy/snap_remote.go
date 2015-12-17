// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package snappy

import (
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v2"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/remote"
	"github.com/ubuntu-core/snappy/progress"
)

// RemoteSnapPart represents a snap available on the server
type RemoteSnapPart struct {
	pkg remote.Snap
}

// Type returns the type of the SnapPart (app, gadget, ...)
func (s *RemoteSnapPart) Type() pkg.Type {
	return s.pkg.Type
}

// Name returns the name
func (s *RemoteSnapPart) Name() string {
	return s.pkg.Name
}

// Version returns the version
func (s *RemoteSnapPart) Version() string {
	return s.pkg.Version
}

// Description returns the description
func (s *RemoteSnapPart) Description() string {
	return s.pkg.Title
}

// Origin is the origin
func (s *RemoteSnapPart) Origin() string {
	return s.pkg.Origin
}

// Hash returns the hash
func (s *RemoteSnapPart) Hash() string {
	return s.pkg.DownloadSha512
}

// Channel returns the channel used
func (s *RemoteSnapPart) Channel() string {
	return s.pkg.Channel
}

// Icon returns the icon
func (s *RemoteSnapPart) Icon() string {
	return s.pkg.IconURL
}

// IsActive returns true if the snap is active
func (s *RemoteSnapPart) IsActive() bool {
	return false
}

// IsInstalled returns true if the snap is installed
func (s *RemoteSnapPart) IsInstalled() bool {
	return false
}

// InstalledSize returns the size of the installed snap
func (s *RemoteSnapPart) InstalledSize() int64 {
	return -1
}

// DownloadSize returns the dowload size
func (s *RemoteSnapPart) DownloadSize() int64 {
	return s.pkg.DownloadSize
}

// Date returns the last update time
func (s *RemoteSnapPart) Date() time.Time {
	var p time.Time
	var err error

	for _, fmt := range []string{
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05.000000Z",
	} {
		p, err = time.Parse(fmt, s.pkg.LastUpdated)
		if err == nil {
			break
		}
	}

	return p
}

// download writes an http.Request showing a progress.Meter
func download(name string, w io.Writer, req *http.Request, pbar progress.Meter) error {
	client := &http.Client{}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return &ErrDownload{Code: resp.StatusCode, URL: req.URL}
	}

	if pbar != nil {
		pbar.Start(name, float64(resp.ContentLength))
		mw := io.MultiWriter(w, pbar)
		_, err = io.Copy(mw, resp.Body)
		pbar.Finished()
	} else {
		_, err = io.Copy(w, resp.Body)
	}

	return err
}

// Download downloads the snap and returns the filename
func (s *RemoteSnapPart) Download(pbar progress.Meter) (string, error) {
	w, err := ioutil.TempFile("", s.pkg.Name)
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			os.Remove(w.Name())
		}
	}()
	defer w.Close()

	// try anonymous download first and fallback to authenticated
	url := s.pkg.AnonDownloadURL
	if url == "" {
		url = s.pkg.DownloadURL
	}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	setUbuntuStoreHeaders(req)

	if err := download(s.Name(), w, req, pbar); err != nil {
		return "", err
	}

	return w.Name(), w.Sync()
}

func (s *RemoteSnapPart) saveStoreManifest() error {
	content, err := yaml.Marshal(s.pkg)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dirs.SnapMetaDir, 0755); err != nil {
		return err
	}

	// don't worry about previous contents
	return helpers.AtomicWriteFile(RemoteManifestPath(s), content, 0644, 0)
}

// Install installs the snap
func (s *RemoteSnapPart) Install(pbar progress.Meter, flags InstallFlags) (string, error) {
	downloadedSnap, err := s.Download(pbar)
	if err != nil {
		return "", err
	}
	defer os.Remove(downloadedSnap)

	if err := s.saveStoreManifest(); err != nil {
		return "", err
	}

	return installClick(downloadedSnap, flags, pbar, s.Origin())
}

// SetActive sets the snap active
func (s *RemoteSnapPart) SetActive(bool, progress.Meter) error {
	return ErrNotInstalled
}

// Uninstall remove the snap from the system
func (s *RemoteSnapPart) Uninstall(progress.Meter) error {
	return ErrNotInstalled
}

// Config is used to to configure the snap
func (s *RemoteSnapPart) Config(configuration []byte) (new string, err error) {
	return "", err
}

// NeedsReboot returns true if the snap becomes active on the next reboot
func (s *RemoteSnapPart) NeedsReboot() bool {
	return false
}

// Frameworks returns the list of frameworks needed by the snap
func (s *RemoteSnapPart) Frameworks() ([]string, error) {
	return nil, ErrNotImplemented
}
