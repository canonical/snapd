// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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

package tooling_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/tooling"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type toolingSuite struct {
	testutil.BaseTest
	root string

	storeActionsBunchSizes []int
	storeActions           []*store.SnapAction
	curSnaps               [][]*store.CurrentSnap

	assertMaxFormats map[string]int

	tsto *tooling.ToolingStore

	// SeedSnaps helps creating and making available seed snaps
	// (it provides MakeAssertedSnap etc.) for the tests.
	*seedtest.SeedSnaps
}

var _ = Suite(&toolingSuite{})

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)
)

const packageCore = `
name: core
version: 16.04
type: os
`

func (s *toolingSuite) SetUpTest(c *C) {
	s.root = c.MkDir()

	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.tsto = tooling.MockToolingStore(s)

	s.SeedSnaps = &seedtest.SeedSnaps{}
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	assertstest.AddMany(s.StoreSigning, s.Brands.AccountsAndKeys("my-brand")...)

	otherAcct := assertstest.NewAccount(s.StoreSigning, "other", map[string]interface{}{
		"account-id": "other",
	}, "")
	s.StoreSigning.Add(otherAcct)

	// mock the mount cmds (for the extract kernel assets stuff)
	c1 := testutil.MockCommand(c, "mount", "")
	s.AddCleanup(c1.Restore)
	c2 := testutil.MockCommand(c, "umount", "")
	s.AddCleanup(c2.Restore)
}

func (s *toolingSuite) MakeAssertedSnap(c *C, snapYaml string, files [][]string, revision snap.Revision, developerID string) {
	s.SeedSnaps.MakeAssertedSnap(c, snapYaml, files, revision, developerID, s.StoreSigning.Database)
}

func (s *toolingSuite) setupSnaps(c *C, publishers map[string]string, defaultsYaml string) {
	s.MakeAssertedSnap(c, packageCore, nil, snap.R(3), "canonical")
}

func (s *toolingSuite) TestNewToolingStore(c *C) {
	// default
	u, err := url.Parse("https://api.snapcraft.io/")
	c.Assert(err, IsNil)

	tsto, err := tooling.NewToolingStore()
	c.Assert(err, IsNil)

	c.Check(tsto.StoreURL(), DeepEquals, u)
}

func (s *toolingSuite) TestNewToolingStoreUbuntuStoreURL(c *C) {
	u, err := url.Parse("https://api.other")
	c.Assert(err, IsNil)

	os.Setenv("UBUNTU_STORE_URL", "https://api.other")
	defer os.Unsetenv("UBUNTU_STORE_URL")

	tsto, err := tooling.NewToolingStore()
	c.Assert(err, IsNil)

	c.Check(tsto.StoreURL(), DeepEquals, u)
}

func (s *toolingSuite) TestNewToolingStoreInvalidUbuntuStoreURL(c *C) {
	os.Setenv("UBUNTU_STORE_URL", ":/what")
	defer os.Unsetenv("UBUNTU_STORE_URL")

	_, err := tooling.NewToolingStore()
	c.Assert(err, ErrorMatches, `invalid UBUNTU_STORE_URL: .*`)
}

func (s *toolingSuite) TestNewToolingStoreWithAuthFile(c *C) {
	tmpdir := c.MkDir()
	authFn := filepath.Join(tmpdir, "auth.json")
	err := os.WriteFile(authFn, []byte(`{
"macaroon": "MACAROON",
"discharges": ["DISCHARGE"]
}`), 0600)
	c.Assert(err, IsNil)

	os.Setenv("UBUNTU_STORE_AUTH_DATA_FILENAME", authFn)
	defer os.Unsetenv("UBUNTU_STORE_AUTH_DATA_FILENAME")

	tsto, err := tooling.NewToolingStore()
	c.Assert(err, IsNil)
	creds := tsto.Creds()
	u1creds, ok := creds.(*tooling.UbuntuOneCreds)
	c.Assert(ok, Equals, true)
	c.Check(u1creds.User.StoreMacaroon, Equals, "MACAROON")
	c.Check(u1creds.User.StoreDischarges, DeepEquals, []string{"DISCHARGE"})
}

