// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"net/http"
	"os/user"
	"path/filepath"
	"regexp"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/strutil"
)

var (
	loginCmd = &Command{
		Path:     "/v2/login",
		POST:     loginUser,
		PolkitOK: "io.snapcraft.snapd.login",
	}

	logoutCmd = &Command{
		Path: "/v2/logout",
		POST: logoutUser,
	}

	// backwards compat; to-be-deprecated
	createUserCmd = &Command{
		Path:     "/v2/create-user",
		POST:     postCreateUser,
		RootOnly: true,
	}

	usersCmd = &Command{
		Path:     "/v2/users",
		GET:      getUsers,
		POST:     postUsers,
		RootOnly: true,
	}
)

var (
	osutilAddUser = osutil.AddUser
	osutilDelUser = osutil.DelUser
)

// userResponseData contains the data releated to user creation/login/query
type userResponseData struct {
	ID       int      `json:"id,omitempty"`
	Username string   `json:"username,omitempty"`
	Email    string   `json:"email,omitempty"`
	SSHKeys  []string `json:"ssh-keys,omitempty"`

	Macaroon   string   `json:"macaroon,omitempty"`
	Discharges []string `json:"discharges,omitempty"`
}

var isEmailish = regexp.MustCompile(`.@.*\..`).MatchString

func loginUser(c *Command, r *http.Request, user *auth.UserState) Response {
	var loginData struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Otp      string `json:"otp"`
	}

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&loginData); err != nil {
		return BadRequest("cannot decode login data from request body: %v", err)
	}

	if loginData.Email == "" && isEmailish(loginData.Username) {
		// for backwards compatibility, if no email is provided assume username is the email
		loginData.Email = loginData.Username
		loginData.Username = ""
	}

	if loginData.Email == "" && user != nil && user.Email != "" {
		loginData.Email = user.Email
	}

	// the "username" needs to look a lot like an email address
	if !isEmailish(loginData.Email) {
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: "please use a valid email address.",
				Kind:    errorKindInvalidAuthData,
				Value:   map[string][]string{"email": {"invalid"}},
			},
			Status: 400,
		}, nil)
	}

	overlord := c.d.overlord
	st := overlord.State()
	theStore := getStore(c)
	macaroon, discharge, err := theStore.LoginUser(loginData.Email, loginData.Password, loginData.Otp)
	switch err {
	case store.ErrAuthenticationNeeds2fa:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Kind:    errorKindTwoFactorRequired,
				Message: err.Error(),
			},
			Status: 401,
		}, nil)
	case store.Err2faFailed:
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Kind:    errorKindTwoFactorFailed,
				Message: err.Error(),
			},
			Status: 401,
		}, nil)
	default:
		switch err := err.(type) {
		case store.InvalidAuthDataError:
			return SyncResponse(&resp{
				Type: ResponseTypeError,
				Result: &errorResult{
					Message: err.Error(),
					Kind:    errorKindInvalidAuthData,
					Value:   err,
				},
				Status: 400,
			}, nil)
		case store.PasswordPolicyError:
			return SyncResponse(&resp{
				Type: ResponseTypeError,
				Result: &errorResult{
					Message: err.Error(),
					Kind:    errorKindPasswordPolicy,
					Value:   err,
				},
				Status: 401,
			}, nil)
		}
		return Unauthorized(err.Error())
	case nil:
		// continue
	}
	st.Lock()
	if user != nil {
		// local user logged-in, set its store macaroons
		user.StoreMacaroon = macaroon
		user.StoreDischarges = []string{discharge}
		// user's email address authenticated by the store
		user.Email = loginData.Email
		err = auth.UpdateUser(st, user)
	} else {
		user, err = auth.NewUser(st, loginData.Username, loginData.Email, macaroon, []string{discharge})
	}
	st.Unlock()
	if err != nil {
		return InternalError("cannot persist authentication details: %v", err)
	}

	result := userResponseData{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}
	return SyncResponse(result, nil)
}

func logoutUser(c *Command, r *http.Request, user *auth.UserState) Response {
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if user == nil {
		return BadRequest("not logged in")
	}
	err := auth.RemoveUser(state, user.ID)
	if err != nil {
		return InternalError(err.Error())
	}

	return SyncResponse(nil, nil)
}

// this might need to become a function, if having user admin becomes a config option
var hasUserAdmin = !release.OnClassic

const noUserAdmin = "system user administration via snapd is not allowed on this system"

func postUsers(c *Command, r *http.Request, user *auth.UserState) Response {
	if !hasUserAdmin {
		return MethodNotAllowed(noUserAdmin, r.Method)
	}

	var postData postUserData

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postData); err != nil {
		return BadRequest("cannot decode user action data from request body: %v", err)
	}
	if decoder.More() {
		return BadRequest("spurious content after user action")
	}
	switch postData.Action {
	case "create":
		return createUser(c, postData.postUserCreateData)
	case "remove":
		return removeUser(c, postData.Username, postData.postUserDeleteData)
	case "":
		return BadRequest("missing user action")
	}
	return BadRequest("unsupported user action %q", postData.Action)
}

