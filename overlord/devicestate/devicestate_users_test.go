// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022-2024 Canonical Ltd
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

package devicestate_test

import (
	"fmt"
	"os/user"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

var _ = check.Suite(&usersSuite{})

type usersSuite struct {
	deviceMgrBaseSuite

	storetest.Store

	err                   error
	userInfoResult        *store.User
	userInfoExpectedEmail string

	mockUserHome      string
	trivialUserLookup func(username string) (*user.User, error)

	StoreSigning *assertstest.StoreStack
}

func (s *usersSuite) UserInfo(email string) (userinfo *store.User, err error) {
	// poke state lock
	s.state.Lock()
	s.state.Unlock()

	if s.userInfoExpectedEmail != email {
		panic(fmt.Sprintf("%q != %q", s.userInfoExpectedEmail, email))
	}
	return s.userInfoResult, s.err
}

func (s *usersSuite) errorIsInternal(err error) bool {
	_, ok := err.(*devicestate.UserError)
	return !ok
}

func (s *usersSuite) SetUpTest(c *check.C) {
	classic := false
	s.setupBaseTest(c, classic)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	s.mockUserHome = c.MkDir()
	s.trivialUserLookup = mkUserLookup(s.mockUserHome)
	s.AddCleanup(devicestate.MockUserLookup(s.trivialUserLookup))

	s.state.Lock()
	snapstate.ReplaceStore(s.state, s)
	s.state.Unlock()

	// make sure we don't call these by accident
	s.AddCleanup(devicestate.MockOsutilAddUser(func(name string, opts *osutil.AddUserOptions) error {
		c.Fatalf("unexpected add user %q call", name)
		return fmt.Errorf("unexpected add user %q call", name)
	}))
	s.AddCleanup(devicestate.MockOsutilDelUser(func(name string, opts *osutil.DelUserOptions) error {
		c.Fatalf("unexpected del user %q call", name)
		return fmt.Errorf("unexpected del user %q call", name)
	}))

	s.userInfoResult = nil
	s.userInfoExpectedEmail = ""
}

func mkUserLookup(userHomeDir string) func(string) (*user.User, error) {
	return func(username string) (*user.User, error) {
		cur, err := user.Current()
		cur.Username = username
		cur.HomeDir = userHomeDir
		return cur, err
	}
}

func (s *usersSuite) TestCreateUserNoSSHKeys(c *check.C) {
	s.userInfoExpectedEmail = "popper@lse.ac.uk"
	s.userInfoResult = &store.User{
		Username:         "karl",
		OpenIDIdentifier: "xxyyzz",
	}

	// create user
	s.state.Lock()
	createdUser, err := devicestate.CreateUser(s.state, true, "popper@lse.ac.uk", time.Time{})
	s.state.Unlock()

	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Matches, `cannot create user "popper@lse.ac.uk": no ssh keys found`)
	c.Check(s.errorIsInternal(err), check.Equals, false)
	c.Check(createdUser, check.IsNil)
}

