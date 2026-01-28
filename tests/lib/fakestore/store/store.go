// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/asserts/systestkeys"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snapdenv"
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

type snapCachedInfo struct {
	info *snap.Info
	err  error
}

// Store is our snappy software store implementation
type Store struct {
	url       string
	blobDir   string
	assertDir string

	assertFallback bool
	fallback       *store.Store

	srv *http.Server

	channelRepository *ChannelRepository

	snapsCache map[string]snapCachedInfo
}

// NewStore creates a new store server serving snaps from the given top directory and assertions from topDir/asserts. If assertFallback is true missing assertions are looked up in the main online store.
func NewStore(topDir, addr string, assertFallback bool) *Store {
	mux := http.NewServeMux()
	var sto *store.Store
	if assertFallback {
		snapdenv.SetUserAgentFromVersion("unknown", nil, "fakestore")
		sto = store.New(nil, nil)
	}
	store := &Store{
		blobDir:   topDir,
		assertDir: filepath.Join(topDir, "asserts"),

		assertFallback: assertFallback,
		fallback:       sto,

		url: fmt.Sprintf("http://%s", addr),
		srv: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
		channelRepository: &ChannelRepository{
			rootDir: filepath.Join(topDir, "channels"),
		},
		snapsCache: make(map[string]snapCachedInfo),
	}

	mux.HandleFunc("/", rootEndpoint)
	mux.HandleFunc("/api/v1/snaps/search", store.searchEndpoint)
	mux.HandleFunc("/api/v1/snaps/details/", store.detailsEndpoint)
	mux.HandleFunc("/api/v1/snaps/metadata", store.bulkEndpoint)
	mux.Handle("/download/", http.StripPrefix("/download/", http.FileServer(http.Dir(topDir))))

	mux.HandleFunc("/api/v1/snaps/auth/nonces", store.nonceEndpoint)
	mux.HandleFunc("/api/v1/snaps/auth/sessions", store.sessionEndpoint)

	// v2
	mux.HandleFunc("/v2/assertions/", store.assertionsEndpoint)
	mux.HandleFunc("/v2/snaps/refresh", store.snapActionEndpoint)

	mux.HandleFunc("/v2/repairs/", store.repairsEndpoint)

	return store
}

// URL returns the base-url that the store is listening on
func (s *Store) URL() string {
	return s.url
}

func (s *Store) RealURL(req *http.Request) string {
	if req.Host == "" {
		return s.url
	} else {
		return fmt.Sprintf("http://%s", req.Host)
	}
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

	s.url = fmt.Sprintf("http://%s", l.Addr())

	go s.srv.Serve(l)
	return nil
}

// Stop stops the server
func (s *Store) Stop() error {
	timeoutTime := 2000 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), timeoutTime)
	defer cancel()
	if err := s.srv.Shutdown(ctx); err != nil {
		// forceful close
		s.srv.Close()
		return fmt.Errorf("store failed to stop after: %s", timeoutTime)
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
	Confinement string
	Type        string
	Base        string
}

func (s *Store) snapEssentialInfo(fn, snapID string, bs asserts.Backstore) (*essentialInfo, error) {
	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	defer restoreSanitize()

	cached, isCached := s.snapsCache[fn]
	if !isCached {
		f, err := snapfile.Open(fn)
		if err != nil {
			return nil, fmt.Errorf("cannot read: %v: %v", fn, err)
		}

		info, err := snap.ReadInfoFromSnapFile(f, nil)
		cached.info = info
		cached.err = err
		s.snapsCache[fn] = cached
	}
	if cached.err != nil {
		return nil, fmt.Errorf("cannot get info for: %v: %v", fn, cached.err)
	}

	snapDigest, size, err := asserts.SnapFileSHA3_384(fn)
	if err != nil {
		return nil, fmt.Errorf("cannot get digest for: %v: %v", fn, err)
	}

	snapRev, devAcct, err := findSnapRevision(snapDigest, bs)
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
		return nil, fmt.Errorf("cannot get info for: %v: %v", fn, err)
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
		revision = makeRevision(cached.info)
	}

	return &essentialInfo{
		Name:        cached.info.SnapName(),
		SnapID:      snapID,
		DeveloperID: develID,
		DevelName:   devel,
		Revision:    revision,
		Version:     cached.info.Version,
		Digest:      snapDigest,
		Size:        size,
		Confinement: string(cached.info.Confinement),
		Type:        string(cached.info.Type()),
		Base:        cached.info.Base,
	}, nil
}

