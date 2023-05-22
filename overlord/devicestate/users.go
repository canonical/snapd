// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2022-2023 Canonical Ltd
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

// UserError is returned when invalid or insufficient data is supplied,
// or if a user-assertion is not found.
type UserError struct {
	Err error
}

func (e *UserError) Error() string {
	return e.Err.Error()
}

type RemoveUserOptions struct {
	Force bool
}

// CreatedUser holds the results from a create user operation.
type CreatedUser struct {
	Username string
	SSHKeys  []string
}

// CreateUser creates a Linux user based on the specified email.
// The username and public ssh keys for the created account are
// determined from Ubuntu store based on the email.
func CreateUser(st *state.State, sudoer bool, email string, expiration time.Time) (*CreatedUser, error) {
	if email == "" {
		return nil, &UserError{Err: fmt.Errorf("cannot create user: 'email' field is empty")}
	}

	storeService := snapstate.Store(st, nil)
	username, opts, err := getUserDetailsFromStore(st, storeService, email)
	if err != nil {
		return nil, &UserError{Err: fmt.Errorf("cannot create user %q: %s", email, err)}
	}

	opts.Sudoer = sudoer
	return addUser(st, username, email, expiration, opts)
}

// CreateKnownUsers creates known users. The user details are fetched
// from existing system user assertions.
// If no email is passed, all known users will be created based on valid system user assertions.
// If an email is passed, only the corresponding system user assertion is used.
func CreateKnownUsers(st *state.State, sudoer bool, email string) ([]*CreatedUser, error) {
	model, err := findModel(st)
	if err != nil {
		return nil, fmt.Errorf("cannot create user: cannot get model assertion: %v", err)
	}

	serial, err := findSerial(st, nil)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		return nil, fmt.Errorf("cannot create user: cannot get serial: %v", err)
	}

	db := assertstate.DB(st)
	if email == "" {
		return createAllKnownSystemUsers(st, db, model, serial, sudoer)
	}

	username, expiration, opts, err := getUserDetailsFromAssertion(db, model, serial, email)
	if err != nil {
		return nil, &UserError{Err: fmt.Errorf("cannot create user %q: %v", email, err)}
	}

	opts.Sudoer = sudoer
	createdUser, err := addUser(st, username, email, expiration, opts)
	if err != nil {
		return nil, err
	}
	return []*CreatedUser{createdUser}, nil
}