func (s *usersSuite) TestCreateUser(c *check.C) {
	expectedUsername := "karl"
	expectedExpiration := time.Now().Add(time.Hour).Round(time.Second)
	s.userInfoExpectedEmail = "popper@lse.ac.uk"
	s.userInfoResult = &store.User{
		Username:         expectedUsername,
		SSHKeys:          []string{"ssh1", "ssh2"},
		OpenIDIdentifier: "xxyyzz",
	}

	var addUserCalled bool
	defer devicestate.MockOsutilAddUser(func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, expectedUsername)
		c.Check(opts.SSHKeys, check.DeepEquals, []string{
			`ssh1 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
			`ssh2 # snapd {"origin":"store","email":"popper@lse.ac.uk"}`,
		})
		c.Check(opts.Gecos, check.Equals, "popper@lse.ac.uk,xxyyzz")
		c.Check(opts.Sudoer, check.Equals, false)
		addUserCalled = true
		return nil
	})()

	// user was setup in state
	// create user
	s.state.Lock()
	createdUser, userErr := devicestate.CreateUser(s.state, false, "popper@lse.ac.uk", expectedExpiration)
	s.state.Unlock()

	c.Assert(userErr, check.IsNil)
	expected := &devicestate.CreatedUser{
		Username: "karl",
		SSHKeys:  []string{"ssh1 # snapd {\"origin\":\"store\",\"email\":\"popper@lse.ac.uk\"}", "ssh2 # snapd {\"origin\":\"store\",\"email\":\"popper@lse.ac.uk\"}"},
	}
	c.Check(createdUser, check.FitsTypeOf, expected)
	c.Check(createdUser, check.DeepEquals, expected)

	s.state.Lock()
	user, err := auth.User(s.state, 1)
	s.state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(user.Username, check.Equals, expectedUsername)
	c.Check(user.Email, check.Equals, s.userInfoExpectedEmail)
	c.Check(user.Macaroon, check.NotNil)
	c.Check(addUserCalled, check.Equals, true)
	c.Check(user.Expiration.Equal(expectedExpiration), check.Equals, true)
	// auth saved to user home dir
	outfile := filepath.Join(s.mockUserHome, ".snap", "auth.json")
	c.Check(osutil.FileExists(outfile), check.Equals, true)
	c.Check(outfile, testutil.FileEquals,
		fmt.Sprintf(`{"id":%d,"username":"%s","email":"%s","macaroon":"%s"}`,
			1, expectedUsername, s.userInfoExpectedEmail, user.Macaroon))
}

