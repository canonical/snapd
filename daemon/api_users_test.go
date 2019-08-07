// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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
	"bytes"
	"fmt"
	"net/http"
	"os/user"
	"path/filepath"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&userSuite{})

// TODO: also pull login/logout tests into this.
// TODO: move to daemon_test package

type userSuite struct {
	apiBaseSuite

	mockUserHome   string
	restoreClassic func()
}

func (s *userSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.restoreClassic = release.MockOnClassic(false)

	s.daemon(c)
	s.mockUserHome = c.MkDir()
	userLookup = mkUserLookup(s.mockUserHome)
}

func (s *userSuite) TearDownTest(c *check.C) {
	s.apiBaseSuite.TearDownTest(c)

	userLookup = user.Lookup
	osutilAddUser = osutil.AddUser

	s.restoreClassic()
}

func mkUserLookup(userHomeDir string) func(string) (*user.User, error) {
	return func(username string) (*user.User, error) {
		cur, err := user.Current()
		cur.Username = username
		cur.HomeDir = userHomeDir
		return cur, err
	}
}

func (s *userSuite) TestPostCreateUserNoSSHKeys(c *check.C) {
	s.userInfoExpectedEmail = "popper@lse.ac.uk"
	s.userInfoResult = &store.User{
		Username:         "karl",
		OpenIDIdentifier: "xxyyzz",
	}
	buf := bytes.NewBufferString(fmt.Sprintf(`{"email": "%s"}`, s.userInfoExpectedEmail))
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `cannot create user for "popper@lse.ac.uk": no ssh keys found`)
}

func (s *userSuite) TestPostCreateUser(c *check.C) {
	expectedUsername := "karl"
	s.userInfoExpectedEmail = "popper@lse.ac.uk"
	s.userInfoResult = &store.User{
		Username:         expectedUsername,
		SSHKeys:          []string{"ssh1", "ssh2"},
		OpenIDIdentifier: "xxyyzz",
	}
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, expectedUsername)
		c.Check(opts.SSHKeys, check.DeepEquals, []string{"ssh1", "ssh2"})
		c.Check(opts.Gecos, check.Equals, "popper@lse.ac.uk,xxyyzz")
		c.Check(opts.Sudoer, check.Equals, false)
		return nil
	}

	buf := bytes.NewBufferString(fmt.Sprintf(`{"email": "%s"}`, s.userInfoExpectedEmail))
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	expected := &userResponseData{
		Username: expectedUsername,
		SSHKeys:  []string{"ssh1", "ssh2"},
	}

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	// user was setup in state
	state := s.d.overlord.State()
	state.Lock()
	user, err := auth.User(state, 1)
	state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, expectedUsername)
	c.Check(user.Email, check.Equals, s.userInfoExpectedEmail)
	c.Check(user.Macaroon, check.NotNil)
	// auth saved to user home dir
	outfile := filepath.Join(s.mockUserHome, ".snap", "auth.json")
	c.Check(osutil.FileExists(outfile), check.Equals, true)
	c.Check(outfile, testutil.FileEquals,
		fmt.Sprintf(`{"id":%d,"username":"%s","email":"%s","macaroon":"%s"}`,
			1, expectedUsername, s.userInfoExpectedEmail, user.Macaroon))
}

func (s *userSuite) setupSigner(accountID string, signerPrivKey asserts.PrivateKey) *assertstest.SigningDB {
	st := s.d.overlord.State()

	signerSigning := s.brands.Register(accountID, signerPrivKey, map[string]interface{}{
		"account-id":   accountID,
		"verification": "verified",
	})
	acctNKey := s.brands.AccountsAndKeys(accountID)

	assertstest.AddMany(s.storeSigning, acctNKey...)
	assertstatetest.AddMany(st, acctNKey...)

	return signerSigning
}