func addComponentBlobToRevisionSet(snaps map[string]*revisionSet, snapIDs map[string]string, fn string, bs asserts.Backstore) error {
	f, err := snapfile.Open(fn)
	if err != nil {
		return fmt.Errorf("cannot read: %v: %v", fn, err)
	}

	info, err := snap.ReadComponentInfoFromContainer(f, nil, nil)
	if err != nil {
		return fmt.Errorf("cannot get info for: %v: %v", fn, err)
	}

	compName := info.Component.ComponentName
	snapName := info.Component.SnapName

	digest, _, err := asserts.SnapFileSHA3_384(fn)
	if err != nil {
		return fmt.Errorf("cannot get digest for: %v: %v", fn, err)
	}

	set, ok := snaps[snapName]
	if !ok {
		return fmt.Errorf("cannot find snap %q for component: %q", snapName, info.Component)
	}

	snapID, ok := snapIDs[snapName]
	if !ok {
		return fmt.Errorf("cannot find snap id for snap %q", snapName)
	}

	pk, err := asserts.PrimaryKeyFromHeaders(asserts.SnapResourceRevisionType, map[string]string{
		"snap-id":           snapID,
		"resource-name":     compName,
		"resource-sha3-384": digest,
	})
	if err != nil {
		return err
	}

	a, err := bs.Get(asserts.SnapResourceRevisionType, pk, asserts.SnapResourceRevisionType.MaxSupportedFormat())
	if err != nil {
		return err
	}
	compRev := snap.R(a.(*asserts.SnapResourceRevision).ResourceRevision())

	for snapRev := range set.revisions {
		pk, err := asserts.PrimaryKeyFromHeaders(asserts.SnapResourcePairType, map[string]string{
			"resource-name":     compName,
			"snap-id":           snapID,
			"resource-revision": compRev.String(),
			"snap-revision":     snapRev.String(),
		})
		if err != nil {
			return err
		}

		_, err = bs.Get(asserts.SnapResourcePairType, pk, asserts.SnapResourcePairType.MaxSupportedFormat())
		if err != nil {
			// no pair assertion for this snap revision, so this one isn't
			// associated with this snap revision
			if errors.Is(err, &asserts.NotFoundError{}) {
				continue
			}
			return err
		}

		if err := set.addComponent(compName, compRev, fn, snapRev); err != nil {
			return err
		}
	}

	return nil
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
	Confinement     string   `json:"confinement"`
	Type            string   `json:"type"`
	Base            string   `json:"base,omitempty"`
}

func (s *Store) searchEndpoint(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(501)
	fmt.Fprintf(w, "search not implemented")
}