func removeUser(c *Command, username string, opts postUserDeleteData) Response {
	// catch silly errors
	if username == "" {
		return BadRequest("need a username to remove")
	}
	// first remove the system user
	if err := osutilDelUser(username, &osutil.DelUserOptions{ExtraUsers: !release.OnClassic}); err != nil {
		return InternalError(err.Error())
	}

	// then the UserState
	st := c.d.overlord.State()
	st.Lock()
	err := auth.RemoveUserByName(st, username)
	st.Unlock()
	// ErrInvalidUser means "not found" in this case
	if err != nil && err != auth.ErrInvalidUser {
		return InternalError(err.Error())
	}

	// returns nil so it's still arguably a []userResponseData
	return SyncResponse(nil, nil)
}

func postCreateUser(c *Command, r *http.Request, user *auth.UserState) Response {
	if !hasUserAdmin {
		return Forbidden(noUserAdmin)
	}
	var createData postUserCreateData

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&createData); err != nil {
		return BadRequest("cannot decode create-user data from request body: %v", err)
	}

	rsp := createUser(c, createData)
	// backwards compatibility hack
	if createData.Email == "" && createData.Known {
		// ok as per old API
		return rsp
	}
	if rsp, ok := rsp.(*resp); ok && rsp.Type == ResponseTypeSync {
		// Result should be a []userResponseData with a single item
		if res, ok := rsp.Result.([]userResponseData); ok && len(res) == 1 {
			return SyncResponse(&res[0], rsp.Meta)
		}
	}
	return rsp
}

func createUser(c *Command, createData postUserCreateData) Response {
	// verify request
	st := c.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	st.Unlock()
	if err != nil {
		return InternalError("cannot get user count: %s", err)
	}

	if !createData.ForceManaged {
		if len(users) > 0 {
			return BadRequest("cannot create user: device already managed")
		}
		if release.OnClassic {
			return BadRequest("cannot create user: device is a classic system")
		}
	}

	var model *asserts.Model
	createKnown := createData.Known
	if createKnown {
		var err error
		st.Lock()
		model, err = c.d.overlord.DeviceManager().Model()
		st.Unlock()
		if err != nil {
			return InternalError("cannot create user: cannot get model assertion: %v", err)
		}
	}

	// special case: the user requested the creation of all known
	// system-users
	if createData.Email == "" && createKnown {
		return createAllKnownSystemUsers(st, model, &createData)
	}
	if createData.Email == "" {
		return BadRequest("cannot create user: 'email' field is empty")
	}

	var username string
	var opts *osutil.AddUserOptions
	if createKnown {
		username, opts, err = getUserDetailsFromAssertion(st, model, createData.Email)
	} else {
		username, opts, err = getUserDetailsFromStore(getStore(c), createData.Email)
	}
	if err != nil {
		return BadRequest("%s", err)
	}

	// FIXME: duplicated code
	opts.Sudoer = createData.Sudoer
	opts.ExtraUsers = !release.OnClassic

	if err := osutilAddUser(username, opts); err != nil {
		return BadRequest("cannot create user %s: %s", username, err)
	}

	if err := setupLocalUser(c.d.overlord.State(), username, createData.Email); err != nil {
		return InternalError("%s", err)
	}

	return SyncResponse([]userResponseData{{
		Username: username,
		SSHKeys:  opts.SSHKeys,
	}}, nil)
}

func getUserDetailsFromStore(theStore snapstate.StoreService, email string) (string, *osutil.AddUserOptions, error) {
	v, err := theStore.UserInfo(email)
	if err != nil {
		return "", nil, fmt.Errorf("cannot create user %q: %s", email, err)
	}
	if len(v.SSHKeys) == 0 {
		return "", nil, fmt.Errorf("cannot create user for %q: no ssh keys found", email)
	}

	gecos := fmt.Sprintf("%s,%s", email, v.OpenIDIdentifier)
	opts := &osutil.AddUserOptions{
		SSHKeys: v.SSHKeys,
		Gecos:   gecos,
	}
	return v.Username, opts, nil
}

func createAllKnownSystemUsers(st *state.State, modelAs *asserts.Model, createData *postUserCreateData) Response {
	var createdUsers []userResponseData
	headers := map[string]string{
		"brand-id": modelAs.BrandID(),
	}

	st.Lock()
	db := assertstate.DB(st)
	assertions, err := db.FindMany(asserts.SystemUserType, headers)
	st.Unlock()
	if err != nil && !asserts.IsNotFound(err) {
		return BadRequest("cannot find system-user assertion: %s", err)
	}

	for _, as := range assertions {
		email := as.(*asserts.SystemUser).Email()
		// we need to use getUserDetailsFromAssertion as this verifies
		// the assertion against the current brand/model/time
		username, opts, err := getUserDetailsFromAssertion(st, modelAs, email)
		if err != nil {
			logger.Noticef("ignoring system-user assertion for %q: %s", email, err)
			continue
		}
		// ignore already existing users
		if _, err := userLookup(username); err == nil {
			continue
		}

		// FIXME: duplicated code
		opts.Sudoer = createData.Sudoer
		opts.ExtraUsers = !release.OnClassic

		if err := osutilAddUser(username, opts); err != nil {
			return InternalError("cannot add user %q: %s", username, err)
		}
		if err := setupLocalUser(st, username, email); err != nil {
			return InternalError("%s", err)
		}
		createdUsers = append(createdUsers, userResponseData{
			Username: username,
			SSHKeys:  opts.SSHKeys,
		})
	}

	return SyncResponse(createdUsers, nil)
}

