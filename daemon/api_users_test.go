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
	oldUserAdmin   bool
}

func (s *userSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.restoreClassic = release.MockOnClassic(false)

	s.daemon(c)
	s.mockUserHome = c.MkDir()
	userLookup = mkUserLookup(s.mockUserHome)
	s.oldUserAdmin = hasUserAdmin
	hasUserAdmin = true

	// make sure we don't call these by accident
	osutilAddUser = func(name string, opts *osutil.AddUserOptions) error {
		c.Fatalf("unexpected add user %q call", name)
		return fmt.Errorf("unexpected add user %q call", name)
	}
	osutilDelUser = func(name string, opts *osutil.DelUserOptions) error {
		c.Fatalf("unexpected del user %q call", name)
		return fmt.Errorf("unexpected del user %q call", name)
	}
}

func (s *userSuite) TearDownTest(c *check.C) {
	s.apiBaseSuite.TearDownTest(c)

	userLookup = user.Lookup
	osutilAddUser = osutil.AddUser
	osutilDelUser = osutil.DelUser

	s.restoreClassic()
	hasUserAdmin = s.oldUserAdmin
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
	s.testCreateUser(c, true)
}

func (s *userSuite) TestPostUserCreate(c *check.C) {
	s.testCreateUser(c, false)
}

