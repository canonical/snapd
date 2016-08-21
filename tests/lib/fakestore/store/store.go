// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package store

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/tylerb/graceful.v1"

	"github.com/snapcore/snapd/snap"
)

var (
	// FIXME: make both hardcoded values configurable via
	//        e.g. a "foo_1.0.snap.info" file next to the snap
	defaultDeveloper   = "canonical"
	defaultDeveloperID = "canonical"
	defaultRevision    = 424242
)

func rootEndpoint(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(418)
	fmt.Fprintf(w, "I'm a teapot")
}

// Store is our snappy software store implementation
type Store struct {
	url     string
	blobDir string

	srv *graceful.Server

	snaps map[string]string
}

// NewStore creates a new store server
func NewStore(blobDir, addr string) *Store {
	mux := http.NewServeMux()
	store := &Store{
		blobDir: blobDir,
		snaps:   make(map[string]string),

		url: fmt.Sprintf("http://%s", addr),
		srv: &graceful.Server{
			Timeout: 2 * time.Second,

			Server: &http.Server{
				Addr:    addr,
				Handler: mux,
			},
		},
	}

	mux.HandleFunc("/", rootEndpoint)
	mux.HandleFunc("/search", store.searchEndpoint)
	mux.HandleFunc("/snaps/details/", store.detailsEndpoint)
	mux.HandleFunc("/snaps/metadata", store.bulkEndpoint)
	mux.Handle("/download/", http.StripPrefix("/download/", http.FileServer(http.Dir(blobDir))))

	return store
}

// URL returns the base-url that the store is listening on
func (s *Store) URL() string {
	return s.url
}

func (s *Store) SnapsDir() string {
	return s.blobDir
}

// Start listening
func (s *Store) Start() error {
	l, err := net.Listen("tcp", s.srv.Addr)
	if err != nil {
		return err
	}

	go s.srv.Serve(l)
	return nil
}

// Stop stops the server
func (s *Store) Stop() error {
	timeoutTime := 2000 * time.Millisecond
	s.srv.Stop(timeoutTime / 2)

	select {
	case <-s.srv.StopChan():
	case <-time.After(timeoutTime):
		return fmt.Errorf("store failed to stop after %s", timeoutTime)
	}

	return nil
}

func makeRevision(info *snap.Info) int {
	// TODO: This is a hack to ensure we have higher
	//       revisions here than locally. The fake
	//       snaps get versions like
	//          "1.0+fake1+fake1+fake1"
	//       so we can use this for now to generate
	//       fake revisions. However in the longer
	//       term we should read the real revision
	//       of the snap, increment and add a ".aux"
	//       file to the download directory of the
	//       store that contains the revision and the
	//       developer. The fake-store can then read
	//       that file when sending the reply.
	n := strings.Count(info.Version, "+fake") + 1
	return n * defaultRevision
}

type searchPayloadJSON struct {
	Packages []detailsReplyJSON `json:"clickindex:package"`
}

type searchReplyJSON struct {
	Payload searchPayloadJSON `json:"_embedded"`
}

type detailsReplyJSON struct {
	SnapID          string `json:"snap_id"`
	PackageName     string `json:"package_name"`
	Developer       string `json:"origin"`
	DeveloperID     string `json:"developer_id"`
	AnonDownloadURL string `json:"anon_download_url"`
	DownloadURL     string `json:"download_url"`
	Version         string `json:"version"`
	Revision        int    `json:"revision"`
}

func (s *Store) searchEndpoint(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(501)
	fmt.Fprintf(w, "search not implemented")
}