var (
	brandPrivKey, _   = assertstest.GenerateKey(752)
	partnerPrivKey, _ = assertstest.GenerateKey(752)
	unknownPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *userSuite) makeSystemUsers(c *check.C, systemUsers []map[string]interface{}) {
	st := s.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	assertstatetest.AddMany(st, s.storeSigning.StoreAccountKey(""))

	s.setupSigner("my-brand", brandPrivKey)
	s.setupSigner("partner", partnerPrivKey)
	s.setupSigner("unknown", unknownPrivKey)

	model := s.brands.Model("my-brand", "my-model", map[string]interface{}{
		"architecture":          "amd64",
		"gadget":                "pc",
		"kernel":                "pc-kernel",
		"required-snaps":        []interface{}{"required-snap1"},
		"system-user-authority": []interface{}{"my-brand", "partner"},
	})
	// now add model related stuff to the system
	assertstatetest.AddMany(st, model)

	for _, suMap := range systemUsers {
		su, err := s.brands.Signing(suMap["authority-id"].(string)).Sign(asserts.SystemUserType, suMap, nil, "")
		c.Assert(err, check.IsNil)
		su = su.(*asserts.SystemUser)
		// now add system-user assertion to the system
		assertstatetest.AddMany(st, su)
	}
	// create fake device
	err := devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserial",
	})
	c.Assert(err, check.IsNil)
}

var goodUser = map[string]interface{}{
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "foo@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model", "other-model"},
	"name":         "Boring Guy",
	"username":     "guy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var partnerUser = map[string]interface{}{
	"authority-id": "partner",
	"brand-id":     "my-brand",
	"email":        "p@partner.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"name":         "Partner Guy",
	"username":     "partnerguy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var badUser = map[string]interface{}{
	// bad user (not valid for this model)
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "foobar@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"non-of-the-models-i-have"},
	"name":         "Random Gal",
	"username":     "gal",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

