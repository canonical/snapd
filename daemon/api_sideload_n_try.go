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
	"context"
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
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/strutil"
)

// Form is a multipart form that holds file and non-file parts
type Form struct {
	// Values holds non-file parts keyed by their "name" parameter (from the
	// part's Content-Disposition header).
	Values map[string][]string

	// FileRefs holds file parts keyed by their "name" parameter (from the
	// part's Content-Disposition header). Each reference contains a filename
	// (the "filename" parameter) and the path to a file with the part's contents.
	FileRefs map[string][]*FileReference
}

type FileReference struct {
	Filename string
	TmpPath  string
}

func (f *Form) RemoveAllExcept(paths []string) {
	for _, refs := range f.FileRefs {
		for _, ref := range refs {
			if strutil.ListContains(paths, ref.TmpPath) {
				continue
			}

			if err := os.Remove(ref.TmpPath); err != nil {
				logger.Noticef("cannot remove temporary file: %v", err)
			}
		}
	}
}

type uploadedSnap struct {
	// filename is the original name/path of the snap file.
	filename string
	// tmpPath is the location where the temp snap file is stored.
	tmpPath string
	// instanceName is optional and can only be set if only one snap was uploaded.
	instanceName string
}

// GetSnapFiles returns the original name and temp path for each snap file in
// the form. Optionally, it might include a requested instance name, but only
// if the was only one file in the form.
func (f *Form) GetSnapFiles() ([]*uploadedSnap, *apiError) {
	if len(f.FileRefs["snap"]) == 0 {
		return nil, BadRequest(`cannot find "snap" file field in provided multipart/form-data payload`)
	}

	refs := f.FileRefs["snap"]
	if len(refs) == 1 && len(f.Values["snap-path"]) > 0 {
		uploaded := &uploadedSnap{
			filename: f.Values["snap-path"][0],
			tmpPath:  refs[0].TmpPath,
		}

		if len(f.Values["name"]) > 0 {
			uploaded.instanceName = f.Values["name"][0]
		}
		return []*uploadedSnap{uploaded}, nil
	}

	snapFiles := make([]*uploadedSnap, len(refs))
	for i, ref := range refs {
		snapFiles[i] = &uploadedSnap{
			filename: ref.Filename,
			tmpPath:  ref.TmpPath,
		}
	}

	return snapFiles, nil
}

type sideloadFlags struct {
	snapstate.Flags
	dangerousOK bool
}