func (s *Store) repairsEndpoint(w http.ResponseWriter, req *http.Request) {
	brandAndRepairID := strings.Split(strings.TrimPrefix(req.URL.Path, "/v2/repairs/"), "/")
	if len(brandAndRepairID) != 2 {
		http.Error(w, "missing brand and repair ID", 400)
		return
	}

	bs, err := s.collectAssertions()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting assertions: %v", err), 500)
		return
	}

	a, err := s.retrieveAssertion(bs, asserts.RepairType, brandAndRepairID)
	if errors.Is(err, &asserts.NotFoundError{}) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		w.Write([]byte(`{"status": 404}`))
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot retrieve repair assertion %v: %v", brandAndRepairID, err), 400)
		return
	}

	// handle If-None-Match caching
	revNumString := req.Header.Get("If-None-Match")
	if revNumString != "" {
		revRegexp := regexp.MustCompile(`^"([0-9]+)"$`)
		match := revRegexp.FindStringSubmatch(revNumString)
		if match == nil || len(match) != 2 {
			http.Error(w, fmt.Sprintf("malformed If-None-Match header (%q): must be repair revision number in quotes", revNumString), 400)
			return
		}
		revNum, err := strconv.Atoi(match[1])
		if err != nil {
			http.Error(w, fmt.Sprintf("malformed If-None-Match header (%q): %v", revNumString, err), 400)
			return
		}

		if revNum == a.Revision() {
			// if the If-None-Match header is the assertion revision verbatim
			// then return 304 (Not Modified) and stop
			w.WriteHeader(304)
			return
		}
	}

	// there are two cases, one where we are asked for the full assertion, and
	// one where we are asked for JSON headers of the assertion, so check which
	// one we were asked for by inspecting the Accept header
	switch accept := req.Header.Get("Accept"); accept {
	case "application/json":
		// headers only
		headers := a.Headers()
		// we have to wrap the headers in a JSON object under the key
		// "headers"
		resp := map[string]any{
			"headers": headers,
		}
		b, err := json.Marshal(resp)
		if err != nil {
			http.Error(w, fmt.Sprintf("internal error collecting assertion headers as json: %v", err), 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write(b)
	case "application/x.ubuntu.assertion":
		// full assertion
		w.Header().Set("Content-Type", asserts.MediaType)
		w.WriteHeader(200)
		w.Write(asserts.Encode(a))
	default:
		http.Error(w, fmt.Sprintf("unsupported Accept format (%q): only application/json and application/x.ubuntu.assertion is support", accept), 400)
	}
}

func (s *Store) detailsEndpoint(w http.ResponseWriter, req *http.Request) {
	pkg := strings.TrimPrefix(req.URL.Path, "/api/v1/snaps/details/")
	if pkg == req.URL.Path {
		panic("how?")
	}

	bs, err := s.collectAssertions()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting assertions: %v", err), 500)
		return
	}
	snaps, err := s.collectSnaps(bs)
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting snaps: %v", err), 500)
		return
	}

	set, ok := snaps[pkg]
	if !ok {
		http.NotFound(w, req)
		return
	}

	sn := set.getLatest()

	essInfo, err := s.snapEssentialInfo(sn.path, "", bs)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	details := detailsReplyJSON{
		Architectures:   []string{"all"},
		SnapID:          essInfo.SnapID,
		PackageName:     essInfo.Name,
		Developer:       essInfo.DevelName,
		DeveloperID:     essInfo.DeveloperID,
		AnonDownloadURL: fmt.Sprintf("%s/download/%s", s.RealURL(req), filepath.Base(sn.path)),
		DownloadURL:     fmt.Sprintf("%s/download/%s", s.RealURL(req), filepath.Base(sn.path)),
		Version:         essInfo.Version,
		Revision:        essInfo.Revision,
		DownloadDigest:  hexify(essInfo.Digest),
		Confinement:     essInfo.Confinement,
		Type:            essInfo.Type,
		Base:            essInfo.Base,
	}

	// use indent because this is a development tool, output
	// should look nice
	out, err := json.MarshalIndent(details, "", "    ")
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot marshal: %v: %v", details, err), 400)
		return
	}
	w.Write(out)
}

type revisionSet struct {
	latest    snap.Revision
	revisions map[snap.Revision]availableSnap
}

type availableSnap struct {
	path       string
	components map[string]availableComponent
}

type availableComponent struct {
	path     string
	revision snap.Revision
}

func (rs *revisionSet) get(rev snap.Revision) (availableSnap, bool) {
	if rev.Unset() {
		rev = rs.latest
	}

	sn, ok := rs.revisions[rev]
	return sn, ok
}

func (rs *revisionSet) getLatest() availableSnap {
	sn, ok := rs.revisions[rs.latest]
	if !ok {
		panic("internal error: revision set should always contain latest revision")
	}

	return sn
}

func (rs *revisionSet) add(rev snap.Revision, path string) {
	if rs.revisions == nil {
		rs.revisions = make(map[snap.Revision]availableSnap)
	}

	if rs.latest.N < rev.N {
		rs.latest = rev
	}
	rs.revisions[rev] = availableSnap{path: path, components: make(map[string]availableComponent)}
}

func (rs *revisionSet) addComponent(name string, compRev snap.Revision, path string, snapRev snap.Revision) error {
	sn, ok := rs.revisions[snapRev]
	if !ok {
		return fmt.Errorf("cannot find snap revision %q", snapRev)
	}

	sn.components[name] = availableComponent{path: path, revision: compRev}

	return nil
}