// RemoveUser removes linux user account of passed username.
func RemoveUser(st *state.State, username string, opts *RemoveUserOptions) (*auth.UserState, error) {
	// TODO: allow to remove user entries by email as well
	if opts == nil {
		opts = &RemoveUserOptions{}
	}

	// catch silly errors
	if username == "" {
		return nil, &UserError{Err: fmt.Errorf("need a username to remove")}
	}

	// check the user is known to snapd
	_, err := auth.UserByUsername(st, username)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidUser) {
			return nil, &UserError{Err: fmt.Errorf("user %q is not known", username)}
		}
		return nil, err
	}

	// first remove the system user
	delUseropts := &osutil.DelUserOptions{
		ExtraUsers: !release.OnClassic,
		Force:      opts.Force,
	}
	if err := osutilDelUser(username, delUseropts); err != nil {
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

func getUserDetailsFromStore(st *state.State, theStore snapstate.StoreService, email string) (string, *osutil.AddUserOptions, error) {
	st.Unlock()
	defer st.Lock()

	v, err := theStore.UserInfo(email)
	if err != nil {
		return "", nil, err
	}
	if len(v.SSHKeys) == 0 {
		return "", nil, fmt.Errorf("no ssh keys found")
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

func createKnownSystemUser(state *state.State, userAssertion *asserts.SystemUser, assertDb asserts.RODatabase, model *asserts.Model, serial *asserts.Serial, sudoer bool) (*CreatedUser, error) {
	email := userAssertion.Email()
	// we need to use getUserDetailsFromAssertion as this verifies
	// the assertion against the current brand/model/time
	username, expiration, addUserOpts, err := getUserDetailsFromAssertion(assertDb, model, serial, email)
	if err != nil {
		if errors.Is(err, errSystemUserBoundToSerialButTooEarly) {
			state.Set("system-user-waiting-on-serial", true)
			logger.Noticef("waiting for serial to add user %q: %s", email, err)
			return nil, nil
		}
		logger.Noticef("ignoring system-user assertion for %q: %s", email, err)
		return nil, nil
	}

	// ignore already existing users
	if _, err := userLookup(username); err == nil {
		return nil, nil
	}

	addUserOpts.Sudoer = sudoer
	return addUser(state, username, email, expiration, addUserOpts)
}

var createAllKnownSystemUsers = func(state *state.State, assertDb asserts.RODatabase, model *asserts.Model, serial *asserts.Serial, sudoer bool) ([]*CreatedUser, error) {
	headers := map[string]string{
		"brand-id": model.BrandID(),
	}

	assertions, err := assertDb.FindMany(asserts.SystemUserType, headers)
	if err != nil && !errors.Is(err, &asserts.NotFoundError{}) {
		return nil, &UserError{Err: fmt.Errorf("cannot find system-user assertion: %s", err)}
	}

	var createdUsers []*CreatedUser
	for _, as := range assertions {
		createdUser, err := createKnownSystemUser(state, as.(*asserts.SystemUser), assertDb, model, serial, sudoer)
		if err != nil {
			return nil, err
		}
		if createdUser == nil {
			continue
		}
		createdUsers = append(createdUsers, createdUser)
	}

	return createdUsers, nil
}

var errSystemUserBoundToSerialButTooEarly = errors.New("bound to serial assertion but device not yet registered")

func getUserDetailsFromAssertion(assertDb asserts.RODatabase, modelAs *asserts.Model, serialAs *asserts.Serial, email string) (string, time.Time, *osutil.AddUserOptions, error) {
	brandID := modelAs.BrandID()
	series := modelAs.Series()
	model := modelAs.Model()

	a, err := assertDb.Find(asserts.SystemUserType, map[string]string{
		"brand-id": brandID,
		"email":    email,
	})
	if err != nil {
		return "", time.Time{}, nil, err
	}
	// the asserts package guarantees that this cast will work
	su := a.(*asserts.SystemUser)

	// check that the signer of the assertion is one of the accepted ones
	sysUserAuths := modelAs.SystemUserAuthority()
	if len(sysUserAuths) > 0 && !strutil.ListContains(sysUserAuths, su.AuthorityID()) {
		return "", time.Time{}, nil, fmt.Errorf("%q not in accepted authorities %q", su.AuthorityID(), sysUserAuths)
	}
	// cross check that the assertion is valid for the given series/model
	if len(su.Series()) > 0 && !strutil.ListContains(su.Series(), series) {
		return "", time.Time{}, nil, fmt.Errorf("%q not in series %q", series, su.Series())
	}
	if len(su.Models()) > 0 && !strutil.ListContains(su.Models(), model) {
		return "", time.Time{}, nil, fmt.Errorf("%q not in models %q", model, su.Models())
	}
	if len(su.Serials()) > 0 {
		if serialAs == nil {
			return "", time.Time{}, nil, errSystemUserBoundToSerialButTooEarly
		}
		serial := serialAs.Serial()
		if !strutil.ListContains(su.Serials(), serial) {
			return "", time.Time{}, nil, fmt.Errorf("%q not in serials %q", serial, su.Serials())
		}
	}

	if !su.ValidAt(time.Now()) {
		return "", time.Time{}, nil, fmt.Errorf("assertion not valid anymore")
	}

	gecos := fmt.Sprintf("%s,%s", email, su.Name())
	opts := &osutil.AddUserOptions{
		SSHKeys:             su.SSHKeys(),
		Gecos:               gecos,
		Password:            su.Password(),
		ForcePasswordChange: su.ForcePasswordChange(),
	}
	return su.Username(), su.UserExpiration(), opts, nil
}

func setupLocalUser(state *state.State, username, email string, expiration time.Time) error {
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
	authUser, err := auth.NewUser(state, auth.NewUserParams{
		Username:   username,
		Email:      email,
		Macaroon:   "",
		Discharges: nil,
		Expiration: expiration,
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

func addUser(state *state.State, username string, email string, expiration time.Time, opts *osutil.AddUserOptions) (*CreatedUser, error) {
	opts.ExtraUsers = !release.OnClassic
	if err := osutilAddUser(username, opts); err != nil {
		return nil, fmt.Errorf("cannot add user %q: %s", username, err)
	}
	if err := setupLocalUser(state, username, email, expiration); err != nil {
		return nil, err
	}

	return &CreatedUser{
		Username: username,
		SSHKeys:  opts.SSHKeys,
	}, nil
}
