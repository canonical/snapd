// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

	"github.com/ubuntu-core/snappy/snap"
)

var (
	defaultAddr = "localhost:11028"
	// FIXME: make both hardcoded values configurable via
	//        e.g. a "foo_1.0.snap.info" file next to the snap
	defaultDeveloper = "canonical"
	defaultRevision  = 424242
)

func rootEndpoint(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(418)
	fmt.Fprintf(w, "I'm a teapot")
}

// Store is our snappy software store implementation
type Store struct {
	url              string
	blobDir          string
	defaultDeveloper string

	srv *graceful.Server

	snaps map[string]string
}

// NewStore creates a new store server
func NewStore(blobDir string) *Store {
	mux := http.NewServeMux()
	store := &Store{
		blobDir:          blobDir,
		snaps:            make(map[string]string),
		defaultDeveloper: defaultDeveloper,

		url: fmt.Sprintf("http://%s", defaultAddr),
		srv: &graceful.Server{
			Timeout: 2 * time.Second,

			Server: &http.Server{
				Addr:    defaultAddr,
				Handler: mux,
			},
		},
	}

	mux.HandleFunc("/", rootEndpoint)
	mux.HandleFunc("/search", store.searchEndpoint)
	mux.HandleFunc("/package/", store.detailsEndpoint)
	mux.HandleFunc("/click-metadata", store.bulkEndpoint)
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
	s.srv.Stop(0)
	timeoutTime := 2000 * time.Millisecond

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
	Name            string `json:"name"`
	PackageName     string `json:"package_name"`
	Developer       string `json:"origin"`
	AnonDownloadURL string `json:"anon_download_url"`
	DownloadURL     string `json:"download_url"`
	Version         string `json:"version"`
	Revision        int    `json:"revision"`
}

func (s *Store) detailsEndpoint(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(501)
	fmt.Fprintf(w, "details not implemented anymore")
	return
}

func (s *Store) searchEndpoint(w http.ResponseWriter, req *http.Request) {
	query := req.URL.Query()
	q := query.Get("q")
	if !strings.HasPrefix(q, "package_name:\"") {
		w.WriteHeader(501)
		fmt.Fprintf(w, "full search not implemented")
		return

	}
	if !strings.HasSuffix(q, "\"") {
		w.WriteHeader(400)
		fmt.Fprintf(w, "missing final \"")
		return
	}

	s.refreshSnaps()

	pkg := q[len("package_name:\"") : len(q)-1]

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
		Name:            fmt.Sprintf("%s.%s", info.Name(), s.defaultDeveloper),
		PackageName:     info.Name(),
		Developer:       defaultDeveloper,
		AnonDownloadURL: fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
		DownloadURL:     fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
		Version:         info.Version,
		Revision:        makeRevision(info),
	}

	replyData := searchReplyJSON{
		Payload: searchPayloadJSON{
			Packages: []detailsReplyJSON{details},
		},
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

type bulkReqJSON struct {
	Name []string
}

type bulkReplyJSON struct {
	Status          string `json:"status"`
	Name            string `json:"name"`
	PackageName     string `json:"package_name"`
	Developer       string `json:"origin"`
	AnonDownloadURL string `json:"anon_download_url"`
	Version         string `json:"version"`
	Revision        int    `json:"revision"`
}

func (s *Store) bulkEndpoint(w http.ResponseWriter, req *http.Request) {
	var pkgs bulkReqJSON
	var replyData []bulkReplyJSON

	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&pkgs); err != nil {
		http.Error(w, fmt.Sprintf("can't decode request body: %v", err), http.StatusBadRequest)
		return
	}

	s.refreshSnaps()

	// check if we have downloadable snap of the given name
	for _, pkgWithChannel := range pkgs.Name {
		pkg := strings.Split(pkgWithChannel, "/")[0]

		if fn, ok := s.snaps[pkg]; ok {
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

			replyData = append(replyData, bulkReplyJSON{
				Status:          "Published",
				Name:            fmt.Sprintf("%s.%s", info.Name(), s.defaultDeveloper),
				PackageName:     info.Name(),
				Developer:       defaultDeveloper,
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