func (s *toolingSuite) TestNewToolingStoreWithBase64AuthFile(c *C) {
	tmpdir := c.MkDir()
	authFn := filepath.Join(tmpdir, "auth7a")
	authObj := []byte(`{
"r": "MACAROON",
"d": "DISCHARGE"
}`)
	enc := []byte(base64.StdEncoding.EncodeToString(authObj))
	err := os.WriteFile(authFn, enc, 0600)
	c.Assert(err, IsNil)

	os.Setenv("UBUNTU_STORE_AUTH_DATA_FILENAME", authFn)
	defer os.Unsetenv("UBUNTU_STORE_AUTH_DATA_FILENAME")

	tsto, err := tooling.NewToolingStore()
	c.Assert(err, IsNil)
	creds := tsto.Creds()
	u1creds, ok := creds.(*tooling.UbuntuOneCreds)
	c.Assert(ok, Equals, true)
	c.Check(u1creds.User.StoreMacaroon, Equals, "MACAROON")
	c.Check(u1creds.User.StoreDischarges, DeepEquals, []string{"DISCHARGE"})
}

func (s *toolingSuite) TestNewToolingStoreWithAuthFileErrors(c *C) {
	tmpdir := c.MkDir()
	authFn := filepath.Join(tmpdir, "creds")

	os.Setenv("UBUNTU_STORE_AUTH_DATA_FILENAME", authFn)
	defer os.Unsetenv("UBUNTU_STORE_AUTH_DATA_FILENAME")

	tests := []struct {
		data string
		err  string
	}{
		{"", `invalid auth file ".*/creds": empty`},
		{" {}", `invalid auth file ".*/creds": missing fields`},
		{" [...", `invalid snapcraft login file ".*/creds": No section: login.ubuntu.com`},
		{`[login.ubuntu.com]
macaroon =
unbound_discharge =
`, `invalid snapcraft login file ".*/creds": empty fields`},
		{"=", `invalid auth file ".*/creds": not a recognizable format`},
	}

	for _, t := range tests {
		err := os.WriteFile(authFn, []byte(t.data), 0600)
		c.Assert(err, IsNil)

		_, err = tooling.NewToolingStore()
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *toolingSuite) TestNewToolingStoreWithAuthFromSnapcraftLoginFile(c *C) {
	tmpdir := c.MkDir()
	authFn := filepath.Join(tmpdir, "auth.json")
	err := os.WriteFile(authFn, []byte(`[login.ubuntu.com]
macaroon = MACAROON
unbound_discharge = DISCHARGE

`), 0600)
	c.Assert(err, IsNil)

	os.Setenv("UBUNTU_STORE_AUTH_DATA_FILENAME", authFn)
	defer os.Unsetenv("UBUNTU_STORE_AUTH_DATA_FILENAME")

	tsto, err := tooling.NewToolingStore()
	c.Assert(err, IsNil)
	creds := tsto.Creds()
	u1creds, ok := creds.(*tooling.UbuntuOneCreds)
	c.Assert(ok, Equals, true)
	c.Check(u1creds.User.StoreMacaroon, Equals, "MACAROON")
	c.Check(u1creds.User.StoreDischarges, DeepEquals, []string{"DISCHARGE"})
}
func (s *toolingSuite) TestNewToolingStoreWithAuthFromEnv(c *C) {
	tests := []struct {
		dat string
		a   store.Authorizer
		err string
	}{
		{dat: `{
"r": "MACAROON",
"d": "DISCHARGE"
}`, a: &tooling.UbuntuOneCreds{User: auth.UserState{
			StoreMacaroon:   "MACAROON",
			StoreDischarges: []string{"DISCHARGE"},
		}}}, {dat: `{ "t": "u1-macaroon",
"v": {
  "r": "MACAROON",
  "d": "DISCHARGE"
}}`, a: &tooling.UbuntuOneCreds{User: auth.UserState{
			StoreMacaroon:   "MACAROON",
			StoreDischarges: []string{"DISCHARGE"},
		}}}, {dat: `{`, err: `cannot unmarshal base64-decoded auth credentials from UBUNTU_STORE_AUTH: unexpected end of JSON input`}, {dat: `{}`, err: `cannot recognize unmarshalled base64-decoded auth credentials from UBUNTU_STORE_AUTH: no known field combination set`}, {dat: `{ "t": "macaroon",
"v": "MACAROON0"
}`, a: &tooling.SimpleCreds{
			Scheme: "Macaroon",
			Value:  "MACAROON0",
		}}, {dat: `{ "t": "bearer",
"v": "tok"
}`, a: &tooling.SimpleCreds{
			Scheme: "Bearer",
			Value:  "tok",
		}}, {dat: `{"t": "u1-macaroon"}`,
			err: `cannot recognize unmarshalled base64-decoded auth credentials from UBUNTU_STORE_AUTH: no known field combination set`,
		}, {dat: `{"t": "macaroon"}`,
			err: `cannot recognize unmarshalled base64-decoded auth credentials from UBUNTU_STORE_AUTH: no known field combination set`,
		}, {dat: `{"t": 1}`,
			err: `cannot recognize unmarshalled base64-decoded auth credentials from UBUNTU_STORE_AUTH: no known field combination set`,
		}, {dat: `{"t": "macaroon", "v": []}`,
			err: `cannot recognize unmarshalled base64-decoded auth credentials from UBUNTU_STORE_AUTH: no known field combination set`,
		}}
	defer os.Unsetenv("UBUNTU_STORE_AUTH")

	for _, t := range tests {
		os.Setenv("UBUNTU_STORE_AUTH", base64.StdEncoding.EncodeToString([]byte(t.dat)))
		tsto, err := tooling.NewToolingStore()
		if t.err == "" {
			c.Assert(err, IsNil)
			creds := tsto.Creds()
			c.Check(creds, DeepEquals, t.a)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *toolingSuite) TestDownloadpOptionsString(c *C) {
	tests := []struct {
		opts tooling.DownloadSnapOptions
		str  string
	}{
		{tooling.DownloadSnapOptions{LeavePartialOnError: true}, ""},
		{tooling.DownloadSnapOptions{}, ""},
		{tooling.DownloadSnapOptions{TargetDir: "/foo"}, `in "/foo"`},
		{tooling.DownloadSnapOptions{Basename: "foo"}, `to "foo.snap"`},
		{tooling.DownloadSnapOptions{Channel: "foo"}, `from channel "foo"`},
		{tooling.DownloadSnapOptions{Revision: snap.R(42)}, `(42)`},
		{tooling.DownloadSnapOptions{
			CohortKey: "AbCdEfGhIjKlMnOpQrStUvWxYz",
		}, `from cohort "…rStUvWxYz"`},
		{tooling.DownloadSnapOptions{
			TargetDir: "/foo",
			Basename:  "bar",
			Channel:   "baz",
			Revision:  snap.R(13),
			CohortKey: "MSBIc3dwOW9PemozYjRtdzhnY0MwMFh0eFduS0g5UWlDUSAxNTU1NDExNDE1IDBjYzJhNTc1ZjNjOTQ3ZDEwMWE1NTNjZWFkNmFmZDE3ZWJhYTYyNjM4ZWQ3ZGMzNjI5YmU4YjQ3NzAwMjdlMDk=",
		}, `(13) from channel "baz" from cohort "…wMjdlMDk=" to "bar.snap" in "/foo"`}, // note this one is not 'valid' so it's ok if the string is a bit wonky

	}

	for _, t := range tests {
		c.Check(t.opts.String(), Equals, t.str)
	}
}

func (s *toolingSuite) TestDownloadSnapOptionsValid(c *C) {
	tests := []struct {
		opts tooling.DownloadSnapOptions
		err  error
	}{
		{tooling.DownloadSnapOptions{}, nil}, // might want to error if no targetdir
		{tooling.DownloadSnapOptions{TargetDir: "foo"}, nil},
		{tooling.DownloadSnapOptions{Channel: "foo"}, nil},
		{tooling.DownloadSnapOptions{Revision: snap.R(42)}, nil},
		{tooling.DownloadSnapOptions{
			CohortKey: "AbCdEfGhIjKlMnOpQrStUvWxYz",
		}, nil},
		{tooling.DownloadSnapOptions{
			Channel:  "foo",
			Revision: snap.R(42),
		}, nil},
		{tooling.DownloadSnapOptions{
			Channel:   "foo",
			CohortKey: "bar",
		}, nil},
		{tooling.DownloadSnapOptions{
			Revision:  snap.R(1),
			CohortKey: "bar",
		}, tooling.ErrRevisionAndCohort},
		{tooling.DownloadSnapOptions{
			Basename: "/foo",
		}, tooling.ErrPathInBase},
	}

	for _, t := range tests {
		t.opts.LeavePartialOnError = true
		c.Check(t.opts.Validate(), Equals, t.err)
		t.opts.LeavePartialOnError = false
		c.Check(t.opts.Validate(), Equals, t.err)
	}
}

func (s *toolingSuite) TestDownloadSnap(c *C) {
	// TODO: maybe expand on this (test coverage of DownloadSnap is really bad)

	// env shenanigans
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	debug, hadDebug := os.LookupEnv("SNAPD_DEBUG")
	os.Setenv("SNAPD_DEBUG", "1")
	if hadDebug {
		defer os.Setenv("SNAPD_DEBUG", debug)
	} else {
		defer os.Unsetenv("SNAPD_DEBUG")
	}
	logbuf, restore := logger.MockLogger()
	defer restore()

	s.setupSnaps(c, map[string]string{
		"core": "canonical",
	}, "")

	dlDir := c.MkDir()
	opts := tooling.DownloadSnapOptions{
		TargetDir: dlDir,
	}
	dlSnap, err := s.tsto.DownloadSnap("core", opts)
	c.Assert(err, IsNil)
	c.Check(dlSnap.Path, Matches, filepath.Join(dlDir, `core_\d+.snap`))
	c.Check(dlSnap.Info.SnapName(), Equals, "core")
	c.Check(dlSnap.RedirectChannel, Equals, "")

	c.Check(logbuf.String(), Matches, `.* DEBUG: Going to download snap "core" `+opts.String()+".\n")
}

func (s *toolingSuite) TestSetAssertionMaxFormats(c *C) {
	c.Check(s.tsto.AssertionMaxFormats(), IsNil)

	m := map[string]int{
		"snap-declaration": 4,
	}
	s.tsto.SetAssertionMaxFormats(m)
	c.Check(s.tsto.AssertionMaxFormats(), DeepEquals, m)
	c.Check(s.assertMaxFormats, DeepEquals, m)

	s.tsto.SetAssertionMaxFormats(nil)
	c.Check(s.tsto.AssertionMaxFormats(), IsNil)
	c.Check(s.assertMaxFormats, IsNil)
}

// interface for the store
func (s *toolingSuite) SnapAction(_ context.Context, curSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, _ *auth.UserState, _ *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	if assertQuery != nil {
		return nil, nil, fmt.Errorf("unexpected assertion query")
	}

	s.storeActionsBunchSizes = append(s.storeActionsBunchSizes, len(actions))
	s.curSnaps = append(s.curSnaps, curSnaps)
	sars := make([]store.SnapActionResult, 0, len(actions))
	for _, a := range actions {
		if a.Action != "download" {
			return nil, nil, fmt.Errorf("unexpected action %q", a.Action)
		}

		if _, instanceKey := snap.SplitInstanceName(a.InstanceName); instanceKey != "" {
			return nil, nil, fmt.Errorf("unexpected instance key in %q", a.InstanceName)
		}
		// record
		s.storeActions = append(s.storeActions, a)

		info := s.AssertedSnapInfo(a.InstanceName)
		if info == nil {
			return nil, nil, fmt.Errorf("no %q in the fake store", a.InstanceName)
		}
		info1 := *info
		channel := a.Channel
		redirectChannel := ""
		if strings.HasPrefix(a.InstanceName, "default-track-") {
			channel = "default-track/stable"
			redirectChannel = channel
		}
		info1.Channel = channel
		sars = append(sars, store.SnapActionResult{
			Info:            &info1,
			RedirectChannel: redirectChannel,
		})
	}

	return sars, nil, nil
}

func (s *toolingSuite) Download(ctx context.Context, name, targetFn string, downloadInfo *snap.DownloadInfo, pbar progress.Meter, user *auth.UserState, dlOpts *store.DownloadOptions) error {
	return osutil.CopyFile(s.AssertedSnap(name), targetFn, 0)
}

func (s *toolingSuite) SetAssertionMaxFormats(m map[string]int) {
	s.assertMaxFormats = m
}

func (s *toolingSuite) Assertion(assertType *asserts.AssertionType, primaryKey []string, user *auth.UserState) (asserts.Assertion, error) {
	ref := &asserts.Ref{Type: assertType, PrimaryKey: primaryKey}
	return ref.Resolve(s.StoreSigning.Find)
}

func (s *toolingSuite) SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	if sequence <= 0 {
		panic("unexpected call to SeqFormingAssertion with unspecified sequence")
	}

	seq := &asserts.AtSequence{
		Type:        assertType,
		SequenceKey: sequenceKey,
		Sequence:    sequence,
		Revision:    asserts.RevisionNotKnown,
	}
	return seq.Resolve(s.StoreSigning.Find)
}

func (s *toolingSuite) TestUpdateUserAuth(c *C) {
	u := auth.UserState{
		StoreMacaroon:   "macaroon",
		StoreDischarges: []string{"discharge1"},
	}
	creds := &tooling.UbuntuOneCreds{
		User: u,
	}

	u1, err := creds.UpdateUserAuth(&u, []string{"discharge2"})
	c.Assert(err, IsNil)
	c.Check(u1, Equals, &u)
	c.Check(u1.StoreDischarges, DeepEquals, []string{"discharge2"})
}

func (s *toolingSuite) TestSimpleCreds(c *C) {
	creds := &tooling.SimpleCreds{
		Scheme: "Auth-Scheme",
		Value:  "auth-value",
	}
	c.Check(creds.CanAuthorizeForUser(nil), Equals, true)
	r, err := http.NewRequest("POST", "http://svc", nil)
	c.Assert(err, IsNil)
	c.Assert(creds.Authorize(r, nil, nil, nil), IsNil)
	auth := r.Header.Get("Authorization")
	c.Check(auth, Equals, `Auth-Scheme auth-value`)
}

func (s *toolingSuite) setupSequenceFormingAssertion(c *C) {
	vs, err := s.StoreSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "base-set",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc",
				"id":       "idididididididididididididididid",
				"presence": "required",
				"revision": "1",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.StoreSigning.Add(vs)
	c.Check(err, IsNil)
}

func (s *toolingSuite) TestAssertionSequenceFormingFetcherSimple(c *C) {
	s.setupSequenceFormingAssertion(c)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.StoreSigning.Trusted,
	})
	c.Assert(err, IsNil)

	// Add in prereqs
	err = db.Add(s.StoreSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	var saveCalled int
	sf := s.tsto.AssertionSequenceFormingFetcher(db, func(a asserts.Assertion) error {
		saveCalled++
		return nil
	})
	c.Check(sf, NotNil)

	seq := &asserts.AtSequence{
		Type:        asserts.ValidationSetType,
		SequenceKey: []string{"16", "canonical", "base-set"},
		Sequence:    1,
	}

	err = sf.FetchSequence(seq)
	c.Check(err, IsNil)
	c.Check(saveCalled, Equals, 1)

	// Verify it was put into the database
	vsa, err := seq.Resolve(db.Find)
	c.Assert(err, IsNil)
	c.Check(vsa.(*asserts.ValidationSet).Name(), Equals, "base-set")
	c.Check(vsa.(*asserts.ValidationSet).Sequence(), Equals, 1)
}
