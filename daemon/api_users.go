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
	"net/http"
	"regexp"
	"time"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/store"
)

var (
	loginCmd = &Command{
		Path:        "/v2/login",
		POST:        loginUser,
		WriteAccess: authenticatedAccess{Polkit: polkitActionLogin},
	}

	logoutCmd = &Command{
		Path:        "/v2/logout",
		POST:        logoutUser,
		WriteAccess: authenticatedAccess{Polkit: polkitActionLogin},
	}

	// backwards compat; to-be-deprecated
	createUserCmd = &Command{
		Path:        "/v2/create-user",
		POST:        postCreateUser,
		WriteAccess: rootAccess{},
	}

	usersCmd = &Command{
		Path:        "/v2/users",
		GET:         getUsers,
		POST:        postUsers,
		ReadAccess:  rootAccess{},
		WriteAccess: rootAccess{},
	}
)

var (
	deviceStateCreateUser       = devicestate.CreateUser
	deviceStateCreateKnownUsers = devicestate.CreateKnownUsers
	deviceStateRemoveUser       = devicestate.RemoveUser
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
	mylog.Check(decoder.Decode(&loginData))

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
		return &apiError{
			Status:  400,
			Message: "please use a valid email address.",
			Kind:    client.ErrorKindInvalidAuthData,
			Value:   map[string][]string{"email": {"invalid"}},
		}
	}

	overlord := c.d.overlord
	st := overlord.State()
	theStore := storeFrom(c.d)
	macaroon, discharge := mylog.Check3(theStore.LoginUser(loginData.Email, loginData.Password, loginData.Otp))
	switch err {
	case store.ErrAuthenticationNeeds2fa:
		return &apiError{
			Status:  401,
			Message: err.Error(),
			Kind:    client.ErrorKindTwoFactorRequired,
		}
	case store.Err2faFailed:
		return &apiError{
			Status:  401,
			Message: err.Error(),
			Kind:    client.ErrorKindTwoFactorFailed,
		}
	default:
		switch err := err.(type) {
		case store.InvalidAuthDataError:
			return &apiError{
				Status:  400,
				Message: err.Error(),
				Kind:    client.ErrorKindInvalidAuthData,
				Value:   err,
			}
		case store.PasswordPolicyError:
			return &apiError{
				Status:  401,
				Message: err.Error(),
				Kind:    client.ErrorKindPasswordPolicy,
				Value:   err,
			}
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
		mylog.Check(auth.UpdateUser(st, user))
	} else {
		user = mylog.Check2(auth.NewUser(st, auth.NewUserParams{
			Username:   loginData.Username,
			Email:      loginData.Email,
			Macaroon:   macaroon,
			Discharges: []string{discharge},
		}))
	}
	st.Unlock()

	result := userResponseData{
		ID:         user.ID,
		Username:   user.Username,
		Email:      user.Email,
		Macaroon:   user.Macaroon,
		Discharges: user.Discharges,
	}
	return SyncResponse(result)
}

func logoutUser(c *Command, r *http.Request, user *auth.UserState) Response {
	state := c.d.overlord.State()
	state.Lock()
	defer state.Unlock()

	if user == nil {
		return BadRequest("not logged in")
	}
	_ := mylog.Check2(auth.RemoveUser(state, user.ID))

	return SyncResponse(nil)
}

// this might need to become a function, if having user admin becomes a config option
var hasUserAdmin = !release.OnClassic

const noUserAdmin = "system user administration via snapd is not allowed on this system"

func postUsers(c *Command, r *http.Request, user *auth.UserState) Response {
	if !hasUserAdmin {
		return MethodNotAllowed(noUserAdmin)
	}

	var postData postUserData

	decoder := json.NewDecoder(r.Body)
	mylog.Check(decoder.Decode(&postData))

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
	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	u := mylog.Check2(deviceStateRemoveUser(st, username, &devicestate.RemoveUserOptions{}))

	result := map[string]interface{}{
		"removed": []userResponseData{
			{ID: u.ID, Username: u.Username, Email: u.Email},
		},
	}
	return SyncResponse(result)
}