func (s *Store) collectSnaps(bs asserts.Backstore) (map[string]*revisionSet, error) {
	snapFns, err := filepath.Glob(filepath.Join(s.blobDir, "*.snap"))
	if err != nil {
		return nil, err
	}

	restoreSanitize := snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {})
	defer restoreSanitize()

	snaps := make(map[string]*revisionSet)
	snapNamesToID := make(map[string]string, len(snapFns))
	for _, fn := range snapFns {
		// if the snap is asserted, then the returned info will contain the ID
		// taken from the database
		const snapID = ""
		info, err := s.snapEssentialInfo(fn, snapID, bs)
		if err != nil {
			return nil, err
		}

		if _, ok := snaps[info.Name]; !ok {
			snaps[info.Name] = &revisionSet{}
		}

		snaps[info.Name].add(snap.R(info.Revision), fn)

		channels, err := s.channelRepository.findSnapChannels(info.Digest)
		if err != nil {
			return nil, err
		}

		for _, channel := range channels {
			compositeName := fmt.Sprintf("%s|%s", info.Name, channel)
			if _, ok := snaps[compositeName]; !ok {
				snaps[compositeName] = &revisionSet{}
			}
			snaps[compositeName].add(snap.R(info.Revision), fn)
		}

		if info.SnapID != "" {
			snapNamesToID[info.Name] = info.SnapID
		}

		logger.Debugf("found snap %q (revision %d) at %v", info.Name, info.Revision, fn)
	}

	compFns, err := filepath.Glob(filepath.Join(s.blobDir, "*.comp"))
	if err != nil {
		return nil, err
	}

	for _, fn := range compFns {
		if err := addComponentBlobToRevisionSet(snaps, snapNamesToID, fn, bs); err != nil {
			return nil, err
		}
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
		http.Error(w, fmt.Sprintf("cannot decode request body: %v", err), 400)
		return
	}

	bs, err := s.collectAssertions()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting assertions: %v", err), 500)
		return
	}

	var remoteStore string
	if snapdenv.UseStagingStore() {
		remoteStore = "staging"
	} else {
		remoteStore = "production"
	}
	snapIDtoName, err := addSnapIDs(bs, someSnapIDtoName[remoteStore])
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting snapIDs: %v", err), 500)
		return
	}

	snaps, err := s.collectSnaps(bs)
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting snaps: %v", err), 500)
		return
	}

	// check if we have downloadable snap of the given SnapID
	for _, pkg := range pkgs.CandidateSnaps {
		name := snapIDtoName[pkg.SnapID]
		if name == "" {
			http.Error(w, fmt.Sprintf("unknown snap-id: %q", pkg.SnapID), 400)
			return
		}

		set, ok := snaps[name]
		if !ok {
			continue
		}

		sn := set.getLatest()

		essInfo, err := s.snapEssentialInfo(sn.path, pkg.SnapID, bs)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		replyData.Payload.Packages = append(replyData.Payload.Packages, detailsReplyJSON{
			Architectures:   []string{"all"},
			SnapID:          essInfo.SnapID,
			PackageName:     essInfo.Name,
			Developer:       essInfo.DevelName,
			DeveloperID:     essInfo.DeveloperID,
			DownloadURL:     fmt.Sprintf("%s/download/%s", s.RealURL(req), filepath.Base(sn.path)),
			AnonDownloadURL: fmt.Sprintf("%s/download/%s", s.RealURL(req), filepath.Base(sn.path)),
			Version:         essInfo.Version,
			Revision:        essInfo.Revision,
			DownloadDigest:  hexify(essInfo.Digest),
			Confinement:     essInfo.Confinement,
			Type:            essInfo.Type,
			Base:            essInfo.Base,
		})
	}

	// use indent because this is a development tool, output
	// should look nice
	out, err := json.MarshalIndent(replyData, "", "    ")
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot marshal: %v: %v", replyData, err), 400)
		return
	}
	w.Write(out)

}

