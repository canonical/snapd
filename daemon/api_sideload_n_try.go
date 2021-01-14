// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2020 Canonical Ltd
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
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
)

const maxReadBuflen = 1024 * 1024

func sideloadOrTrySnap(c *Command, body io.ReadCloser, boundary string, user *auth.UserState) Response {
	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("cannot find route for change")
	}

	// POSTs to sideload snaps must be a multipart/form-data file upload.
	form, err := multipart.NewReader(body, boundary).ReadForm(maxReadBuflen)
	if err != nil {
		return BadRequest("cannot read POST form: %v", err)
	}

	dangerousOK := isTrue(form, "dangerous")
	flags, err := modeFlags(isTrue(form, "devmode"), isTrue(form, "jailmode"), isTrue(form, "classic"))
	if err != nil {
		return BadRequest(err.Error())
	}

	if len(form.Value["action"]) > 0 && form.Value["action"][0] == "try" {
		if len(form.Value["snap-path"]) == 0 {
			return BadRequest("need 'snap-path' value in form")
		}
		return trySnap(c.d.overlord.State(), form.Value["snap-path"][0], flags)
	}
	flags.RemoveSnapPath = true

	flags.Unaliased = isTrue(form, "unaliased")
	flags.IgnoreRunning = isTrue(form, "ignore-running")

	// find the file for the "snap" form field
	var snapBody multipart.File
	var origPath string
out:
	for name, fheaders := range form.File {
		if name != "snap" {
			continue
		}
		for _, fheader := range fheaders {
			snapBody, err = fheader.Open()
			origPath = fheader.Filename
			if err != nil {
				return BadRequest(`cannot open uploaded "snap" file: %v`, err)
			}
			defer snapBody.Close()

			break out
		}
	}
	defer form.RemoveAll()

	if snapBody == nil {
		return BadRequest(`cannot find "snap" file field in provided multipart/form-data payload`)
	}

	// we are in charge of the tempfile life cycle until we hand it off to the change
	changeTriggered := false
	// if you change this prefix, look for it in the tests
	// also see localInstallCleanup in snapstate/snapmgr.go
	tmpf, err := ioutil.TempFile(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix)
	if err != nil {
		return InternalError("cannot create temporary file: %v", err)
	}

	tempPath := tmpf.Name()

	defer func() {
		if !changeTriggered {
			os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(tmpf, snapBody); err != nil {
		return InternalError("cannot copy request into temporary file: %v", err)
	}
	tmpf.Sync()

	if len(form.Value["snap-path"]) > 0 {
		origPath = form.Value["snap-path"][0]
	}

	var instanceName string

	if len(form.Value["name"]) > 0 {
		// caller has specified desired instance name
		instanceName = form.Value["name"][0]
		if err := snap.ValidateInstanceName(instanceName); err != nil {
			return BadRequest(err.Error())
		}
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var snapName string
	var sideInfo *snap.SideInfo

	if !dangerousOK {
		si, err := snapasserts.DeriveSideInfo(tempPath, assertstate.DB(st))
		switch {
		case err == nil:
			snapName = si.RealName
			sideInfo = si
		case asserts.IsNotFound(err):
			// with devmode we try to find assertions but it's ok
			// if they are not there (implies --dangerous)
			if !isTrue(form, "devmode") {
				msg := "cannot find signatures with metadata for snap"
				if origPath != "" {
					msg = fmt.Sprintf("%s %q", msg, origPath)
				}
				return BadRequest(msg)
			}
			// TODO: set a warning if devmode
		default:
			return BadRequest(err.Error())
		}
	}

	if snapName == "" {
		// potentially dangerous but dangerous or devmode params were set
		info, err := unsafeReadSnapInfo(tempPath)
		if err != nil {
			return BadRequest("cannot read snap file: %v", err)
		}
		snapName = info.SnapName()
		sideInfo = &snap.SideInfo{RealName: snapName}
	}

	if instanceName != "" {
		requestedSnapName := snap.InstanceSnap(instanceName)
		if requestedSnapName != snapName {
			return BadRequest(fmt.Sprintf("instance name %q does not match snap name %q", instanceName, snapName))
		}
	} else {
		instanceName = snapName
	}

	msg := fmt.Sprintf(i18n.G("Install %q snap from file"), instanceName)
	if origPath != "" {
		msg = fmt.Sprintf(i18n.G("Install %q snap from file %q"), instanceName, origPath)
	}

	tset, _, err := snapstateInstallPath(st, sideInfo, tempPath, instanceName, "", flags)
	if err != nil {
		return errToResponse(err, []string{snapName}, InternalError, "cannot install snap file: %v")
	}

	chg := newChange(st, "install-snap", msg, []*state.TaskSet{tset}, []string{instanceName})
	chg.Set("api-data", map[string]string{"snap-name": instanceName})

	ensureStateSoon(st)

	// only when the unlock succeeds (as opposed to panicing) is the handoff done
	// but this is good enough
	changeTriggered = true

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

func isTrue(form *multipart.Form, key string) bool {
	value := form.Value[key]
	if len(value) == 0 {
		return false
	}
	b, err := strconv.ParseBool(value[0])
	if err != nil {
		return false
	}

	return b
}

func trySnap(st *state.State, trydir string, flags snapstate.Flags) Response {
	st.Lock()
	defer st.Unlock()

	if !filepath.IsAbs(trydir) {
		return BadRequest("cannot try %q: need an absolute path", trydir)
	}
	if !osutil.IsDirectory(trydir) {
		return BadRequest("cannot try %q: not a snap directory", trydir)
	}

	// the developer asked us to do this with a trusted snap dir
	info, err := unsafeReadSnapInfo(trydir)
	if _, ok := err.(snap.NotSnapError); ok {
		return SyncResponse(&resp{
			Type: ResponseTypeError,
			Result: &errorResult{
				Message: err.Error(),
				Kind:    client.ErrorKindNotSnap,
			},
			Status: 400,
		}, nil)
	}
	if err != nil {
		return BadRequest("cannot read snap info for %s: %s", trydir, err)
	}

	tset, err := snapstateTryPath(st, info.InstanceName(), trydir, flags)
	if err != nil {
		return errToResponse(err, []string{info.InstanceName()}, BadRequest, "cannot try %s: %s", trydir)
	}

	msg := fmt.Sprintf(i18n.G("Try %q snap from %s"), info.InstanceName(), trydir)
	chg := newChange(st, "try-snap", msg, []*state.TaskSet{tset}, []string{info.InstanceName()})
	chg.Set("api-data", map[string]string{"snap-name": info.InstanceName()})

	ensureStateSoon(st)

	return AsyncResponse(nil, &Meta{Change: chg.ID()})
}

var unsafeReadSnapInfo = unsafeReadSnapInfoImpl

func unsafeReadSnapInfoImpl(snapPath string) (*snap.Info, error) {
	// Condider using DeriveSideInfo before falling back to this!
	snapf, err := snapfile.Open(snapPath)
	if err != nil {
		return nil, err
	}
	return snap.ReadInfoFromSnapFile(snapf, nil)
}
