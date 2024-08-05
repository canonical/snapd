// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2023 Canonical Ltd
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
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

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

// RemoveAllExcept removes all temporary files uploaded with form, except for
// the given paths. Should be called once the files uploaded with the form are
// no longer needed.
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

type uploadedContainer struct {
	// filename is the original name/path of the container file.
	filename string
	// tmpPath is the location where the temp container file is stored.
	tmpPath string
	// instanceName is optional and can only be set if only one snap was uploaded.
	instanceName string
}

// GetSnapFiles returns the original name and temp path for each snap file in
// the form. Optionally, it might include a requested instance name, but only
// if the was only one file in the form.
func (f *Form) GetSnapFiles() ([]*uploadedContainer, *apiError) {
	if len(f.FileRefs["snap"]) == 0 {
		return nil, BadRequest(`cannot find "snap" file field in provided multipart/form-data payload`)
	}

	refs := f.FileRefs["snap"]
	if len(refs) == 1 && len(f.Values["snap-path"]) > 0 {
		uploaded := &uploadedContainer{
			filename: f.Values["snap-path"][0],
			tmpPath:  refs[0].TmpPath,
		}

		if len(f.Values["name"]) > 0 {
			uploaded.instanceName = f.Values["name"][0]
		}
		return []*uploadedContainer{uploaded}, nil
	}

	snapFiles := make([]*uploadedContainer, len(refs))
	for i, ref := range refs {
		snapFiles[i] = &uploadedContainer{
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

func sideloadOrTrySnap(ctx context.Context, c *Command, body io.ReadCloser, boundary string, user *auth.UserState) Response {
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

	if len(form.Values["quota-group"]) > 0 {
		if len(form.Values["quota-group"]) != 1 {
			return BadRequest("too many names provided for 'quota-group' option")
		}
		flags.QuotaGroupName = form.Values["quota-group"][0]
	}

	flags.RemoveSnapPath = true
	flags.Unaliased = isTrue(form, "unaliased")
	flags.IgnoreRunning = isTrue(form, "ignore-running")
	trasactionVals := form.Values["transaction"]
	flags.Transaction = client.TransactionPerSnap
	if len(trasactionVals) > 0 {
		switch trasactionVals[0] {
		case string(client.TransactionPerSnap), string(client.TransactionAllSnaps):
			flags.Transaction = client.TransactionType(trasactionVals[0])
		default:
			return BadRequest(`transaction must be either %q or %q`,
				client.TransactionPerSnap, client.TransactionAllSnaps)
		}
	}

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
		chg, errRsp = sideloadManySnaps(ctx, st, snapFiles, sideloadFlags, user)
	} else {
		chg, errRsp = sideloadSnap(ctx, st, snapFiles[0], sideloadFlags)
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

// sideloadedInfo contains information from a bunch of sideloaded snaps
type sideloadedInfo struct {
	snaps      []sideloadSnapInfo
	components []sideloadComponentInfo
}

type sideloadSnapInfo struct {
	sideInfo   *snap.SideInfo
	components []sideloadComponentInfo
	origPath   string
	tmpPath    string
}

type sideloadComponentInfo struct {
	sideInfo *snap.ComponentSideInfo
	origPath string
	tmpPath  string
}

func sideloadInfo(st *state.State, uploads []*uploadedContainer, flags sideloadFlags) (*sideloadedInfo, *apiError) {
	deviceCtx, err := snapstate.DevicePastSeeding(st, nil)
	if err != nil {
		return nil, InternalError(err.Error())
	}

	var components []sideloadComponentInfo
	var snaps []sideloadSnapInfo
	for _, upload := range uploads {
		si, snapErr := readSideInfo(st, upload.tmpPath, upload.filename, flags, deviceCtx.Model())
		if snapErr != nil {
			if !flags.dangerousOK {
				// TODO:COMPS: read assertions for components
				return nil, snapErr
			}

			ci, err := readComponentInfoFromCont(upload.tmpPath, nil)
			if err != nil {
				logger.Noticef("cannot sideload as a snap: %v", snapErr)
				logger.Noticef("cannot sideload as a component: %v", err)

				// note that here we forward the error from reading the snap
				// file, rather than the component file. this is consistent with
				// what we do when installing one component from file. maybe
				// something to change?
				return nil, snapErr
			}

			components = append(components, sideloadComponentInfo{
				sideInfo: &snap.ComponentSideInfo{
					Component: ci.Component,
					Revision:  snap.Revision{},
				},
				origPath: upload.filename,
				tmpPath:  upload.tmpPath,
			})
			continue
		}

		snaps = append(snaps, sideloadSnapInfo{
			sideInfo: si,
			origPath: upload.filename,
			tmpPath:  upload.tmpPath,
		})
	}

	snapByName := func(name string) (*sideloadSnapInfo, bool) {
		for i := range snaps {
			if snaps[i].sideInfo.RealName == name {
				return &snaps[i], true
			}
		}
		return nil, false
	}

	onlyComponents := make([]sideloadComponentInfo, 0)
	for _, ci := range components {
		snapName := ci.sideInfo.Component.SnapName

		ssi, ok := snapByName(snapName)
		if !ok {
			onlyComponents = append(onlyComponents, ci)
			continue
		}

		ssi.components = append(ssi.components, ci)
	}

	return &sideloadedInfo{
		components: onlyComponents,
		snaps:      snaps,
	}, nil
}

func sideloadTaskSets(ctx context.Context, st *state.State, sideload *sideloadedInfo, userID int, flags snapstate.Flags) ([]*state.TaskSet, *apiError) {
	if flags.Transaction == client.TransactionAllSnaps && flags.Lane == 0 {
		flags.Lane = st.NewLane()
	}

	var tss []*state.TaskSet

	// handle all of the components whose snaps are not present in the set of
	// files that are being sideloaded
	for _, comp := range sideload.components {
		snapName := comp.sideInfo.Component.SnapName
		snapInfo, err := installedSnapInfo(st, snapName)
		if err != nil {
			if errors.Is(err, state.ErrNoState) {
				return nil, SnapNotInstalled(snapName, fmt.Errorf("snap owning %q not installed", comp.sideInfo.Component))
			}
			return nil, BadRequest("cannot retrieve information for %q: %v", snapName, err)
		}

		ts, err := snapstateInstallComponentPath(st, comp.sideInfo, snapInfo, comp.tmpPath, snapstate.Options{
			Flags: flags,
		})
		if err != nil {
			return nil, errToResponse(err, nil, InternalError, "cannot install component %q for snap %q: %v", comp.sideInfo.Component, snapName, err)
		}
		tss = append(tss, ts)
	}

	// handle everything else
	var pathSnaps []snapstate.PathSnap
	for _, sn := range sideload.snaps {
		comps := make(map[*snap.ComponentSideInfo]string, len(sn.components))
		for _, ci := range sn.components {
			comps[ci.sideInfo] = ci.tmpPath
		}

		pathSnaps = append(pathSnaps, snapstate.PathSnap{
			Path:       sn.tmpPath,
			SideInfo:   sn.sideInfo,
			Components: comps,
		})
	}

	_, uts, err := snapstateUpdateWithGoal(ctx, st, snapstatePathUpdateGoal(pathSnaps...), nil, snapstate.Options{
		UserID: userID,
		Flags:  flags,
	})
	if err != nil {
		return nil, errToResponse(err, nil, InternalError, "cannot install snap/component files: %v")
	}

	return append(tss, uts.Refresh...), nil
}

func sideloadManySnaps(ctx context.Context, st *state.State, uploads []*uploadedContainer, flags sideloadFlags, user *auth.UserState) (*state.Change, *apiError) {
	slInfo, apiErr := sideloadInfo(st, uploads, flags)
	if apiErr != nil {
		return nil, apiErr
	}

	var userID int
	if user != nil {
		userID = user.ID
	}

	tss, err := sideloadTaskSets(ctx, st, slInfo, userID, flags.Flags)
	if err != nil {
		return nil, err
	}

	snapNames := make([]string, 0, len(slInfo.snaps))
	snapToComps := make(map[string][]string, len(slInfo.components))
	for _, sn := range slInfo.snaps {
		snapName := sn.sideInfo.RealName
		snapNames = append(snapNames, snapName)

		if len(sn.components) == 0 {
			continue
		}

		snapToComps[snapName] = make([]string, 0, len(sn.components))
		for _, c := range sn.components {
			snapToComps[snapName] = append(snapToComps[snapName], c.sideInfo.Component.ComponentName)
		}
	}

	for _, ci := range slInfo.components {
		snapToComps[ci.sideInfo.Component.SnapName] = append(snapToComps[ci.sideInfo.Component.SnapName], ci.sideInfo.Component.ComponentName)
	}

	msg := multiPathInstallMessage(slInfo)

	chg := newChange(st, "install-snap", msg, tss, snapNames)
	apiData := make(map[string]interface{}, 0)

	if len(snapNames) > 0 {
		apiData["snap-names"] = snapNames
	}

	if len(snapToComps) > 0 {
		apiData["components"] = snapToComps
	}
	chg.Set("api-data", apiData)

	return chg, nil
}

func multiPathInstallMessage(sli *sideloadedInfo) string {
	var b strings.Builder
	switch len(sli.snaps) {
	case 0:
		b.WriteString(i18n.G("Install"))
	case 1:
		b.WriteString(i18n.G("Install snap"))
	default:
		b.WriteString(i18n.G("Install snaps"))
	}

	var paths []string
	for i, sn := range sli.snaps {
		fmt.Fprintf(&b, " %q", sn.sideInfo.RealName)
		paths = append(paths, sn.origPath)

		comps := make([]string, 0, len(sn.components))
		for _, c := range sn.components {
			comps = append(comps, c.sideInfo.Component.ComponentName)
			paths = append(paths, c.origPath)
		}

		if len(comps) > 0 {
			b.WriteString(" (")
			if len(sn.components) > 1 {
				fmt.Fprintf(&b, i18n.G("with components %s"), strutil.Quoted(comps))
			} else {
				fmt.Fprintf(&b, i18n.G("with component %s"), strutil.Quoted(comps))
			}
			b.WriteRune(')')
		}

		if i < len(sli.snaps)-1 {
			b.WriteRune(',')
		}
	}

	compNames := make([]string, 0, len(sli.components))
	for _, c := range sli.components {
		compNames = append(compNames, c.sideInfo.Component.String())
		paths = append(paths, c.origPath)
	}

	if len(sli.snaps) != 0 && len(sli.components) != 0 {
		b.WriteString(i18n.G(" and"))
	}

	switch len(sli.components) {
	case 0:
	case 1:
		fmt.Fprintf(&b, i18n.G(" component %s"), strutil.Quoted(compNames))
	default:
		fmt.Fprintf(&b, i18n.G(" components %s"), strutil.Quoted(compNames))
	}

	fmt.Fprintf(&b, " from files %s", strutil.Quoted(paths))

	return b.String()
}

func sideloadSnap(_ context.Context, st *state.State, snapFile *uploadedContainer, flags sideloadFlags) (*state.Change, *apiError) {
	var instanceName string
	if snapFile.instanceName != "" {
		// caller has specified desired instance name
		instanceName = snapFile.instanceName
		if err := snap.ValidateInstanceName(instanceName); err != nil {
			return nil, BadRequest(err.Error())
		}
	}

	deviceCtx, err := snapstate.DevicePastSeeding(st, nil)
	if err != nil {
		return nil, InternalError(err.Error())
	}

	// These two are filled only for components
	var compInfo *snap.ComponentInfo
	var snapInfo *snap.Info

	sideInfo, apiErr := readSideInfo(st, snapFile.tmpPath, snapFile.filename, flags, deviceCtx.Model())
	if apiErr != nil {
		// TODO:COMPS: installation of local but asserted components
		// needs to addressed yet. This will also help with deciding
		// whether we are dealing with a snap or a component.
		// Try to load as a component
		var compErr *apiError
		compInfo, snapInfo, compErr = readComponentInfo(st, snapFile.tmpPath, instanceName, flags)
		if compErr != nil {
			logger.Noticef("cannot sideload as a snap: %v", apiErr)
			logger.Noticef("cannot sideload as a component: %v", compErr)
			// If the snap owning the component was not found, we already read
			// the component information, so this is a valid component and we
			// report the snap not found error. Otherwise, we don't know and
			// we report the error while trying to read the file as a snap.
			if compErr.Kind == client.ErrorKindSnapNotInstalled {
				return nil, compErr
			}
			return nil, apiErr
		}
		sideInfo = &snapInfo.SideInfo
	}

	if instanceName != "" {
		requestedSnapName := snap.InstanceSnap(instanceName)
		if requestedSnapName != sideInfo.RealName {
			return nil, BadRequest(fmt.Sprintf("instance name %q does not match snap name %q", instanceName, sideInfo.RealName))
		}
	} else {
		instanceName = sideInfo.RealName
	}

	var tset *state.TaskSet
	contType := "snap"
	message := fmt.Sprintf("%q snap", instanceName)
	if compInfo == nil {
		// TODO pass per request context
		tset, _, err = snapstateInstallPath(st, sideInfo, snapFile.tmpPath, instanceName, "", flags.Flags, nil)
	} else {
		// It is a component
		contType = "component"
		message = fmt.Sprintf("%q component for %q snap",
			compInfo.Component.ComponentName, instanceName)
		tset, err = snapstateInstallComponentPath(st, snap.NewComponentSideInfo(compInfo.Component, snap.Revision{}), snapInfo, snapFile.tmpPath, snapstate.Options{
			Flags: flags.Flags,
		})
	}
	if err != nil {
		return nil, errToResponse(err, []string{sideInfo.RealName}, InternalError, "cannot install %s file: %v", contType)
	}

	msg := fmt.Sprintf(i18n.G("Install %s from file %q"), message, snapFile.filename)
	chg := newChange(st, "install-"+contType, msg, []*state.TaskSet{tset}, []string{instanceName})
	apiData := map[string]interface{}{}
	if compInfo == nil {
		apiData = map[string]interface{}{
			"snap-name":  instanceName,
			"snap-names": []string{instanceName},
		}
	} else {
		// Installing only a component, so snap name is inside components entry
		// (snap-name would be included if installing snap+components)
		apiData["components"] = map[string][]string{
			instanceName: {compInfo.Component.ComponentName},
		}
	}
	chg.Set("api-data", apiData)

	return chg, nil
}

func readSideInfo(st *state.State, tempPath string, origPath string, flags sideloadFlags, model *asserts.Model) (*snap.SideInfo, *apiError) {
	var sideInfo *snap.SideInfo

	if !flags.dangerousOK {
		si, err := snapasserts.DeriveSideInfo(tempPath, model, assertstate.DB(st))
		switch {
		case err == nil:
			sideInfo = si
		case errors.Is(err, &asserts.NotFoundError{}):
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

var readComponentInfoFromCont = readComponentInfoFromContImpl

func readComponentInfoFromContImpl(tempPath string, csi *snap.ComponentSideInfo) (*snap.ComponentInfo, error) {
	compf, err := snapfile.Open(tempPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open container: %w", err)
	}

	// hook information isn't loaded here, but it shouldn't be needed in this
	// context
	return snap.ReadComponentInfoFromContainer(compf, nil, csi)
}

// readComponentInfo reads ComponentInfo from a snap component file and the
// snap.Info of the matching installed snap. If instanceName is not empty, it
// is used to find the right instance, otherwise the SnapName from the
// component is used.
func readComponentInfo(st *state.State, tempPath, instanceName string, flags sideloadFlags) (*snap.ComponentInfo, *snap.Info, *apiError) {
	if !flags.dangerousOK {
		// TODO:COMPS: read assertions for components
		return nil, nil, BadRequest("only unasserted installation of local component with --dangerous is supported at the moment")
	}

	// TODO:COMPS: will this need to take a non-nil snap.ComponentSideInfo?
	// not sure where it would get it from, i guess whatever assertion we
	// end up receiving
	ci, err := readComponentInfoFromCont(tempPath, nil)
	if err != nil {
		return nil, nil, BadRequest("cannot read component metadata: %v", err)
	}

	// If no instance was provided in the request we use the snap name from the component
	if instanceName == "" {
		instanceName = ci.Component.SnapName
	}
	si, err := installedSnapInfo(st, instanceName)
	if err != nil {
		if errors.Is(err, state.ErrNoState) {
			return nil, nil, SnapNotInstalled(instanceName, fmt.Errorf("snap owning %q not installed", ci.Component))
		}
		return nil, nil, BadRequest("cannot retrieve information for %q: %v", instanceName, err)
	}

	return ci, si, nil
}

func installedSnapInfo(st *state.State, instanceName string) (*snap.Info, error) {
	var snapst snapstate.SnapState
	if err := snapstate.Get(st, instanceName, &snapst); err != nil {
		return nil, err
	}

	snapInfo, err := snapst.CurrentInfo()
	if err != nil {
		return nil, err
	}

	return snapInfo, nil
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
	tmpf, err := os.CreateTemp(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix+"*.snap")
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

	// TODO consider support for trying snaps plus components
	tset, err := snapstateTryPath(st, info.InstanceName(), trydir, flags)
	if err != nil {
		return errToResponse(err, []string{info.InstanceName()}, BadRequest, "cannot try %s: %s", trydir)
	}

	msg := fmt.Sprintf(i18n.G("Try %q snap from %s"), info.InstanceName(), trydir)
	chg := newChange(st, "try-snap", msg, []*state.TaskSet{tset}, []string{info.InstanceName()})
	chg.Set("api-data", map[string]interface{}{
		"snap-name":  info.InstanceName(),
		"snap-names": []string{info.InstanceName()},
	})

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