func getUserDetailsFromAssertion(st *state.State, modelAs *asserts.Model, email string) (string, *osutil.AddUserOptions, error) {
	errorPrefix := fmt.Sprintf("cannot add system-user %q: ", email)

	st.Lock()
	db := assertstate.DB(st)
	st.Unlock()

	brandID := modelAs.BrandID()
	series := modelAs.Series()
	model := modelAs.Model()

	a, err := db.Find(asserts.SystemUserType, map[string]string{
		"brand-id": brandID,
		"email":    email,
	})
	if err != nil {
		return "", nil, fmt.Errorf(errorPrefix+"%v", err)
	}
	// the asserts package guarantees that this cast will work
	su := a.(*asserts.SystemUser)

	// cross check that the assertion is valid for the given series/model

	// check that the signer of the assertion is one of the accepted ones
	sysUserAuths := modelAs.SystemUserAuthority()
	if len(sysUserAuths) > 0 && !strutil.ListContains(sysUserAuths, su.AuthorityID()) {
		return "", nil, fmt.Errorf(errorPrefix+"%q not in accepted authorities %q", email, su.AuthorityID(), sysUserAuths)
	}
	if len(su.Series()) > 0 && !strutil.ListContains(su.Series(), series) {
		return "", nil, fmt.Errorf(errorPrefix+"%q not in series %q", email, series, su.Series())
	}
	if len(su.Models()) > 0 && !strutil.ListContains(su.Models(), model) {
		return "", nil, fmt.Errorf(errorPrefix+"%q not in models %q", model, su.Models())
	}
	if !su.ValidAt(time.Now()) {
		return "", nil, fmt.Errorf(errorPrefix + "assertion not valid anymore")
	}

	gecos := fmt.Sprintf("%s,%s", email, su.Name())
	opts := &osutil.AddUserOptions{
		SSHKeys:             su.SSHKeys(),
		Gecos:               gecos,
		Password:            su.Password(),
		ForcePasswordChange: su.ForcePasswordChange(),
	}
	return su.Username(), opts, nil
}

type postUserData struct {
	Action   string `json:"action"`
	Username string `json:"username"`
	postUserCreateData
	postUserDeleteData
}

type postUserCreateData struct {
	Email        string `json:"email"`
	Sudoer       bool   `json:"sudoer"`
	Known        bool   `json:"known"`
	ForceManaged bool   `json:"force-managed"`
}

type postUserDeleteData struct{}

var userLookup = user.Lookup

func setupLocalUser(st *state.State, username, email string) error {
	user, err := userLookup(username)
	if err != nil {
		return fmt.Errorf("cannot lookup user %q: %s", username, err)
	}
	uid, gid, err := osutil.UidGid(user)
	if err != nil {
		return err
	}
	authDataFn := filepath.Join(user.HomeDir, ".snap", "auth.json")
	if err := osutil.MkdirAllChown(filepath.Dir(authDataFn), 0700, uid, gid); err != nil {
		return err
	}

	// setup new user, local-only
	st.Lock()
	authUser, err := auth.NewUser(st, username, email, "", nil)
	st.Unlock()
	if err != nil {
		return fmt.Errorf("cannot persist authentication details: %v", err)
	}
	// store macaroon auth, user's ID, email and username in auth.json in
	// the new users home dir
	outStr, err := json.Marshal(struct {
		ID       int    `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
		Macaroon string `json:"macaroon"`
	}{
		ID:       authUser.ID,
		Username: authUser.Username,
		Email:    authUser.Email,
		Macaroon: authUser.Macaroon,
	})
	if err != nil {
		return fmt.Errorf("cannot marshal auth data: %s", err)
	}
	if err := osutil.AtomicWriteFileChown(authDataFn, []byte(outStr), 0600, 0, uid, gid); err != nil {
		return fmt.Errorf("cannot write auth file %q: %s", authDataFn, err)
	}

	return nil
}

func getUsers(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	users, err := auth.Users(st)
	st.Unlock()
	if err != nil {
		return InternalError("cannot get users: %s", err)
	}

	resp := make([]userResponseData, len(users))
	for i, u := range users {
		resp[i] = userResponseData{
			Username: u.Username,
			Email:    u.Email,
			ID:       u.ID,
		}
	}
	return SyncResponse(resp, nil)
}