func (s *Store) collectAssertions() (asserts.Backstore, error) {
	bs := asserts.NewMemoryBackstore()

	add := func(a asserts.Assertion) {
		if err := bs.Put(a.Type(), a); err != nil {
			logger.Noticef("cannot add assertion %q: %v", a.Headers(), err)
		}
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
		b, err := os.ReadFile(fn)
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

type currentSnap struct {
	SnapID          string `json:"snap-id"`
	InstanceKey     string `json:"instance-key"`
	TrackingChannel string `json:"tracking-channel"`
}

type snapAction struct {
	Action      string `json:"action"`
	InstanceKey string `json:"instance-key"`
	SnapID      string `json:"snap-id"`
	Name        string `json:"name"`
	Revision    int    `json:"revision,omitempty"`
	Channel     string `json:"channel,omitempty"`
}

type snapActionRequest struct {
	Context []currentSnap `json:"context"`
	Fields  []string      `json:"fields"`
	Actions []snapAction  `json:"actions"`
}

type snapActionResult struct {
	Result      string          `json:"result"`
	InstanceKey string          `json:"instance-key"`
	SnapID      string          `json:"snap-id"`
	Name        string          `json:"name"`
	Snap        detailsResultV2 `json:"snap"`
}

type snapActionResultList struct {
	Results []*snapActionResult `json:"results"`
}

type detailsResultV2 struct {
	Architectures []string `json:"architectures"`
	Base          string   `json:"base,omitempty"`
	SnapID        string   `json:"snap-id"`
	Name          string   `json:"name"`
	Publisher     struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"publisher"`
	Download    downloadInfo         `json:"download"`
	Version     string               `json:"version"`
	Revision    int                  `json:"revision"`
	Confinement string               `json:"confinement"`
	Type        string               `json:"type"`
	Resources   []snapResourceResult `json:"resources,omitempty"`
	SnapYAML    string               `json:"snap-yaml"`
}

type downloadInfo struct {
	URL      string `json:"url"`
	Sha3_384 string `json:"sha3-384"`
	Size     uint64 `json:"size"`
}

type snapResourceResult struct {
	Download downloadInfo `json:"download"`
	Type     string       `json:"type"`
	Name     string       `json:"name"`
	Revision int          `json:"revision"`
	Version  string       `json:"version"`
}

func (s *Store) snapActionEndpoint(w http.ResponseWriter, req *http.Request) {
	var reqData snapActionRequest
	var replyData snapActionResultList

	decoder := json.NewDecoder(req.Body)
	if err := decoder.Decode(&reqData); err != nil {
		http.Error(w, fmt.Sprintf("cannot decode request body: %v", err), 400)
		return
	}

	bs, err := s.collectAssertions()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting assertions: %v", err), 500)
		return
	}

	var remoteStore string
	if snapdenv.UseStagingStore() {
		remoteStore = "staging"
	} else {
		remoteStore = "production"
	}
	snapIDtoName, err := addSnapIDs(bs, someSnapIDtoName[remoteStore])
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting snapIDs: %v", err), 500)
		return
	}

	snaps, err := s.collectSnaps(bs)
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting snaps: %v", err), 500)
		return
	}

	actions := reqData.Actions
	if len(actions) == 1 && actions[0].Action == "refresh-all" {
		actions = make([]snapAction, len(reqData.Context))
		for i, s := range reqData.Context {
			actions[i] = snapAction{
				Action:      "refresh",
				SnapID:      s.SnapID,
				InstanceKey: s.InstanceKey,
			}
		}
	}

	contextChannel := make(map[string]string)
	for _, context := range reqData.Context {
		contextChannel[context.SnapID] = context.TrackingChannel
	}

	// check if we have downloadable snap of the given SnapID or name
	for _, a := range actions {
		name := a.Name
		snapID := a.SnapID
		if a.Action == "refresh" {
			name = snapIDtoName[snapID]
		}

		if name == "" {
			if isAutoRefreshRequest(req) {
				// Skip auto-refresh for snap installed on the system not added to the fakestore
				continue
			}
			http.Error(w, fmt.Sprintf("unknown snap-id: %q", snapID), 400)
			return
		}

		var set *revisionSet
		var foundSnap bool
		var channel string
		foundChannel := false
		if a.Channel != "" {
			foundChannel = true
			channel = a.Channel
		}
		if !foundChannel {
			channel, foundChannel = contextChannel[snapID]
		}
		if foundChannel {
			set, foundSnap = snaps[fmt.Sprintf("%s|%s", name, channel)]
		}
		if !foundSnap {
			// FIXME: It is possible that many tests do
			// not use channels correctly. So we have to
			// fallback to searching for snaps by just
			// name, without channel. Maybe we should
			// remove that, and fix all the tests instead.
			set, foundSnap = snaps[name]
		}

		if !foundSnap {
			continue
		}

		sn, ok := set.get(snap.R(a.Revision))

		if !ok {
			// TODO: this should send back some error?
			continue
		}

		essInfo, err := s.snapEssentialInfo(sn.path, snapID, bs)
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}

		// include snap.yaml
		f, err := snapfile.Open(sn.path)
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot open snap container: %v", err.Error()), 500)
			return
		}

		snapYaml, err := f.ReadFile("meta/snap.yaml")
		if err != nil {
			http.Error(w, fmt.Sprintf("cannot read snap.yaml: %v", err.Error()), 500)
			return
		}

		resources := make([]snapResourceResult, 0, len(sn.components))
		for compName, comp := range sn.components {
			f, err := snapfile.Open(path.Join(comp.path))
			if err != nil {
				http.Error(w, fmt.Sprintf("cannot read: %v: %v", compName, err), 400)
				return
			}

			digest, size, err := asserts.SnapFileSHA3_384(comp.path)
			if err != nil {
				http.Error(w, fmt.Sprintf("cannot get digest for: %v: %v", compName, err), 400)
				return
			}

			compInfo, err := snap.ReadComponentInfoFromContainer(f, nil, nil)
			if err != nil {
				http.Error(w, fmt.Sprintf("cannot get info for: %v: %v", compName, err), 400)
				return
			}

			resources = append(resources, snapResourceResult{
				Name:     compName,
				Revision: comp.revision.N,
				Type:     fmt.Sprintf("component/%s", compInfo.Type),
				Version:  compInfo.Version(essInfo.Version),
				Download: downloadInfo{
					URL:      fmt.Sprintf("%s/download/%s", s.RealURL(req), filepath.Base(comp.path)),
					Sha3_384: hexify(digest),
					Size:     size,
				},
			})
		}

		details := detailsResultV2{
			Architectures: []string{"all"},
			SnapID:        essInfo.SnapID,
			Name:          essInfo.Name,
			Version:       essInfo.Version,
			Revision:      essInfo.Revision,
			Confinement:   essInfo.Confinement,
			Type:          essInfo.Type,
			Base:          essInfo.Base,
			SnapYAML:      string(snapYaml),
		}
		if len(resources) > 0 {
			details.Resources = resources
		}

		res := &snapActionResult{
			Result:      a.Action,
			InstanceKey: a.InstanceKey,
			SnapID:      essInfo.SnapID,
			Name:        essInfo.Name,
			Snap:        details,
		}

		logger.Debugf("requested snap %q revision %d", essInfo.Name, a.Revision)
		res.Snap.Publisher.ID = essInfo.DeveloperID
		res.Snap.Publisher.Username = essInfo.DevelName
		res.Snap.Download.URL = fmt.Sprintf("%s/download/%s", s.RealURL(req), filepath.Base(sn.path))
		res.Snap.Download.Sha3_384 = hexify(essInfo.Digest)
		res.Snap.Download.Size = essInfo.Size
		replyData.Results = append(replyData.Results, res)
	}

	// use indent because this is a development tool, output
	// should look nice
	out, err := json.MarshalIndent(replyData, "", "    ")
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot marshal: %v: %v", replyData, err), 400)
		return
	}
	w.Write(out)
}

func isAutoRefreshRequest(req *http.Request) bool {
	return req.Header.Get("Snap-Refresh-Reason") == "scheduled"
}

func (s *Store) retrieveAssertion(bs asserts.Backstore, assertType *asserts.AssertionType, primaryKey []string) (asserts.Assertion, error) {
	a, err := bs.Get(assertType, primaryKey, assertType.MaxSupportedFormat())
	if errors.Is(err, &asserts.NotFoundError{}) && s.assertFallback {
		return s.fallback.Assertion(assertType, primaryKey, nil)
	}
	return a, err
}

func (s *Store) retrieveLatestSequenceFormingAssertion(bs asserts.Backstore, assertType *asserts.AssertionType, sequenceKey []string) (asserts.Assertion, error) {
	a, err := bs.SequenceMemberAfter(assertType, sequenceKey, -1, assertType.MaxSupportedFormat())
	if errors.Is(err, &asserts.NotFoundError{}) && s.assertFallback {
		return s.fallback.SeqFormingAssertion(assertType, sequenceKey, -1, nil)
	}
	return a, err
}

func (s *Store) sequenceFromQueryValues(values url.Values) (int, error) {
	if val, ok := values["sequence"]; ok {
		// special case value of 'latest', in that case
		// we return -1 to indicate we want the newest
		if val[0] != "latest" {
			seq, err := strconv.Atoi(val[0])
			if err != nil {
				return -1, fmt.Errorf("cannot parse sequence %s: %v", val[0], err)
			}

			// Only positive integers and 'latest' are valid
			if seq <= 0 {
				return -1, fmt.Errorf("the requested sequence must be above 0")
			}
			return seq, nil
		}
	}
	return -1, nil
}

func (s *Store) assertTypeAndKey(urlPath string) (*asserts.AssertionType, []string, error) {
	// trim the assertions prefix, and handle any query parameters
	assertPath := strings.TrimPrefix(urlPath, "/v2/assertions/")
	comps := strings.Split(assertPath, "/")
	if len(comps) == 0 {
		return nil, nil, fmt.Errorf("missing assertion type")
	}

	typ := asserts.Type(comps[0])
	if typ == nil {
		return nil, nil, fmt.Errorf("unknown assertion type: %s", comps[0])
	}
	return typ, comps[1:], nil
}

func (s *Store) retrieveAssertionWrapper(bs asserts.Backstore, assertType *asserts.AssertionType, keyParts []string, values url.Values) (asserts.Assertion, error) {
	pk := keyParts
	if assertType.SequenceForming() {
		seq, err := s.sequenceFromQueryValues(values)
		if err != nil {
			return nil, err
		}

		// If no sequence value was provided, or when requesting the latest sequence
		// point of an assertion, we use a different method of resolving the assertion.
		if seq <= 0 {
			return s.retrieveLatestSequenceFormingAssertion(bs, assertType, keyParts)
		}

		// Otherwise append the sequence to form the primary key and use
		// the default retrieval.
		pk = append(pk, strconv.Itoa(seq))
	}

	if !assertType.AcceptablePrimaryKey(pk) {
		return nil, fmt.Errorf("wrong primary key length: %v", pk)
	}
	return s.retrieveAssertion(bs, assertType, pk)
}

func (s *Store) assertionsEndpoint(w http.ResponseWriter, req *http.Request) {
	typ, pk, err := s.assertTypeAndKey(req.URL.Path)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	bs, err := s.collectAssertions()
	if err != nil {
		http.Error(w, fmt.Sprintf("internal error collecting assertions: %v", err), 500)
		return
	}

	as, err := s.retrieveAssertionWrapper(bs, typ, pk, req.URL.Query())
	if errors.Is(err, &asserts.NotFoundError{}) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(404)
		w.Write([]byte(`{"error-list":[{"code":"not-found","message":"not found"}]}`))
		return
	}
	if err != nil {
		http.Error(w, fmt.Sprintf("cannot retrieve assertion %v: %v", pk, err), 400)
		return
	}

	w.Header().Set("Content-Type", asserts.MediaType)
	w.WriteHeader(200)
	w.Write(asserts.Encode(as))
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

func (s *Store) nonceEndpoint(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(`{"nonce": "blah"}`))
}

func (s *Store) sessionEndpoint(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write([]byte(`{"macaroon": "blahblah"}`))
}

type ChannelRepository struct {
	rootDir string
}

func (cr *ChannelRepository) findSnapChannels(snapDigest string) ([]string, error) {
	dataPath := filepath.Join(cr.rootDir, snapDigest)
	f, err := os.Open(dataPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines, nil
}