func (s *usersSuite) TestUserActionRemoveDelUserErr(c *check.C) {
	s.state.Lock()
	_, err := auth.NewUser(s.state, auth.NewUserParams{
		Username:   "some-user",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	s.state.Unlock()
	c.Check(err, check.IsNil)

	called := 0
	defer devicestate.MockOsutilDelUser(func(username string, opts *osutil.DelUserOptions) error {
		called++
		c.Check(username, check.Equals, "some-user")
		return fmt.Errorf("wat")
	})()

	s.state.Lock()
	userState, userErr := devicestate.RemoveUser(s.state, "some-user", &devicestate.RemoveUserOptions{})
	s.state.Unlock()
	c.Check(userErr, check.NotNil)
	c.Check(s.errorIsInternal(err), check.Equals, true)
	c.Check(userErr.Error(), check.Matches, "wat")
	c.Assert(userState, check.IsNil)
	c.Check(called, check.Equals, 1)
}

func (s *usersSuite) TestUserActionRemoveDelUserForce(c *check.C) {
	s.state.Lock()
	_, err := auth.NewUser(s.state, auth.NewUserParams{
		Username:   "some-user",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	s.state.Unlock()
	c.Check(err, check.IsNil)

	calls := 0
	defer devicestate.MockOsutilDelUser(func(username string, opts *osutil.DelUserOptions) error {
		calls++
		c.Check(username, check.Equals, "some-user")
		c.Check(opts.Force, check.Equals, true)
		return nil
	})()

	s.state.Lock()
	_, err = devicestate.RemoveUser(s.state, "some-user", &devicestate.RemoveUserOptions{Force: true})
	s.state.Unlock()
	c.Check(err, check.IsNil)
	c.Check(calls, check.Equals, 1)
}

func (s *usersSuite) TestUserActionRemoveStateErr(c *check.C) {
	s.state.Lock()
	s.state.Set("auth", 42) // breaks auth
	s.state.Unlock()
	called := 0
	defer devicestate.MockOsutilDelUser(func(username string, opts *osutil.DelUserOptions) error {
		called++
		c.Check(username, check.Equals, "some-user")
		return nil
	})()

	s.state.Lock()
	userState, err := devicestate.RemoveUser(s.state, "some-user", &devicestate.RemoveUserOptions{})
	s.state.Unlock()

	c.Check(err, check.NotNil)
	c.Check(s.errorIsInternal(err), check.Equals, true)
	c.Check(err.Error(), check.Matches, `internal error: could not unmarshal state entry "auth": .*`)
	c.Assert(userState, check.IsNil)
	c.Check(called, check.Equals, 0)
}

func (s *usersSuite) TestUserActionRemoveNoUserInState(c *check.C) {
	called := 0
	defer devicestate.MockOsutilDelUser(func(username string, opts *osutil.DelUserOptions) error {
		called++
		c.Check(username, check.Equals, "some-user")
		return nil
	})

	s.state.Lock()
	userState, err := devicestate.RemoveUser(s.state, "some-user", &devicestate.RemoveUserOptions{})
	s.state.Unlock()

	c.Check(err, check.NotNil)
	c.Check(s.errorIsInternal(err), check.Equals, false)
	c.Check(err.Error(), check.Matches, `user "some-user" is not known`)
	c.Assert(userState, check.IsNil)
	c.Check(called, check.Equals, 0)
}

func (s *usersSuite) TestUserActionRemove(c *check.C) {
	s.state.Lock()
	user, err := auth.NewUser(s.state, auth.NewUserParams{
		Username:   "some-user",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	s.state.Unlock()
	c.Check(err, check.IsNil)

	called := 0
	defer devicestate.MockOsutilDelUser(func(username string, opts *osutil.DelUserOptions) error {
		called++
		c.Check(username, check.Equals, "some-user")
		return nil
	})()

	s.state.Lock()
	userState, err := devicestate.RemoveUser(s.state, "some-user", &devicestate.RemoveUserOptions{})
	s.state.Unlock()

	c.Check(err, check.IsNil)
	expected := &auth.UserState{ID: user.ID, Username: user.Username, Email: user.Email}
	c.Check(userState, check.FitsTypeOf, expected)
	c.Check(userState, check.DeepEquals, expected)
	c.Check(called, check.Equals, 1)

	// and the user is removed from state
	s.state.Lock()
	_, err = auth.User(s.state, user.ID)
	s.state.Unlock()
	c.Check(err, check.Equals, auth.ErrInvalidUser)
}

func (s *usersSuite) TestUserActionRemoveNoUsername(c *check.C) {

	userState, err := devicestate.RemoveUser(s.state, "", &devicestate.RemoveUserOptions{})
	c.Check(err, check.NotNil)
	c.Check(err.Error(), check.Matches, "need a username to remove")
	c.Check(s.errorIsInternal(err), check.Equals, false)
	c.Check(userState, check.IsNil)
}

func (s *usersSuite) setupSigner(accountID string, signerPrivKey asserts.PrivateKey) *assertstest.SigningDB {

	signerSigning := s.brands.Register(accountID, signerPrivKey, map[string]interface{}{
		"account-id":   accountID,
		"verification": "verified",
	})
	acctNKey := s.brands.AccountsAndKeys(accountID)

	assertstest.AddMany(s.storeSigning, acctNKey...)
	assertstatetest.AddMany(s.state, acctNKey...)

	return signerSigning
}

var (
	partnerPrivKey, _ = assertstest.GenerateKey(752)
	unknownPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *usersSuite) makeSystemUsers(c *check.C, systemUsers []map[string]interface{}) {
	s.state.Lock()
	defer s.state.Unlock()

	assertstatetest.AddMany(s.state, s.storeSigning.StoreAccountKey(""))

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
	assertstatetest.AddMany(s.state, model)
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
	assertstatetest.AddMany(s.state, serial)

	for _, suMap := range systemUsers {
		su, err := s.brands.Signing(suMap["authority-id"].(string)).Sign(asserts.SystemUserType, suMap, nil, "")
		c.Assert(err, check.IsNil)
		su = su.(*asserts.SystemUser)
		// now add system-user assertion to the system
		assertstatetest.AddMany(s.state, su)
	}
	// create fake device
	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
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

var expireUser = map[string]interface{}{
	"format":        "2",
	"authority-id":  "my-brand",
	"brand-id":      "my-brand",
	"email":         "foo@bar.com",
	"series":        []interface{}{"16", "18"},
	"models":        []interface{}{"my-model", "other-model"},
	"name":          "Boring Guy",
	"username":      "guy",
	"password":      "$6$salt$hash",
	"user-presence": "until-expiration",
	"since":         time.Now().Format(time.RFC3339),
	"until":         time.Now().Add(24 * 30 * time.Hour).Format(time.RFC3339),
}

func (s *usersSuite) TestGetUserDetailsFromAssertionHappy(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	s.state.Lock()
	model, err := s.mgr.Model()
	db := assertstate.DB(s.state)
	s.state.Unlock()
	c.Assert(err, check.IsNil)

	// ensure that if we query the details from the assert DB we get
	// the expected user
	username, expiration, opts, err := devicestate.GetUserDetailsFromAssertion(db, model, nil, "foo@bar.com")
	c.Assert(err, check.IsNil)
	c.Check(username, check.Equals, "guy")
	c.Check(opts, check.DeepEquals, &osutil.AddUserOptions{
		Gecos:    "foo@bar.com,Boring Guy",
		Password: "$6$salt$hash",
	})
	c.Check(expiration.IsZero(), check.Equals, true)
}

func (s *usersSuite) TestCreateUserFromAssertion(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})
	s.createUserFromAssertion(c, false)
}

func (s *usersSuite) TestCreateUserExpireFromAssertion(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{expireUser})
	users := s.createUserFromAssertion(c, false)
	c.Assert(len(users), check.Equals, 1)
	until, err := time.Parse(time.RFC3339, expireUser["until"].(string))
	c.Assert(err, check.IsNil)
	c.Check(users[0].Expiration.Equal(until), check.Equals, true)
}

func (s *usersSuite) TestCreateUserFromAssertionWithForcePasswordChange(c *check.C) {
	user := make(map[string]interface{})
	for k, v := range goodUser {
		user[k] = v
	}
	user["force-password-change"] = "true"
	lusers := []map[string]interface{}{user}
	s.makeSystemUsers(c, lusers)
	s.createUserFromAssertion(c, true)
}

func (s *usersSuite) createUserFromAssertion(c *check.C, forcePasswordChange bool) []*auth.UserState {

	// mock the calls that create the user
	var addUserCalled bool
	defer devicestate.MockOsutilAddUser(func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, "guy")
		c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		c.Check(opts.ForcePasswordChange, check.Equals, forcePasswordChange)
		addUserCalled = true
		return nil
	})()

	// create user
	s.state.Lock()
	createdUsers, err := devicestate.CreateKnownUsers(s.state, false, "foo@bar.com")
	s.state.Unlock()

	expected := []*devicestate.CreatedUser{
		{
			Username: "guy",
		},
	}
	c.Assert(err, check.IsNil)
	c.Check(len(createdUsers), check.Equals, 1)
	c.Check(createdUsers, check.FitsTypeOf, expected)
	c.Check(createdUsers, check.DeepEquals, expected)
	c.Check(addUserCalled, check.Equals, true)

	// ensure the user was added to the state
	s.state.Lock()
	users, err := auth.Users(s.state)
	s.state.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(users, check.HasLen, 1)
	return users
}

func (s *usersSuite) TestCreateUserFromAssertionAllKnown(c *check.C) {
	expectSudoer := false
	createKnown := true
	s.testCreateUserFromAssertion(c, createKnown, expectSudoer)
}

func (s *usersSuite) TestCreateUserFromAssertionAllAutomatic(c *check.C) {
	// automatic implies "sudoder" and "createKnown"
	expectSudoer := true
	createKnown := true
	s.testCreateUserFromAssertion(c, createKnown, expectSudoer)
}

func (s *usersSuite) testCreateUserFromAssertion(c *check.C, createKnown bool, expectSudoer bool) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser, partnerUser, serialUser, badUser, badUserNoMatchingSerial, unknownUser})
	created := map[string]bool{}

	// mock the calls that create the user
	defer devicestate.MockOsutilAddUser(func(username string, opts *osutil.AddUserOptions) error {
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
		c.Check(opts.Sudoer, check.Equals, expectSudoer)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		created[username] = true
		return nil
	})()

	// make sure we report them as non-existing until created
	defer devicestate.MockUserLookup(func(username string) (*user.User, error) {
		if created[username] {
			return s.trivialUserLookup(username)
		}
		return nil, fmt.Errorf("not created yet")
	})()

	// create user
	var createdUsers []*devicestate.CreatedUser
	var err error
	s.state.Lock()
	if createKnown {
		createdUsers, err = devicestate.CreateKnownUsers(s.state, expectSudoer, "")
	} else {
		var createdUser *devicestate.CreatedUser
		createdUser, err = devicestate.CreateUser(s.state, expectSudoer, "", time.Time{})
		createdUsers = append(createdUsers, createdUser)
	}
	s.state.Unlock()

	c.Check(created, check.DeepEquals, map[string]bool{
		"guy":           true,
		"partnerguy":    true,
		"goodserialguy": true,
	})

	expected := []*devicestate.CreatedUser{
		{
			Username: "guy",
		},
		{
			Username: "partnerguy",
		},
		{
			Username: "goodserialguy",
		},
	}

	// sort created users, so we can use check.DeepEquals
	sort.Slice(expected, func(i, j int) bool {
		return expected[i].Username < expected[j].Username
	})
	sort.Slice(createdUsers, func(i, j int) bool {
		return createdUsers[i].Username < createdUsers[j].Username
	})

	c.Assert(err, check.IsNil)
	c.Check(len(createdUsers), check.Equals, 3)
	c.Check(createdUsers, check.FitsTypeOf, expected)
	c.Check(createdUsers, check.DeepEquals, expected)

	// ensure the user was added to the state
	s.state.Lock()
	users, userErr := auth.Users(s.state)
	s.state.Unlock()
	c.Assert(userErr, check.IsNil)
	c.Check(users, check.HasLen, 3)
}