func postCreateUser(c *Command, r *http.Request, user *auth.UserState) Response {
	if !hasUserAdmin {
		return Forbidden(noUserAdmin)
	}
	var createData postUserCreateData

	decoder := json.NewDecoder(r.Body)
	mylog.Check(decoder.Decode(&createData))

	// this is /v2/create-user, meaning we want the
	// backwards-compatible wackiness
	// Request singleUserResultCompat only if the request
	// was *not* to create all known system users.
	// Automatic implies known, which means we have
	// to take that into account as well
	known := createData.Known || createData.Automatic
	if !known || createData.Email != "" {
		createData.singleUserResultCompat = true
	}

	return createUser(c, createData)
}

func createUser(c *Command, createData postUserCreateData) Response {
	var createdUsersResponse []userResponseData
	// verify request
	st := c.d.overlord.State()
	st.Lock()
	users := mylog.Check2(auth.Users(st))
	st.Unlock()

	if !createData.Expiration.IsZero() {
		// Automatic implies known
		if createData.Known || createData.Automatic {
			// Known users are described by assertions, and assertions provide their own
			// expiration date for users, which we will not allow to overwrite
			return BadRequest("cannot create user: expiration date cannot be provided for known users")
		}
		if createData.Expiration.Before(time.Now()) {
			return BadRequest("cannot create user: expiration date must be set in the future")
		}
	}

	if !createData.ForceManaged {
		if len(users) > 0 && createData.Automatic {
			// no users created but no error with the automatic flag
			return SyncResponse([]userResponseData{})
		}
		if len(users) > 0 {
			return BadRequest("cannot create user: device already managed")
		}
		if release.OnClassic {
			return BadRequest("cannot create user: device is a classic system")
		}
	}
	if createData.Automatic {
		var enabled bool
		st.Lock()
		tr := config.NewTransaction(st)
		mylog.Check(tr.Get("core", "users.create.automatic", &enabled))
		st.Unlock()

		// defaults to enabled

		if !enabled {
			// disabled, do nothing
			return SyncResponse([]userResponseData{})
		}
		// Automatic implies known/sudoers
		createData.Known = true
		createData.Sudoer = true
	}

	createdUsers := mylog.Check2(doCreateUser(st, createData))

	if createData.singleUserResultCompat {
		return SyncResponse(&userResponseData{
			Username: createdUsers[0].Username,
			SSHKeys:  createdUsers[0].SSHKeys,
		})
	}

	for _, cu := range createdUsers {
		createdUsersResponse = append(createdUsersResponse, userResponseData{
			Username: cu.Username,
			SSHKeys:  cu.SSHKeys,
		})
	}
	return SyncResponse(createdUsersResponse)
}

func doCreateUser(st *state.State, createData postUserCreateData) ([]*devicestate.CreatedUser, error) {
	st.Lock()
	defer st.Unlock()

	if createData.Known {
		return deviceStateCreateKnownUsers(st, createData.Sudoer, createData.Email)
	}

	user := mylog.Check2(deviceStateCreateUser(st, createData.Sudoer, createData.Email, createData.Expiration))
	return []*devicestate.CreatedUser{user}, err
}

type postUserData struct {
	Action   string `json:"action"`
	Username string `json:"username"`
	postUserCreateData
	postUserDeleteData
}

type postUserCreateData struct {
	Email        string    `json:"email"`
	Sudoer       bool      `json:"sudoer"`
	Known        bool      `json:"known"`
	ForceManaged bool      `json:"force-managed"`
	Automatic    bool      `json:"automatic"`
	Expiration   time.Time `json:"expiration"`

	// singleUserResultCompat indicates whether to preserve
	// backwards compatibility, which results in more clunky
	// return values (userResponseData OR [userResponseData] vs now
	// uniform [userResponseData]); internal, not from JSON.
	singleUserResultCompat bool
}

type postUserDeleteData struct{}

func getUsers(c *Command, r *http.Request, user *auth.UserState) Response {
	st := c.d.overlord.State()
	st.Lock()
	users := mylog.Check2(auth.Users(st))
	st.Unlock()

	resp := make([]userResponseData, len(users))
	for i, u := range users {
		resp[i] = userResponseData{
			Username: u.Username,
			Email:    u.Email,
			ID:       u.ID,
		}
	}
	return SyncResponse(resp)
}
