// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2022 Canonical Ltd
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

package devicestate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/user"
	"path/filepath"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

var (
	osutilAddUser = osutil.AddUser
	osutilDelUser = osutil.DelUser
	userLookup    = user.Lookup
)

// UserError is used for errors which are known to snapd. This is errors
// originate from this code here.
type UserError struct {
	Err error
}

func (e *UserError) Error() string {
	return e.Err.Error()
}

// CreatedUser is the returned result which wraps some resulting data
// from a create user operation.
type CreatedUser struct {
	Username string
	SSHKeys  []string
}

// CreateUser creates linux user based on the passed email, username
// and public ssh keys for the created user account are determined
// from Ubuntu store based on the passed email.
func CreateUser(st *state.State, sudoer bool, email string) (CreatedUser, error) {
	if email == "" {
		return CreatedUser{}, &UserError{Err: fmt.Errorf("cannot create user: 'email' field is empty")}
	}

	db := assertstate.DB(st)
	u := &createUserOpts{
		assertDb: db,
		modelAs:  nil,
		serialAs: nil,
		isSudoer: sudoer,
	}

	storeService := snapstate.Store(st, nil)
	st.Unlock()
	username, opts, err := getUserDetailsFromStore(storeService, email)
	st.Lock()

	if err != nil {
		return CreatedUser{}, &UserError{Err: err}
	}
	opts.Sudoer = sudoer
	createdUser, err := u.createUser(st, opts, username, email)
	if err != nil {
		return CreatedUser{}, err
	}
	return createdUser, nil
}

// CreateKnownUsers create known user(s)
// user details are fetched from existing system user assertions.
// If no email is passed, all known users will be created based on valid system user assertions.
// If email is passed, only corresponding system user assertion is used for user creation.
func CreateKnownUsers(st *state.State, mgr *DeviceManager, sudoer bool, email string) ([]CreatedUser, error) {
	model, err := mgr.Model()
	if err != nil {
		return nil, fmt.Errorf("cannot create user: cannot get model assertion: %v", err)
	}

	serial, err := mgr.Serial()
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, fmt.Errorf("cannot create user: cannot get serial: %v", err)
	}

	db := assertstate.DB(st)
	u := &createUserOpts{
		assertDb: db,
		modelAs:  model,
		serialAs: serial,
		isSudoer: sudoer,
	}

	if email == "" {
		return u.createAllKnownSystemUsers(st)
	}

	username, opts, err := getUserDetailsFromAssertion(u.assertDb, u.modelAs, u.serialAs, email)
	if err != nil {
		return nil, &UserError{Err: err}
	}

	opts.Sudoer = sudoer
	createdUser, err := u.createUser(st, opts, username, email)
	if err != nil {
		return nil, err
	}
	return []CreatedUser{createdUser}, nil
}

// RemoveUser removes linux user account of passed username.
func RemoveUser(st *state.State, username string) (*auth.UserState, error) {
	// TODO: allow to remove user entries by email as well

	// catch silly errors
	if username == "" {
		return nil, &UserError{Err: fmt.Errorf("need a username to remove")}
	}

	// check the user is known to snapd
	_, err := auth.UserByUsername(st, username)
	if err == auth.ErrInvalidUser {
		return nil, &UserError{Err: fmt.Errorf("user %q is not known", username)}
	}
	if err != nil {
		return nil, err
	}

	// first remove the system user
	if err := osutilDelUser(username, &osutil.DelUserOptions{ExtraUsers: !release.OnClassic}); err != nil {
		return nil, err
	}

	// then the UserState
	u, err := auth.RemoveUserByUsername(st, username)
	// ErrInvalidUser means "not found" in this case
	if err != nil && err != auth.ErrInvalidUser {
		return nil, err
	}
	return u, nil
}

func (u *createUserOpts) createUser(st *state.State, opts *osutil.AddUserOptions, username, email string) (CreatedUser, error) {
	opts.ExtraUsers = !release.OnClassic
	createdUser, err := u.addUser(st, username, email, opts)
	if err != nil {
		return CreatedUser{}, err
	}

	return createdUser, nil
}