func (s *Store) detailsEndpoint(w http.ResponseWriter, req *http.Request) {
	pkg := strings.TrimPrefix(req.URL.Path, "/snaps/details/")
	if pkg == req.URL.Path {
		panic("how?")
	}

	s.refreshSnaps()

	fn, ok := s.snaps[pkg]
	if !ok {
		http.NotFound(w, req)
		return
	}

	snapFile, err := snap.Open(fn)
	if err != nil {
		http.Error(w, fmt.Sprintf("can not read: %v: %v", fn, err), http.StatusBadRequest)
		return
	}
	// TODO: get side-info from a aux file
	info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("can get info for: %v: %v", fn, err), http.StatusBadRequest)
		return
	}

	details := detailsReplyJSON{
		PackageName:     info.Name(),
		Developer:       defaultDeveloper,
		DeveloperID:     defaultDeveloperID,
		AnonDownloadURL: fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
		DownloadURL:     fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
		Version:         info.Version,
		Revision:        makeRevision(info),
	}

	// use indent because this is a development tool, output
	// should look nice
	out, err := json.MarshalIndent(details, "", "    ")
	if err != nil {
		http.Error(w, fmt.Sprintf("can't marshal: %v: %v", details, err), http.StatusBadRequest)
		return
	}
	w.Write(out)
}

func (s *Store) refreshSnaps() error {
	s.snaps = map[string]string{}

	snaps, err := filepath.Glob(filepath.Join(s.blobDir, "*.snap"))
	if err != nil {
		return err
	}

	for _, fn := range snaps {
		snapFile, err := snap.Open(fn)
		if err != nil {
			return err
		}
		info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
		if err != nil {
			return err
		}
		s.snaps[info.Name()] = fn
	}

	return nil
}

type candidateSnap struct {
	SnapID string `json:"snap_id"`
}

type bulkReqJSON struct {
	CandidateSnaps []candidateSnap `json:"snaps"`
	Fields         []string        `json:"fields"`
}

type payload struct {
	Packages []detailsReplyJSON `json:"clickindex:package"`
}

type bulkReplyJSON struct {
	Payload payload `json:"_embedded"`
}

// FIXME: find a better way to extract the snapID -> name mapping
//        for the fake store
var snapIDtoName = map[string]string{
	"buPKUD3TKqCOgLEjjHx5kSiCpIs5cMuQ": "hello-world",
	"EQPfyVOJF0AZNz9P2IJ6UKwldLFN5TzS": "xkcd-webserver",
	"b8X2psL1ryVrPt5WEmpYiqfr5emixTd7": "ubuntu-core",
	"bul8uZn9U3Ll4ke6BMqvNVEZjuJCSQvO": "canonical-pc",
	"SkKeDk2PRgBrX89DdgULk3pyY5DJo6Jk": "canonical-pc-linux",
	"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw": "test-snapd-tools",
}

func (s *Store) bulkEndpoint(w http.ResponseWriter, req *http.Request) {
	var pkgs bulkReqJSON
	var replyData bulkReplyJSON

	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&pkgs); err != nil {
		http.Error(w, fmt.Sprintf("can't decode request body: %v", err), http.StatusBadRequest)
		return
	}

	s.refreshSnaps()

	// check if we have downloadable snap of the given SnapID
	for _, pkg := range pkgs.CandidateSnaps {

		name := snapIDtoName[pkg.SnapID]
		if name == "" {
			http.Error(w, fmt.Sprintf("unknown snapid: %q", pkg.SnapID), http.StatusBadRequest)
			return
		}

		if fn, ok := s.snaps[name]; ok {
			snapFile, err := snap.Open(fn)
			if err != nil {
				http.Error(w, fmt.Sprintf("can not read: %v: %v", fn, err), http.StatusBadRequest)
				return
			}

			// TODO: get side-info from a aux file
			info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
			if err != nil {
				http.Error(w, fmt.Sprintf("can get info for: %v: %v", fn, err), http.StatusBadRequest)
				return
			}

			replyData.Payload.Packages = append(replyData.Payload.Packages, detailsReplyJSON{
				SnapID:          pkg.SnapID,
				PackageName:     info.Name(),
				Developer:       defaultDeveloper,
				DeveloperID:     defaultDeveloperID,
				DownloadURL:     fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
				AnonDownloadURL: fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
				Version:         info.Version,
				Revision:        makeRevision(info),
			})
		}
	}

	// use indent because this is a development tool, output
	// should look nice
	out, err := json.MarshalIndent(replyData, "", "    ")
	if err != nil {
		http.Error(w, fmt.Sprintf("can marshal: %v: %v", replyData, err), http.StatusBadRequest)
		return
	}
	w.Write(out)

}
