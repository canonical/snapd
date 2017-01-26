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
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/tylerb/graceful.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/asserts/systestkeys"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

func rootEndpoint(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(418)
	fmt.Fprintf(w, "I'm a teapot")
}

func hexify(in string) string {
	bs, err := base64.RawURLEncoding.DecodeString(in)
	if err != nil {
		panic(err)
	}
	return fmt.Sprintf("%x", bs)
}

// Store is our snappy software store implementation
type Store struct {
	url       string
	blobDir   string
	assertDir string

	assertFallback bool
	fallback       *store.Store

	srv *graceful.Server
}

// NewStore creates a new store server serving snaps from the given top directory and assertions from topDir/asserts. If assertFallback is true missing assertions are looked up in the main online store.
func NewStore(topDir, addr string, assertFallback bool) *Store {
	mux := http.NewServeMux()
	var sto *store.Store
	if assertFallback {
		httputil.SetUserAgentFromVersion("unknown", "fakestore")
		sto = store.New(nil, nil)
	}
	store := &Store{
		blobDir:   topDir,
		assertDir: filepath.Join(topDir, "asserts"),

		assertFallback: assertFallback,
		fallback:       sto,

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
	mux.Handle("/download/", http.StripPrefix("/download/", http.FileServer(http.Dir(topDir))))
	mux.HandleFunc("/assertions/", store.assertionsEndpoint)

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

var (
	defaultDeveloper   = "canonical"
	defaultDeveloperID = "canonical"
	defaultRevision    = 424242
)

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

type essentialInfo struct {
	Name        string
	SnapID      string
	DeveloperID string
	DevelName   string
	Revision    int
	Version     string
	Size        uint64
	Digest      string
}

var errInfo = errors.New("cannot get info")

func snapEssentialInfo(w http.ResponseWriter, fn, snapID string, bs asserts.Backstore) (*essentialInfo, error) {
	snapFile, err := snap.Open(fn)
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot read: %v: %v", fn, err), http.StatusBadRequest)
		return nil, errInfo
	}

	info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
	if err != nil {
		http.Error(w, fmt.Sprintf("can get info for: %v: %v", fn, err), http.StatusBadRequest)
		return nil, errInfo
	}

	snapDigest, size, err := asserts.SnapFileSHA3_384(fn)
	if err != nil {
		http.Error(w, fmt.Sprintf("can get digest for: %v: %v", fn, err), http.StatusBadRequest)
		return nil, errInfo
	}

	snapRev, devAcct, err := findSnapRevision(snapDigest, bs)
	if err != nil && err != asserts.ErrNotFound {
		http.Error(w, fmt.Sprintf("can get info for: %v: %v", fn, err), http.StatusBadRequest)
		return nil, errInfo
	}

	var devel, develID string
	var revision int
	if snapRev != nil {
		snapID = snapRev.SnapID()
		develID = snapRev.DeveloperID()
		devel = devAcct.Username()
		revision = snapRev.SnapRevision()
	} else {
		// XXX: fallback until we are always assertion based
		develID = defaultDeveloperID
		devel = defaultDeveloper
		revision = makeRevision(info)
	}

	return &essentialInfo{
		Name:        info.Name(),
		SnapID:      snapID,
		DeveloperID: develID,
		DevelName:   devel,
		Revision:    revision,
		Version:     info.Version,
		Digest:      snapDigest,
		Size:        size,
	}, nil
}

type searchPayloadJSON struct {
	Packages []detailsReplyJSON `json:"clickindex:package"`
}

type searchReplyJSON struct {
	Payload searchPayloadJSON `json:"_embedded"`
}

type detailsReplyJSON struct {
	Architectures   []string `json:"architecture"`
	SnapID          string   `json:"snap_id"`
	PackageName     string   `json:"package_name"`
	Developer       string   `json:"origin"`
	DeveloperID     string   `json:"developer_id"`
	AnonDownloadURL string   `json:"anon_download_url"`
	DownloadURL     string   `json:"download_url"`
	Version         string   `json:"version"`
	Revision        int      `json:"revision"`
	DownloadDigest  string   `json:"download_sha3_384"`
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

	bs, err := s.collectAssertions()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting assertions: %v", err), http.StatusInternalServerError)
		return
	}
	snaps, err := s.collectSnaps()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting snaps: %v", err), http.StatusInternalServerError)
		return
	}

	fn, ok := snaps[pkg]
	if !ok {
		http.NotFound(w, req)
		return
	}

	essInfo, err := snapEssentialInfo(w, fn, "", bs)
	if essInfo == nil {
		if err != errInfo {
			panic(err)
		}
		return
	}

	details := detailsReplyJSON{
		Architectures:   []string{"all"},
		SnapID:          essInfo.SnapID,
		PackageName:     essInfo.Name,
		Developer:       essInfo.DevelName,
		DeveloperID:     essInfo.DeveloperID,
		AnonDownloadURL: fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
		DownloadURL:     fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
		Version:         essInfo.Version,
		Revision:        essInfo.Revision,
		DownloadDigest:  hexify(essInfo.Digest),
	}

	// use indent because this is a development tool, output
	// should look nice
	out, err := json.MarshalIndent(details, "", "    ")
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot marshal: %v: %v", details, err), http.StatusBadRequest)
		return
	}
	w.Write(out)
}

