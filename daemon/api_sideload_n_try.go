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
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"mime/multipart"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
)

type Form struct {
	Values   map[string][]string
	FileRefs map[string][]*FileReference
}

type FileReference struct {
	Filename string
	TmpPath  string
}

func (f *Form) RemoveAll() {
	for _, refs := range f.FileRefs {
		for _, ref := range refs {
			if err := os.Remove(ref.TmpPath); err != nil {
				logger.Noticef("cannot remove temporary file: %v", err)
			}
		}
	}
}

// SnapFileNameAndPath returns the original file path/name and the path to
// where the temp file is written.
func (f *Form) SnapFileNameAndPath() (name, path string, apiErr *apiError) {
	if len(f.FileRefs["snap"]) == 0 {
		return "", "", BadRequest(`cannot find "snap" file field in provided multipart/form-data payload`)
	}

	snapFile := f.FileRefs["snap"][0]
	name, path = snapFile.Filename, snapFile.TmpPath

	if len(f.Values["snap-path"]) > 0 {
		name = f.Values["snap-path"][0]
	}

	return name, path, nil
}

func sideloadOrTrySnap(c *Command, body io.ReadCloser, boundary string) Response {
	route := c.d.router.Get(stateChangeCmd.Path)
	if route == nil {
		return InternalError("cannot find route for change")
	}

	// POSTs to sideload snaps must be a multipart/form-data file upload.
	mpReader := multipart.NewReader(body, boundary)
	form, errRsp := readForm(mpReader)
	if errRsp != nil {
		return errRsp
	}

	// we are in charge of the tempfile life cycle until we hand it off to the change
	changeTriggered := false
	defer func() {
		if !changeTriggered {
			form.RemoveAll()
		}
	}()
	dangerousOK := isTrue(form, "dangerous")
	flags, err := modeFlags(isTrue(form, "devmode"), isTrue(form, "jailmode"), isTrue(form, "classic"))
	if err != nil {
		return BadRequest(err.Error())
	}

	if len(form.Values["action"]) > 0 && form.Values["action"][0] == "try" {
		if len(form.Values["snap-path"]) == 0 {
			return BadRequest("need 'snap-path' value in form")
		}
		return trySnap(c.d.overlord.State(), form.Values["snap-path"][0], flags)
	}
	flags.RemoveSnapPath = true

	flags.Unaliased = isTrue(form, "unaliased")
	flags.IgnoreRunning = isTrue(form, "ignore-running")
	systemRestartImmediate := isTrue(form, "system-restart-immediate")

	origPath, tempPath, errRsp := form.SnapFileNameAndPath()
	if errRsp != nil {
		return errRsp
	}

	var instanceName string
	if len(form.Values["name"]) > 0 {
		// caller has specified desired instance name
		instanceName = form.Values["name"][0]
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
	if systemRestartImmediate {
		chg.Set("system-restart-immediate", true)
	}
	chg.Set("api-data", map[string]string{"snap-name": instanceName})

	ensureStateSoon(st)

	// only when the unlock succeeds (as opposed to panicing) is the handoff done
	// but this is good enough
	changeTriggered = true

	return AsyncResponse(nil, chg.ID())
}

// maxReadBuflen is the maximum buffer size for reading the non-file parts in the snap upload form
const maxReadBuflen = 1024 * 1024

// readForm returns a Form populated with values (for non-file parts) and file headers (for file
// parts). The file headers contain the original file name and a path to the persisted file in
// dirs.SnapDirBlob. If an error occurs and a non-nil Response is returned, an attempt is made
// to remove temp files.
func readForm(reader *multipart.Reader) (_ *Form, apiErr *apiError) {
	maxMemory := int64(maxReadBuflen)
	form := &Form{
		Values:   make(map[string][]string),
		FileRefs: make(map[string][]*FileReference),
	}

	// clean up if we're failing the request
	defer func() {
		if apiErr != nil {
			form.RemoveAll()
		}
	}()

	for {
		part, err := reader.NextPart()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, BadRequest("cannot read POST form: %v", err)
		}

		name := part.FormName()
		if name == "" {
			continue
		}

		filename := part.FileName()
		if filename == "" {
			// non-file parts are kept in memory
			buf := &bytes.Buffer{}

			// copy one byte more than the max so we know if it exceeds the limit
			n, err := io.CopyN(buf, part, maxMemory+1)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, BadRequest("cannot read form data: %v", err)
			}

			maxMemory -= n
			if maxMemory < 0 {
				return nil, BadRequest("cannot read form data: exceeds memory limit")
			}

			form.Values[name] = append(form.Values[name], buf.String())
			continue
		}

		tmpPath, err := writeToTempFile(part)

		// add it to the form even if err != nil, so it gets deleted
		ref := &FileReference{TmpPath: tmpPath, Filename: filename}
		form.FileRefs[name] = append(form.FileRefs[name], ref)

		if err != nil {
			return nil, InternalError(err.Error())
		}
	}

	// sync the parent directory where the files were written to
	if len(form.FileRefs) > 0 {
		dir, err := os.Open(dirs.SnapBlobDir)
		if err != nil {
			return nil, InternalError("cannot open parent dir of temp files: %v", err)
		}
		defer func() {
			if cerr := dir.Close(); err == nil && cerr != nil {
				err = InternalError("cannot close parent dir of temp files: %v", cerr)
			}
		}()

		if err := dir.Sync(); err != nil {
			return nil, InternalError("cannot sync parent dir of temp files: %v", err)
		}
	}

	return form, nil
}

// writeToTempFile writes the contents of reader to a temp file and returns
// its path. If the path is not empty then a file was written and it's the
// caller's responsibility to clean it up (even if the error is non-nil).
func writeToTempFile(reader io.Reader) (path string, err error) {
	tmpf, err := ioutil.TempFile(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix)
	if err != nil {
		return "", fmt.Errorf("cannot create temp file for form data file part: %v", err)
	}
	defer func() {
		if cerr := tmpf.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("cannot close temp file: %v", cerr)
		}
	}()

	// TODO: limit the file part size by wrapping it w/ http.MaxBytesReader
	if _, err = io.Copy(tmpf, reader); err != nil {
		return tmpf.Name(), fmt.Errorf("cannot write file part: %v", err)
	}

	if err := tmpf.Sync(); err != nil {
		return tmpf.Name(), fmt.Errorf("cannot sync file: %v", err)
	}

	return tmpf.Name(), nil
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
		return &apiError{
			Status:  400,
			Message: err.Error(),
			Kind:    client.ErrorKindNotSnap,
		}
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

	return AsyncResponse(nil, chg.ID())
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