func sideloadOrTrySnap(c *Command, body io.ReadCloser, boundary string, user *auth.UserState) Response {
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

	// we are in charge of the temp files, until they're handed off to the change
	var pathsToNotRemove []string
	defer func() {
		form.RemoveAllExcept(pathsToNotRemove)
	}()

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

	sideloadFlags := sideloadFlags{
		Flags:       flags,
		dangerousOK: isTrue(form, "dangerous"),
	}

	snapFiles, errRsp := form.GetSnapFiles()
	if errRsp != nil {
		return errRsp
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	var chg *state.Change
	if len(snapFiles) > 1 {
		chg, errRsp = sideloadManySnaps(st, snapFiles, sideloadFlags, user)
	} else {
		chg, errRsp = sideloadSnap(st, snapFiles[0], sideloadFlags)
	}
	if errRsp != nil {
		return errRsp
	}

	chg.Set("system-restart-immediate", isTrue(form, "system-restart-immediate"))

	ensureStateSoon(st)

	// the handoff is only done when the unlock succeeds (instead of panicking)
	// but this is good enough
	pathsToNotRemove = make([]string, len(snapFiles))
	for i, snapFile := range snapFiles {
		pathsToNotRemove[i] = snapFile.tmpPath
	}

	return AsyncResponse(nil, chg.ID())
}

func sideloadManySnaps(st *state.State, snapFiles []*uploadedSnap, flags sideloadFlags, user *auth.UserState) (*state.Change, *apiError) {
	sideInfos := make([]*snap.SideInfo, len(snapFiles))
	names := make([]string, len(snapFiles))
	tempPaths := make([]string, len(snapFiles))
	origPaths := make([]string, len(snapFiles))

	for i, snapFile := range snapFiles {
		si, apiError := readSideInfo(st, snapFile.tmpPath, snapFile.filename, flags)
		if apiError != nil {
			return nil, apiError
		}

		sideInfos[i] = si
		names[i] = si.RealName
		tempPaths[i] = snapFile.tmpPath
		origPaths[i] = snapFile.filename
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	tss, err := snapstateInstallPathMany(context.TODO(), st, sideInfos, tempPaths, userID, &flags.Flags)
	if err != nil {
		return nil, errToResponse(err, tempPaths, InternalError, "cannot install snap files: %v")
	}

	msg := fmt.Sprintf(i18n.G("Install snaps %s from files %s"), strutil.Quoted(names), strutil.Quoted(origPaths))
	chg := newChange(st, "install-snap", msg, tss, names)
	chg.Set("api-data", map[string][]string{"snap-names": names})

	return chg, nil
}

func sideloadSnap(st *state.State, snapFile *uploadedSnap, flags sideloadFlags) (*state.Change, *apiError) {
	var instanceName string
	if snapFile.instanceName != "" {
		// caller has specified desired instance name
		instanceName = snapFile.instanceName
		if err := snap.ValidateInstanceName(instanceName); err != nil {
			return nil, BadRequest(err.Error())
		}
	}

	sideInfo, apiErr := readSideInfo(st, snapFile.tmpPath, snapFile.filename, flags)
	if apiErr != nil {
		return nil, apiErr
	}

	if instanceName != "" {
		requestedSnapName := snap.InstanceSnap(instanceName)
		if requestedSnapName != sideInfo.RealName {
			return nil, BadRequest(fmt.Sprintf("instance name %q does not match snap name %q", instanceName, sideInfo.RealName))
		}
	} else {
		instanceName = sideInfo.RealName
	}

	tset, _, err := snapstateInstallPath(st, sideInfo, snapFile.tmpPath, instanceName, "", flags.Flags)
	if err != nil {
		return nil, errToResponse(err, []string{sideInfo.RealName}, InternalError, "cannot install snap file: %v")
	}

	msg := fmt.Sprintf(i18n.G("Install %q snap from file %q"), instanceName, snapFile.filename)
	chg := newChange(st, "install-snap", msg, []*state.TaskSet{tset}, []string{instanceName})
	chg.Set("api-data", map[string]string{"snap-name": instanceName})

	return chg, nil
}

func readSideInfo(st *state.State, tempPath string, origPath string, flags sideloadFlags) (*snap.SideInfo, *apiError) {
	var sideInfo *snap.SideInfo

	if !flags.dangerousOK {
		si, err := snapasserts.DeriveSideInfo(tempPath, assertstate.DB(st))
		switch {
		case err == nil:
			sideInfo = si
		case asserts.IsNotFound(err):
			// with devmode we try to find assertions but it's ok
			// if they are not there (implies --dangerous)
			if !flags.DevMode {
				msg := "cannot find signatures with metadata for snap"
				if origPath != "" {
					msg = fmt.Sprintf("%s %q", msg, origPath)
				}
				return nil, BadRequest(msg)
			}
			// TODO: set a warning if devmode
		default:
			return nil, BadRequest(err.Error())
		}
	}

	if sideInfo == nil {
		// potentially dangerous but dangerous or devmode params were set
		info, err := unsafeReadSnapInfo(tempPath)
		if err != nil {
			return nil, BadRequest("cannot read snap file: %v", err)
		}
		sideInfo = &snap.SideInfo{RealName: info.SnapName()}
	}
	return sideInfo, nil
}

// maxReadBuflen is the maximum buffer size for reading the non-file parts in the snap upload form
const maxReadBuflen = 1024 * 1024

// readForm returns a Form populated with values (for non-file parts) and file headers (for file
// parts). The file headers contain the original file name and a path to the persisted file in
// dirs.SnapDirBlob. If an error occurs and a non-nil Response is returned, an attempt is made
// to remove temp files.
func readForm(reader *multipart.Reader) (_ *Form, apiErr *apiError) {
	availMemory := int64(maxReadBuflen)
	form := &Form{
		Values:   make(map[string][]string),
		FileRefs: make(map[string][]*FileReference),
	}

	// clean up if we're failing the request
	defer func() {
		if apiErr != nil {
			form.RemoveAllExcept(nil)
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
			n, err := io.CopyN(buf, part, availMemory+1)
			if err != nil && !errors.Is(err, io.EOF) {
				return nil, BadRequest("cannot read form data: %v", err)
			}

			availMemory -= n
			if availMemory < 0 {
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
			if cerr := dir.Close(); apiErr == nil && cerr != nil {
				apiErr = InternalError("cannot close parent dir of temp files: %v", cerr)
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