func (s *usersSuite) TestCreateAllKnownUsersWithExpiration(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{expireUser})
	created := map[string]bool{}

	// mock the calls that create the user
	defer devicestate.MockOsutilAddUser(func(username string, opts *osutil.AddUserOptions) error {
		c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		created[username] = true
		return nil
	})()

	// make sure we report them as non-existing until created
	defer devicestate.MockUserLookup(func(username string) (*user.User, error) {
		if created[username] {
			return s.trivialUserLookup(username)
		}
		return nil, fmt.Errorf("not created yet")
	})()

	// create user
	var createdUsers []*devicestate.CreatedUser
	var err error
	s.state.Lock()
	createdUsers, err = devicestate.CreateKnownUsers(s.state, false, "")
	s.state.Unlock()

	c.Check(created, check.DeepEquals, map[string]bool{"guy": true})
	expected := []*devicestate.CreatedUser{{Username: "guy"}}
	c.Assert(err, check.IsNil)
	c.Check(len(createdUsers), check.Equals, 1)
	c.Check(createdUsers, check.FitsTypeOf, expected)
	c.Check(createdUsers, check.DeepEquals, expected)

	// ensure the user was added to the state
	s.state.Lock()
	users, userErr := auth.Users(s.state)
	s.state.Unlock()
	c.Assert(userErr, check.IsNil)
	c.Check(users, check.HasLen, 1)

	// Verify expiration
	until, err := time.Parse(time.RFC3339, expireUser["until"].(string))
	c.Assert(err, check.IsNil)
	c.Check(users[0].Expiration.Equal(until), check.Equals, true)
}