var unknownUser = map[string]interface{}{
	"authority-id": "unknown",
	"brand-id":     "my-brand",
	"email":        "x@partner.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"name":         "XGuy",
	"username":     "xguy",
	"password":     "$6$salt$hash",
	"since":        time.Now().Format(time.RFC3339),
	"until":        time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

func (s *userSuite) TestGetUserDetailsFromAssertionHappy(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.overlord.State()

	st.Lock()
	model, err := s.d.overlord.DeviceManager().Model()
	st.Unlock()
	c.Assert(err, check.IsNil)

	// ensure that if we query the details from the assert DB we get
	// the expected user
	username, opts, err := getUserDetailsFromAssertion(st, model, "foo@bar.com")
	c.Check(username, check.Equals, "guy")
	c.Check(opts, check.DeepEquals, &osutil.AddUserOptions{
		Gecos:    "foo@bar.com,Boring Guy",
		Password: "$6$salt$hash",
	})
	c.Check(err, check.IsNil)
}

// FIXME: These tests all look similar, with small deltas. Would be
// nice to transform them into a table that is just the deltas, and
// run on a loop.
func (s *userSuite) TestPostCreateUserFromAssertion(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	// mock the calls that create the user
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, "guy")
		c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		c.Check(opts.ForcePasswordChange, check.Equals, false)
		return nil
	}

	defer func() {
		osutilAddUser = osutil.AddUser
	}()

	// do it!
	buf := bytes.NewBufferString(`{"email": "foo@bar.com","known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	expected := &userResponseData{
		Username: "guy",
	}

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	// ensure the user was added to the state
	st := s.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Check(users, check.HasLen, 1)
}

func (s *userSuite) TestPostCreateUserFromAssertionWithForcePasswordChnage(c *check.C) {
	lusers := []map[string]interface{}{goodUser}
	lusers[0]["force-password-change"] = "true"
	s.makeSystemUsers(c, lusers)

	// mock the calls that create the user
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, "guy")
		c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		c.Check(opts.ForcePasswordChange, check.Equals, true)
		return nil
	}

	defer func() {
		osutilAddUser = osutil.AddUser
	}()

	// do it!
	buf := bytes.NewBufferString(`{"email": "foo@bar.com","known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	expected := &userResponseData{
		Username: "guy",
	}

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)

	// ensure the user was added to the state
	st := s.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Check(users, check.HasLen, 1)
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnown(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser, partnerUser, badUser, unknownUser})
	created := map[string]bool{}
	// mock the calls that create the user
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		switch username {
		case "guy":
			c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		case "partnerguy":
			c.Check(opts.Gecos, check.Equals, "p@partner.com,Partner Guy")
		default:
			c.Logf("unexpected username %q", username)
			c.Fail()
		}
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		created[username] = true
		return nil
	}
	oldLookup := userLookup
	// make sure we report them as non-existing until created
	userLookup = func(username string) (*user.User, error) {
		if created[username] {
			return oldLookup(username)
		}
		return nil, fmt.Errorf("not created yet")
	}

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	// note that we get a list here instead of a single
	// userResponseData item
	c.Check(rsp.Result, check.FitsTypeOf, []userResponseData{})
	seen := map[string]bool{}
	for _, u := range rsp.Result.([]userResponseData) {
		seen[u.Username] = true
		c.Check(u, check.DeepEquals, userResponseData{Username: u.Username})
	}
	c.Check(seen, check.DeepEquals, map[string]bool{
		"guy":        true,
		"partnerguy": true,
	})

	// ensure the user was added to the state
	st := s.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Check(users, check.HasLen, 2)
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnownClassicErrors(c *check.C) {
	restore := release.MockOnClassic(true)
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `cannot create user: device is a classic system`)
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnownButOwnedErrors(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.overlord.State()
	st.Lock()
	_, err := auth.NewUser(st, "username", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `cannot create user: device already managed`)
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnownNoModelError(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	st := s.d.overlord.State()
	// have not model yet
	st.Lock()
	err := devicestatetest.SetDevice(st, &auth.DeviceState{})
	st.Unlock()
	c.Assert(err, check.IsNil)

	// do it!
	buf := bytes.NewBufferString(`{"known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `cannot create user: cannot get model assertion: no state entry for key`)
}

func (s *userSuite) TestPostCreateUserFromAssertionAllKnownButOwned(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	st := s.d.overlord.State()
	st.Lock()
	_, err := auth.NewUser(st, "username", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	// mock the calls that create the user
	created := map[string]bool{}
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, "guy")
		c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		created[username] = true
		return nil
	}
	oldLookup := userLookup
	// make sure we report them as non-existing until created
	userLookup = func(username string) (*user.User, error) {
		if created[username] {
			return oldLookup(username)
		}
		return nil, fmt.Errorf("not created yet")
	}

	// do it!
	buf := bytes.NewBufferString(`{"known":true,"force-managed":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	// note that we get a list here instead of a single
	// userResponseData item
	expected := []userResponseData{
		{Username: "guy"},
	}
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *userSuite) TestUsersEmpty(c *check.C) {
	req, err := http.NewRequest("GET", "/v2/users", nil)
	c.Assert(err, check.IsNil)

	rsp := getUsers(usersCmd, req, nil).(*resp)

	expected := []userResponseData{}
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *userSuite) TestUsersHasUser(c *check.C) {
	st := s.d.overlord.State()
	st.Lock()
	u, err := auth.NewUser(st, "someuser", "mymail@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/users", nil)
	c.Assert(err, check.IsNil)

	rsp := getUsers(usersCmd, req, nil).(*resp)

	expected := []userResponseData{
		{ID: u.ID, Username: u.Username, Email: u.Email},
	}
	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result, check.FitsTypeOf, expected)
	c.Check(rsp.Result, check.DeepEquals, expected)
}

func (s *userSuite) TestSysInfoIsManaged(c *check.C) {
	st := s.d.overlord.State()
	st.Lock()
	_, err := auth.NewUser(st, "someuser", "mymail@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Assert(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	rsp := sysInfo(sysInfoCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeSync)
	c.Check(rsp.Result.(map[string]interface{})["managed"], check.Equals, true)
}

func (s *userSuite) TestSysInfoWorksDegraded(c *check.C) {
	s.d.SetDegradedMode(fmt.Errorf("some error"))

	req, err := http.NewRequest("GET", "/v2/system-info", nil)
	c.Assert(err, check.IsNil)

	rsp := sysInfo(sysInfoCmd, req, nil).(*resp)
	c.Check(rsp.Status, check.Equals, 200)
}