func (s *userSuite) testCreateUser(c *check.C, oldWay bool) {
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

	var rsp *resp
	var expected interface{}
	expectedItem := userResponseData{
		Username: expectedUsername,
		SSHKeys:  []string{"ssh1", "ssh2"},
	}

	if oldWay {
		buf := bytes.NewBufferString(fmt.Sprintf(`{"email": "%s"}`, s.userInfoExpectedEmail))
		req, err := http.NewRequest("POST", "/v2/create-user", buf)
		c.Assert(err, check.IsNil)

		rsp = postCreateUser(createUserCmd, req, nil).(*resp)
		expected = &expectedItem
	} else {
		buf := bytes.NewBufferString(fmt.Sprintf(`{"action":"create","email": "%s"}`, s.userInfoExpectedEmail))
		req, err := http.NewRequest("POST", "/v2/users", buf)
		c.Assert(err, check.IsNil)

		rsp = postUsers(usersCmd, req, nil).(*resp)
		expected = []userResponseData{expectedItem}
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

func (s *userSuite) TestNoUserAdminCreateUser(c *check.C) { s.testNoUserAdmin(c, "/v2/create-user") }
func (s *userSuite) TestNoUserAdminPostUser(c *check.C)   { s.testNoUserAdmin(c, "/v2/users") }
func (s *userSuite) testNoUserAdmin(c *check.C, endpoint string) {
	hasUserAdmin = false

	buf := bytes.NewBufferString("{}")
	req, err := http.NewRequest("POST", endpoint, buf)
	c.Assert(err, check.IsNil)

	switch endpoint {
	case "/v2/users":
		rsp := postUsers(usersCmd, req, nil).(*resp)
		c.Check(rsp, check.DeepEquals, MethodNotAllowed(noUserAdmin))
	case "/v2/create-user":
		rsp := postCreateUser(createUserCmd, req, nil).(*resp)
		c.Check(rsp, check.DeepEquals, Forbidden(noUserAdmin))
	default:
		c.Fatalf("unknown endpoint %q", endpoint)
	}
}

func (s *userSuite) TestPostUserBadBody(c *check.C) {
	buf := bytes.NewBufferString(`42`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, "cannot decode user action data from request body: .*")
}

func (s *userSuite) TestPostUserBadAfterBody(c *check.C) {
	buf := bytes.NewBufferString(`{}42`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp, check.DeepEquals, BadRequest("spurious content after user action"))
}

func (s *userSuite) TestPostUserNoAction(c *check.C) {
	buf := bytes.NewBufferString("{}")
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp, check.DeepEquals, BadRequest("missing user action"))
}

func (s *userSuite) TestPostUserBadAction(c *check.C) {
	buf := bytes.NewBufferString(`{"action":"patatas"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp, check.DeepEquals, BadRequest(`unsupported user action "patatas"`))
}

func (s *userSuite) TestPostUserActionRemoveNoUsername(c *check.C) {
	buf := bytes.NewBufferString(`{"action":"remove"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp, check.DeepEquals, BadRequest("need a username to remove"))
}

func (s *userSuite) TestPostUserActionRemoveDelUserErr(c *check.C) {
	st := s.d.overlord.State()
	st.Lock()
	_, err := auth.NewUser(st, "some-user", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	called := 0
	osutilDelUser = func(username string, opts *osutil.DelUserOptions) error {
		called++
		c.Check(username, check.Equals, "some-user")
		return fmt.Errorf("wat")
	}

	buf := bytes.NewBufferString(`{"action":"remove","username":"some-user"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp.Status, check.Equals, 500)
	c.Check(rsp.Result.(*errorResult).Message, check.Equals, "wat")
	c.Check(called, check.Equals, 1)
}

func (s *userSuite) TestPostUserActionRemoveStateErr(c *check.C) {
	st := s.d.overlord.State()
	st.Lock()
	st.Set("auth", 42) // breaks auth
	st.Unlock()
	called := 0
	osutilDelUser = func(username string, opts *osutil.DelUserOptions) error {
		called++
		c.Check(username, check.Equals, "some-user")
		return nil
	}

	buf := bytes.NewBufferString(`{"action":"remove","username":"some-user"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp.Status, check.Equals, 500)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `internal error: could not unmarshal state entry "auth": .*`)
	c.Check(called, check.Equals, 0)
}

func (s *userSuite) TestPostUserActionRemoveNoUserInState(c *check.C) {
	called := 0
	osutilDelUser = func(username string, opts *osutil.DelUserOptions) error {
		called++
		c.Check(username, check.Equals, "some-user")
		return nil
	}

	buf := bytes.NewBufferString(`{"action":"remove","username":"some-user"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)

	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp, check.DeepEquals, BadRequest(`user "some-user" is not known`))
	c.Check(called, check.Equals, 0)
}

func (s *userSuite) TestPostUserActionRemove(c *check.C) {
	st := s.d.overlord.State()
	st.Lock()
	user, err := auth.NewUser(st, "some-user", "email@test.com", "macaroon", []string{"discharge"})
	st.Unlock()
	c.Check(err, check.IsNil)

	called := 0
	osutilDelUser = func(username string, opts *osutil.DelUserOptions) error {
		called++
		c.Check(username, check.Equals, "some-user")
		return nil
	}

	buf := bytes.NewBufferString(`{"action":"remove","username":"some-user"}`)
	req, err := http.NewRequest("POST", "/v2/users", buf)
	c.Assert(err, check.IsNil)
	rsp := postUsers(usersCmd, req, nil).(*resp)
	c.Check(rsp.Status, check.Equals, 200)
	expected := []userResponseData{
		{ID: user.ID, Username: user.Username, Email: user.Email},
	}
	c.Check(rsp.Result, check.DeepEquals, map[string]interface{}{
		"removed": expected,
	})
	c.Check(called, check.Equals, 1)

	// and the user is removed from state
	st.Lock()
	_, err = auth.User(st, user.ID)
	st.Unlock()
	c.Check(err, check.Equals, auth.ErrInvalidUser)
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
	// and a serial
	deviceKey, _ := assertstest.GenerateKey(752)
	encDevKey, err := asserts.EncodePublicKey(deviceKey.PublicKey())
	c.Assert(err, check.IsNil)
	serial, err := s.brands.Signing("my-brand").Sign(asserts.SerialType, map[string]interface{}{
		"authority-id":        "my-brand",
		"brand-id":            "my-brand",
		"model":               "my-model",
		"serial":              "serialserial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	assertstatetest.AddMany(st, serial)

	for _, suMap := range systemUsers {
		su, err := s.brands.Signing(suMap["authority-id"].(string)).Sign(asserts.SystemUserType, suMap, nil, "")
		c.Assert(err, check.IsNil)
		su = su.(*asserts.SystemUser)
		// now add system-user assertion to the system
		assertstatetest.AddMany(st, su)
	}
	// create fake device
	err = devicestatetest.SetDevice(st, &auth.DeviceState{
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

var serialUser = map[string]interface{}{
	"format":       "1",
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "serial@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"serials":      []interface{}{"serialserial"},
	"name":         "Serial Guy",
	"username":     "goodserialguy",
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

var badUserNoMatchingSerial = map[string]interface{}{
	"format":       "1",
	"authority-id": "my-brand",
	"brand-id":     "my-brand",
	"email":        "noserial@bar.com",
	"series":       []interface{}{"16", "18"},
	"models":       []interface{}{"my-model"},
	"serials":      []interface{}{"different-serialserial"},
	"name":         "No Serial Guy",
	"username":     "noserial",
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
	username, opts, err := getUserDetailsFromAssertion(st, model, nil, "foo@bar.com")
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

func (s *userSuite) TestPostCreateUserFromAssertionWithForcePasswordChange(c *check.C) {
	user := make(map[string]interface{})
	for k, v := range goodUser {
		user[k] = v
	}
	user["force-password-change"] = "true"
	lusers := []map[string]interface{}{user}
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
	s.makeSystemUsers(c, []map[string]interface{}{goodUser, partnerUser, serialUser, badUser, badUserNoMatchingSerial, unknownUser})
	created := map[string]bool{}
	// mock the calls that create the user
	osutilAddUser = func(username string, opts *osutil.AddUserOptions) error {
		switch username {
		case "guy":
			c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		case "partnerguy":
			c.Check(opts.Gecos, check.Equals, "p@partner.com,Partner Guy")
		case "goodserialguy":
			c.Check(opts.Gecos, check.Equals, "serial@bar.com,Serial Guy")
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
		"guy":           true,
		"partnerguy":    true,
		"goodserialguy": true,
	})

	// ensure the user was added to the state
	st := s.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	c.Assert(err, check.IsNil)
	st.Unlock()
	c.Check(users, check.HasLen, 3)
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

func (s *userSuite) TestPostCreateUserFromAssertionNoModel(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	model := s.brands.Model("my-brand", "other-model", map[string]interface{}{
		"architecture":          "amd64",
		"gadget":                "pc",
		"kernel":                "pc-kernel",
		"system-user-authority": []interface{}{"my-brand", "partner"},
	})
	s.makeSystemUsers(c, []map[string]interface{}{serialUser})

	st := s.d.overlord.State()
	st.Lock()
	assertstatetest.AddMany(st, model)
	err := devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "other-serial-assertion",
	})
	st.Unlock()
	c.Assert(err, check.IsNil)

	// do it!
	buf := bytes.NewBufferString(`{"email":"serial@bar.com", "known":true}`)
	req, err := http.NewRequest("POST", "/v2/create-user", buf)
	c.Assert(err, check.IsNil)

	rsp := postCreateUser(createUserCmd, req, nil).(*resp)

	c.Check(rsp.Type, check.Equals, ResponseTypeError)
	c.Check(rsp.Result.(*errorResult).Message, check.Matches, `cannot add system-user "serial@bar.com": bound to serial assertion but device not yet registered`)
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