func (s *usersSuite) TestCreateUserFromAssertionAllKnownNoModelError(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// have not model yet
	s.state.Lock()
	err := devicestatetest.SetDevice(s.state, &auth.DeviceState{})
	s.state.Unlock()
	c.Assert(err, check.IsNil)

	// create user
	s.state.Lock()
	createdUsers, userErr := devicestate.CreateKnownUsers(s.state, true, "")
	s.state.Unlock()

	c.Assert(userErr, check.NotNil)
	c.Check(userErr.Error(), check.Matches, `cannot create user: cannot get model assertion: no state entry for key`)
	c.Check(s.errorIsInternal(userErr), check.Equals, true)
	c.Assert(createdUsers, check.IsNil)
}

func (s *usersSuite) TestCreateUserFromAssertionNoSerial(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{serialUser})

	s.state.Lock()
	err := devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	s.state.Unlock()
	c.Assert(err, check.IsNil)

	// create user
	s.state.Lock()
	createdUsers, userErr := devicestate.CreateKnownUsers(s.state, true, "serial@bar.com")
	s.state.Unlock()

	c.Check(userErr, check.NotNil)
	c.Check(userErr.Error(), check.Matches, `cannot create user "serial@bar.com": bound to serial assertion but device not yet registered`)
	c.Check(s.errorIsInternal(userErr), check.Equals, false)
	c.Assert(createdUsers, check.IsNil)
}