func (s *Store) collectSnaps() (map[string]string, error) {
	snapFns, err := filepath.Glob(filepath.Join(s.blobDir, "*.snap"))
	if err != nil {
		return nil, err
	}

	snaps := map[string]string{}

	for _, fn := range snapFns {
		snapFile, err := snap.Open(fn)
		if err != nil {
			return nil, err
		}
		info, err := snap.ReadInfoFromSnapFile(snapFile, nil)
		if err != nil {
			return nil, err
		}
		snaps[info.Name()] = fn
	}

	return snaps, err
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

var someSnapIDtoName = map[string]map[string]string{
	"production": {
		"b8X2psL1ryVrPt5WEmpYiqfr5emixTd7": "ubuntu-core",
		"99T7MUlRhtI3U0QFgl5mXXESAiSwt776": "core",
		"bul8uZn9U3Ll4ke6BMqvNVEZjuJCSQvO": "canonical-pc",
		"SkKeDk2PRgBrX89DdgULk3pyY5DJo6Jk": "canonical-pc-linux",
		"eFe8BTR5L5V9F7yHeMAPxkEr2NdUXMtw": "test-snapd-tools",
		"Wcs8QL2iRQMjsPYQ4qz4V1uOlElZ1ZOb": "test-snapd-python-webserver",
		"DVvhXhpa9oJjcm0rnxfxftH1oo5vTW1M": "test-snapd-go-webserver",
	},
	"staging": {
		"xMNMpEm0COPZy7jq9YRwWVLCD9q5peow": "core",
		"02AHdOomTzby7gTaiLX3M3SGMmXDfLJp": "test-snapd-tools",
		"uHjTANBWSXSiYzNOUXZNDnOSH3POSqWS": "test-snapd-python-webserver",
		"edmdK5G9fP1q1bGyrjnaDXS4RkdjiTGV": "test-snapd-go-webserver",
	},
}

func (s *Store) bulkEndpoint(w http.ResponseWriter, req *http.Request) {
	var pkgs bulkReqJSON
	var replyData bulkReplyJSON

	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&pkgs); err != nil {
		http.Error(w, fmt.Sprintf("cannot decode request body: %v", err), http.StatusBadRequest)
		return
	}

	bs, err := s.collectAssertions()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting assertions: %v", err), http.StatusInternalServerError)
		return
	}

	var remoteStore string
	if osutil.GetenvBool("SNAPPY_USE_STAGING_STORE") {
		remoteStore = "staging"
	} else {
		remoteStore = "production"
	}
	snapIDtoName, err := addSnapIDs(bs, someSnapIDtoName[remoteStore])
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting snapIDs: %v", err), http.StatusInternalServerError)
		return
	}

	snaps, err := s.collectSnaps()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting snaps: %v", err), http.StatusInternalServerError)
		return
	}

	// check if we have downloadable snap of the given SnapID
	for _, pkg := range pkgs.CandidateSnaps {

		name := snapIDtoName[pkg.SnapID]
		if name == "" {
			http.Error(w, fmt.Sprintf("unknown snapid: %q", pkg.SnapID), http.StatusBadRequest)
			return
		}

		if fn, ok := snaps[name]; ok {
			essInfo, err := snapEssentialInfo(w, fn, pkg.SnapID, bs)
			if essInfo == nil {
				if err != errInfo {
					panic(err)
				}
				return
			}

			replyData.Payload.Packages = append(replyData.Payload.Packages, detailsReplyJSON{
				Architectures:   []string{"all"},
				SnapID:          essInfo.SnapID,
				PackageName:     essInfo.Name,
				Developer:       essInfo.DevelName,
				DeveloperID:     essInfo.DeveloperID,
				DownloadURL:     fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
				AnonDownloadURL: fmt.Sprintf("%s/download/%s", s.URL(), filepath.Base(fn)),
				Version:         essInfo.Version,
				Revision:        essInfo.Revision,
				DownloadDigest:  hexify(essInfo.Digest),
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

func (s *Store) collectAssertions() (asserts.Backstore, error) {
	bs := asserts.NewMemoryBackstore()

	add := func(a asserts.Assertion) {
		bs.Put(a.Type(), a)
	}

	for _, t := range sysdb.Trusted() {
		add(t)
	}
	add(systestkeys.TestRootAccount)
	add(systestkeys.TestRootAccountKey)
	add(systestkeys.TestStoreAccountKey)

	aFiles, err := filepath.Glob(filepath.Join(s.assertDir, "*"))
	if err != nil {
		return nil, err
	}

	for _, fn := range aFiles {
		b, err := ioutil.ReadFile(fn)
		if err != nil {
			return nil, err
		}

		a, err := asserts.Decode(b)
		if err != nil {
			return nil, err
		}

		add(a)
	}

	return bs, nil
}

func isAssertNotFound(err error) bool {
	if err == asserts.ErrNotFound {
		return true
	}
	if _, ok := err.(*store.AssertionNotFoundError); ok {
		return true
	}
	return false
}

func (s *Store) retrieveAssertion(bs asserts.Backstore, assertType *asserts.AssertionType, primaryKey []string) (asserts.Assertion, error) {
	a, err := bs.Get(assertType, primaryKey, assertType.MaxSupportedFormat())
	if err == asserts.ErrNotFound && s.assertFallback {
		return s.fallback.Assertion(assertType, primaryKey, nil)
	}
	return a, err
}

func (s *Store) assertionsEndpoint(w http.ResponseWriter, req *http.Request) {
	assertPath := strings.TrimPrefix(req.URL.Path, "/assertions/")

	bs, err := s.collectAssertions()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting assertions: %v", err), http.StatusInternalServerError)
		return
	}

	comps := strings.Split(assertPath, "/")

	if len(comps) == 0 {
		http.Error(w, "missing assertion type", http.StatusBadRequest)
		return

	}

	typ := asserts.Type(comps[0])
	if typ == nil {
		http.Error(w, fmt.Sprintf("unknown assertion type: %s", comps[0]), http.StatusBadRequest)
		return
	}

	if len(typ.PrimaryKey) != len(comps)-1 {
		http.Error(w, fmt.Sprintf("wrong primary key length: %v", comps), http.StatusBadRequest)
		return
	}

	a, err := s.retrieveAssertion(bs, typ, comps[1:])
	if isAssertNotFound(err) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		w.Write([]byte(`{"status": 404}`))
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot retrieve assertion %v: %v", comps, err), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", asserts.MediaType)
	w.WriteHeader(http.StatusOK)
	w.Write(asserts.Encode(a))
}

func addSnapIDs(bs asserts.Backstore, initial map[string]string) (map[string]string, error) {
	m := make(map[string]string)
	for id, name := range initial {
		m[id] = name
	}

	hit := func(a asserts.Assertion) {
		decl := a.(*asserts.SnapDeclaration)
		m[decl.SnapID()] = decl.SnapName()
	}

	err := bs.Search(asserts.SnapDeclarationType, nil, hit, asserts.SnapDeclarationType.MaxSupportedFormat())
	if err != nil {
		return nil, err
	}

	return m, nil
}

func findSnapRevision(snapDigest string, bs asserts.Backstore) (*asserts.SnapRevision, *asserts.Account, error) {
	a, err := bs.Get(asserts.SnapRevisionType, []string{snapDigest}, asserts.SnapRevisionType.MaxSupportedFormat())
	if err != nil {
		return nil, nil, err
	}
	snapRev := a.(*asserts.SnapRevision)

	a, err = bs.Get(asserts.AccountType, []string{snapRev.DeveloperID()}, asserts.AccountType.MaxSupportedFormat())
	if err != nil {
		return nil, nil, err
	}
	devAcct := a.(*asserts.Account)

	return snapRev, devAcct, nil
}