func getUserDetailsFromStore(theStore snapstate.StoreService, email string) (string, *osutil.AddUserOptions, error) {
	v, err := theStore.UserInfo(email)
	if err != nil {
		return "", nil, fmt.Errorf("cannot create user %q: %s", email, err)
	}
	if len(v.SSHKeys) == 0 {
		return "", nil, fmt.Errorf("cannot create user for %q: no ssh keys found", email)
	}

	// Amend information where the key came from to ensure it can
	// be update/replaced later
	for i, k := range v.SSHKeys {
		v.SSHKeys[i] = fmt.Sprintf(`%s # snapd {"origin":"store","email":%q}`, k, email)
	}

	gecos := fmt.Sprintf("%s,%s", email, v.OpenIDIdentifier)
	opts := &osutil.AddUserOptions{
		SSHKeys: v.SSHKeys,
		Gecos:   gecos,
	}
	return v.Username, opts, nil
}

// createUserOpts is a helper to handle user operations
type createUserOpts struct {
	assertDb asserts.RODatabase
	modelAs  *asserts.Model
	serialAs *asserts.Serial
	isSudoer bool
}

func (u *createUserOpts) createAllKnownSystemUsers(state *state.State) ([]CreatedUser, error) {
	headers := map[string]string{
		"brand-id": u.modelAs.BrandID(),
	}

	assertions, err := u.assertDb.FindMany(asserts.SystemUserType, headers)
	if err != nil && !asserts.IsNotFound(err) {
		return nil, &UserError{Err: fmt.Errorf("cannot find system-user assertion: %s", err)}
	}

	var createdUsers []CreatedUser
	for _, as := range assertions {
		email := as.(*asserts.SystemUser).Email()
		// we need to use getUserDetailsFromAssertion as this verifies
		// the assertion against the current brand/model/time
		username, opts, err := getUserDetailsFromAssertion(u.assertDb, u.modelAs, u.serialAs, email)
		if err != nil {
			logger.Noticef("ignoring system-user assertion for %q: %s", email, err)
			continue
		}
		// ignore already existing users
		if _, err := userLookup(username); err == nil {
			continue
		}

		opts.Sudoer = u.isSudoer
		opts.ExtraUsers = !release.OnClassic

		createdUser, err := u.addUser(state, username, email, opts)
		if err != nil {
			return nil, err
		}
		createdUsers = append(createdUsers, createdUser)
	}

	return createdUsers, nil
}

func getUserDetailsFromAssertion(assertDb asserts.RODatabase, modelAs *asserts.Model, serialAs *asserts.Serial, email string) (string, *osutil.AddUserOptions, error) {
	errorPrefix := fmt.Sprintf("cannot add system-user %q: ", email)

	brandID := modelAs.BrandID()
	series := modelAs.Series()
	model := modelAs.Model()

	a, err := assertDb.Find(asserts.SystemUserType, map[string]string{
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
	if len(su.Serials()) > 0 {
		if serialAs == nil {
			return "", nil, fmt.Errorf(errorPrefix + "bound to serial assertion but device not yet registered")
		}
		serial := serialAs.Serial()
		if !strutil.ListContains(su.Serials(), serial) {
			return "", nil, fmt.Errorf(errorPrefix+"%q not in serials %q", serial, su.Serials())
		}
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

func (u *createUserOpts) setupLocalUser(state *state.State, username, email string) error {
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
	authUser, err := auth.NewUser(state, auth.NewUserData{
		Username:   username,
		Email:      email,
		Macaroon:   "",
		Discharges: nil,
	})
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

func (u *createUserOpts) addUser(state *state.State, username string, email string, opts *osutil.AddUserOptions) (CreatedUser, error) {
	if err := osutilAddUser(username, opts); err != nil {
		return CreatedUser{}, fmt.Errorf("cannot add user %q: %s", username, err)
	}
	if err := u.setupLocalUser(state, username, email); err != nil {
		return CreatedUser{}, err
	}

	return CreatedUser{
		Username: username,
		SSHKeys:  opts.SSHKeys,
	}, nil
}