func (s *usersSuite) TestCreateUserFromAssertionDelayedAfterSerialAcquisition(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// initialize device, and add system-user assertion for serialUser
	s.makeSystemUsers(c, []map[string]interface{}{serialUser})

	created := map[string]bool{}

	// mock the calls that create the user
	defer devicestate.MockOsutilAddUser(func(username string, opts *osutil.AddUserOptions) error {
		c.Check(opts.Gecos, check.Equals, fmt.Sprintf("%s,%s", serialUser["email"], serialUser["name"]))
		c.Check(opts.Password, check.Equals, serialUser["password"])
		created[username] = true
		return nil
	})()

	// make sure we report them as non-existing until created
	defer devicestate.MockUserLookup(func(username string) (*user.User, error) {
		if created[username] {
			return s.trivialUserLookup(username)
		}
		return nil, fmt.Errorf("not created yet")
	})()

	// remove serial from device
	s.state.Lock()
	err := devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	s.state.Unlock()
	c.Assert(err, check.IsNil)

	// try creating user
	s.state.Lock()
	createdUsers, userErr := devicestate.CreateKnownUsers(s.state, true, "")
	s.state.Unlock()

	// make sure that no users were created
	c.Check(userErr, check.IsNil)
	c.Assert(createdUsers, check.IsNil)
	var waitingOnSerial bool
	s.state.Lock()
	err = s.state.Get("system-user-waiting-on-serial", &waitingOnSerial)
	s.state.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(waitingOnSerial, check.Equals, true)

	// mark seeded, no serial, ensure will still do nothing
	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()
	err = devicestate.EnsureSerialBoundSystemUserAssertionsProcessed(s.mgr)
	c.Check(err, check.IsNil)
	c.Assert(createdUsers, check.IsNil)

	// have a serial now
	s.state.Lock()
	err = devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "my-brand",
		Model:  "my-model",
		Serial: "serialserial",
	})
	s.state.Unlock()
	c.Assert(err, check.IsNil)

	// ensure, thereby creating pending users
	err = devicestate.EnsureSerialBoundSystemUserAssertionsProcessed(s.mgr)
	c.Check(err, check.IsNil)

	// make sure that system-user-waiting-on-serial has been set to false
	s.state.Lock()
	err = s.state.Get("system-user-waiting-on-serial", &waitingOnSerial)
	s.state.Unlock()

	c.Assert(err, check.IsNil)
	c.Check(waitingOnSerial, check.Equals, false)

	// get created users
	s.state.Lock()
	users, err := auth.Users(s.state)
	s.state.Unlock()

	// check that there is only one user, and that the details match serialUser
	c.Assert(err, check.IsNil)
	c.Check(len(users), check.Equals, 1)
	c.Check(users[0].Email, check.Equals, serialUser["email"])
	c.Check(users[0].Username, check.Equals, serialUser["username"])
}

func (s *usersSuite) TestCreateAllKnownUsersFromAssertionNoSerial(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	s.makeSystemUsers(c, []map[string]interface{}{serialUser})

	s.state.Lock()
	err := devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand: "my-brand",
		Model: "my-model",
	})
	s.state.Unlock()
	c.Assert(err, check.IsNil)

	// create user
	s.state.Lock()
	createdUsers, userErr := devicestate.CreateKnownUsers(s.state, true, "")
	s.state.Unlock()

	c.Check(userErr, check.IsNil)
	c.Assert(createdUsers, check.IsNil)

	c.Check(logbuf.String(), check.Matches, `(?s).*waiting for serial to add user "serial@bar\.com": bound to serial assertion but device not yet registered.*`)
}

func (s *usersSuite) TestCreateUserFromAssertionAllKnownButOwned(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	s.state.Lock()
	_, err := auth.NewUser(s.state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	s.state.Unlock()
	c.Check(err, check.IsNil)

	// mock the calls that create the user
	created := map[string]bool{}
	defer devicestate.MockOsutilAddUser(func(username string, opts *osutil.AddUserOptions) error {
		c.Check(username, check.Equals, "guy")
		c.Check(opts.Gecos, check.Equals, "foo@bar.com,Boring Guy")
		c.Check(opts.Sudoer, check.Equals, false)
		c.Check(opts.Password, check.Equals, "$6$salt$hash")
		created[username] = true
		return nil
	})()
	// make sure we report them as non-existing until created
	defer devicestate.MockUserLookup(func(username string) (*user.User, error) {
		if created[username] {
			return s.trivialUserLookup(username)
		}
		return nil, fmt.Errorf("not created yet")
	})()

	// create user
	s.state.Lock()
	createdUsers, err := devicestate.CreateKnownUsers(s.state, false, "")
	s.state.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(created, check.DeepEquals, map[string]bool{
		"guy": true,
	})
	expected := []*devicestate.CreatedUser{
		{
			Username: "guy",
		},
	}
	c.Check(len(createdUsers), check.Equals, 1)
	c.Check(createdUsers, check.FitsTypeOf, expected)
	c.Check(createdUsers, check.DeepEquals, expected)
}

func (s *usersSuite) TestCreateUserFromAssertionAllKnownButSkipExists(c *check.C) {
	s.makeSystemUsers(c, []map[string]interface{}{goodUser})

	s.state.Lock()
	_, err := auth.NewUser(s.state, auth.NewUserParams{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	s.state.Unlock()
	c.Check(err, check.IsNil)

	// mock the calls that create the user
	userLookUps := map[string]bool{}
	addUserCalled := false
	defer devicestate.MockOsutilAddUser(func(username string, opts *osutil.AddUserOptions) error {
		addUserCalled = true
		return fmt.Errorf("osutil.AddUser should not have been called")
	})()

	// report users as known so we dont make a call to create user
	defer devicestate.MockUserLookup(func(username string) (*user.User, error) {
		userLookUps[username] = true
		// return nil to indicate successful lookup
		return nil, nil
	})()

	// create user
	s.state.Lock()
	createdUsers, err := devicestate.CreateKnownUsers(s.state, false, "")
	s.state.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(len(createdUsers), check.Equals, 0)
	c.Check(len(userLookUps), check.Equals, 1)
	c.Check(addUserCalled, check.Equals, false)
}

func (s *usersSuite) TestCreateUserMissingEmail(c *check.C) {
	restore := release.MockOnClassic(false)
	defer restore()

	// create user
	s.state.Lock()
	createdUser, err := devicestate.CreateUser(s.state, true, "", time.Time{})
	s.state.Unlock()

	c.Assert(err, check.NotNil)
	c.Check(err.Error(), check.Matches, `cannot create user: 'email' field is empty`)
	c.Check(s.errorIsInternal(err), check.Equals, false)
	c.Check(createdUser, check.IsNil)
}
