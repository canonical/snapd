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
	"github.com/snapcore/snapd/overlord/swfeats"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/strutil"
)

var (
	installComponentChangeKind = swfeats.RegisterChangeKind("install-component")
	trySnapChangeKind          = swfeats.RegisterChangeKind("try-snap")
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
	// instanceName is optional and can only be set if only one snap or
	// component was uploaded.
	instanceName string
	// componentName is optional and can only be set if one component was
	// uploaded. instanceName must be set if componentName is set.
	// be derived from the filename.
	componentName string
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

		if len(f.Values["component-name"]) > 0 {
			if uploaded.instanceName == "" {
				return nil, BadRequest("snap name must be provided if component name is provided")
			}
			uploaded.componentName = f.Values["component-name"][0]
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
	// snaps contains the set of snaps that should be sideloaded. Any components
	// associated with these snaps that are being sideloaded will be inside of
	// the sideloadSnapInfo for that snap.
	snaps []sideloadSnapInfo
	// components contains the set of components that should be sideloaded, but
	// their associated snaps are not being sideloaded (they must already be
	// installed).
	components []sideloadComponentInfo
}

type sideloadSnapInfo struct {
	info       *snap.Info
	components []sideloadComponentInfo
	origPath   string
	tmpPath    string
}

type sideloadComponentInfo struct {
	snapInfo *snap.Info
	sideInfo *snap.ComponentSideInfo
	origPath string
	tmpPath  string
}

func sideloadInfo(st *state.State, uploads []*uploadedContainer, flags sideloadFlags) (*sideloadedInfo, *apiError) {
	deviceCtx, err := snapstate.DevicePastSeeding(st, nil)
	if err != nil {
		return nil, InternalError(err.Error())
	}

	// keep track of uploads that might be components and the errors that were
	// found when trying to read them as snaps. we cannot just use a map here
	// because we want to maintain the order of the uploads.
	potentialComponents := make([]*uploadedContainer, 0)
	snapParsingErrors := make(map[string]*apiError)

	var snaps []sideloadSnapInfo
	for _, upload := range uploads {
		info, snapErr := readInfoAndDeriveSideInfo(st, upload.tmpPath, upload.filename, flags, deviceCtx.Model())
		if snapErr != nil {
			// if we can't parse the blob as a snap, it might be a component.
			potentialComponents = append(potentialComponents, upload)
			snapParsingErrors[upload.tmpPath] = snapErr
			continue
		}

		snaps = append(snaps, sideloadSnapInfo{
			info:     info,
			origPath: upload.filename,
			tmpPath:  upload.tmpPath,
		})
	}

	// this function is used to look up the snap.Info for components that we're
	// installing. it is used for both dangerous and non-dangerous installs. for
	// non-dangerous installs, we verify that the component we're sideloading is
	// valid with either a snap that is being sideloaded or the snap that is
	// already installed.
	uploadedOrInstalledSnapFunc := uploadedOrInstalledSnapInfoMatcher(st, snaps)

	var components []sideloadComponentInfo
	for _, upload := range potentialComponents {
		snapErr, ok := snapParsingErrors[upload.tmpPath]
		if !ok {
			return nil, InternalError("internal error: cannot find original error parsing blob as snap")
		}

		// TODO: for non-dangerous installs, we will hash the blob twice. once
		// as a snap, once as a component. make it so we only hash it once.
		compInfo, snapInfo, compErr := readComponentInfoAndDeriveSideInfo(st, upload, flags, deviceCtx.Model(), uploadedOrInstalledSnapFunc)
		if compErr != nil {
			logger.Noticef("cannot sideload as a snap: %v", snapErr)
			logger.Noticef("cannot sideload as a component: %v", compErr)
			if compErr.Kind == client.ErrorKindSnapNotInstalled || compErr.Kind == client.ErrorKindMissingSnapResourcePair {
				return nil, compErr
			}
			return nil, snapErr
		}

		components = append(components, sideloadComponentInfo{
			snapInfo: snapInfo,
			sideInfo: &compInfo.ComponentSideInfo,
			origPath: upload.filename,
			tmpPath:  upload.tmpPath,
		})
	}

	// we use this function here to get a pointer to an element in the snaps
	// slice so that we can modify it. we can't just create a mapping in the
	// above loop since the pointers could get invalidated the snaps slice
	// growing.
	snapByName := func(name string) (*sideloadSnapInfo, bool) {
		for i := range snaps {
			if snaps[i].info.RealName == name {
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
		ts, err := snapstateInstallComponentPath(st, comp.sideInfo, comp.snapInfo, comp.tmpPath, snapstate.Options{
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
		comps := make([]snapstate.PathComponent, 0, len(sn.components))
		for _, ci := range sn.components {
			comps = append(comps, snapstate.PathComponent{
				SideInfo: ci.sideInfo,
				Path:     ci.tmpPath,
			})
		}

		pathSnaps = append(pathSnaps, snapstate.PathSnap{
			Path:       sn.tmpPath,
			SideInfo:   &sn.info.SideInfo,
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
		snapName := sn.info.RealName
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

	chg := newChange(st, installSnapChangeKind, msg, tss, snapNames)
	apiData := make(map[string]any, 0)

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
		fmt.Fprintf(&b, " %q", sn.info.RealName)
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

func sideloadSnap(_ context.Context, st *state.State, upload *uploadedContainer, flags sideloadFlags) (*state.Change, *apiError) {
	var instanceName string
	if upload.instanceName != "" {
		// caller has specified desired instance name
		instanceName = upload.instanceName
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

	model := deviceCtx.Model()

	info, snapErr := readInfoAndDeriveSideInfo(st, upload.tmpPath, upload.filename, flags, model)
	if snapErr != nil {
		// if we can't read the blob as a snap, then we try to read it as a
		// component.
		// TODO: for non-dangerous installs we hash the blob twice here. consider
		// only doing that once.
		var compErr *apiError
		compInfo, snapInfo, compErr = readComponentInfoAndDeriveSideInfo(st, upload, flags, model, installedSnapInfoMatcher(st))
		if compErr != nil {
			logger.Noticef("cannot sideload as a snap: %v", snapErr)
			logger.Noticef("cannot sideload as a component: %v", compErr)
			// If the snap owning the component was not found, we already read
			// the component information, so this is a valid component and we
			// report the snap not found error. Otherwise, we don't know and
			// we report the error while trying to read the file as a snap.
			if compErr.Kind == client.ErrorKindSnapNotInstalled || compErr.Kind == client.ErrorKindMissingSnapResourcePair {
				return nil, compErr
			}
			return nil, snapErr
		}
		info = snapInfo
	}

	if instanceName != "" {
		requestedSnapName := snap.InstanceSnap(instanceName)
		if requestedSnapName != info.RealName {
			return nil, BadRequest(fmt.Sprintf("instance name %q does not match snap name %q", instanceName, info.RealName))
		}
	} else {
		instanceName = info.RealName
	}

	var tset *state.TaskSet
	contType := "snap"
	var changeType string
	message := fmt.Sprintf("%q snap", instanceName)
	if compInfo == nil {
		// TODO pass per request context
		tset, _, err = snapstateInstallPath(st, &info.SideInfo, upload.tmpPath, instanceName, "", flags.Flags, nil)
		changeType = installSnapChangeKind
	} else {
		// It is a component
		contType = "component"
		message = fmt.Sprintf("%q component for %q snap",
			compInfo.Component.ComponentName, instanceName)
		tset, err = snapstateInstallComponentPath(st, &compInfo.ComponentSideInfo, snapInfo, upload.tmpPath, snapstate.Options{
			Flags: flags.Flags,
		})
		changeType = installComponentChangeKind
	}
	if err != nil {
		return nil, errToResponse(err, []string{info.RealName}, InternalError, "cannot install %s file: %v", contType)
	}

	msg := fmt.Sprintf(i18n.G("Install %s from file %q"), message, upload.filename)
	chg := newChange(st, changeType, msg, []*state.TaskSet{tset}, []string{instanceName})
	apiData := map[string]any{}
	if compInfo == nil {
		apiData = map[string]any{
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

func readInfoAndDeriveSideInfo(st *state.State, tempPath string, origPath string, flags sideloadFlags, model *asserts.Model) (*snap.Info, *apiError) {
	if flags.dangerousOK {
		info, err := unsafeReadSnapInfo(tempPath)
		if err != nil {
			return nil, BadRequest("cannot read snap file: %v", err)
		}
		info.SideInfo = snap.SideInfo{RealName: info.SnapName()}
		return info, nil
	}

	si, err := snapasserts.DeriveSideInfo(tempPath, model, assertstate.DB(st))
	if err != nil {
		if !errors.Is(err, &asserts.NotFoundError{}) {
			return nil, BadRequest(err.Error())
		}

		// with devmode we try to find assertions but it's ok
		// if they are not there (implies --dangerous)
		if !flags.DevMode {
			msg := "cannot find signatures with metadata for snap/component"
			if origPath != "" {
				msg = fmt.Sprintf("%s %q", msg, origPath)
			}
			return nil, BadRequest(msg)
		}
	}

	info, err := unsafeReadSnapInfo(tempPath)
	if err != nil {
		return nil, BadRequest("cannot read snap file: %v", err)
	}

	// might be nil if snapasserts.DeriveSideInfo returned an error and we're
	// doing a devmode install
	if si != nil {
		info.SideInfo = *si
	} else {
		info.SideInfo = snap.SideInfo{RealName: info.SnapName()}
	}

	return info, nil
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

// readComponentInfoAndDeriveSideInfo reads ComponentInfo from a snap component file and the
// snap.Info of the matching installed snap.
//
// For dangerous installs, we use the component and snap name from the
// component.yaml file inside the blob. If an upload.instanceName is provided,
// then the component in installed for that snap instance.
//
// For non-dangerous installs, we use either the provided snap and component
// names, or we attempt to derive the snap and component names from the provided
// blob's filename.
func readComponentInfoAndDeriveSideInfo(
	st *state.State,
	upload *uploadedContainer,
	flags sideloadFlags,
	model *asserts.Model,
	matchingSnap func(instanceName string, cref naming.ComponentRef) (*snap.Info, *apiError),
) (*snap.ComponentInfo, *snap.Info, *apiError) {
	if flags.dangerousOK {
		return readComponentInfoDangerous(upload, matchingSnap)
	}

	// either use the component ref that is provided by the caller, or do our
	// best to infer it from the filename of the assumed component file
	var cref naming.ComponentRef
	var instanceName string

	// if a component name was provided, then a snap (or possible instance) name
	// must have also been provided
	if upload.componentName == "" {
		ref, err := naming.ComponentRefFromSnapPackFilename(filepath.Base(upload.filename))
		if err != nil {
			return nil, nil, BadRequest("cannot infer component name from filename: %v", upload.filename, err)
		}
		cref = ref
		instanceName = ref.SnapName
	} else {
		snapName, _ := snap.SplitInstanceName(upload.instanceName)
		cref = naming.NewComponentRef(snapName, upload.componentName)
	}

	// we should still override the potentially derived snap name with the given
	// instance name if we have one
	if upload.instanceName != "" {
		instanceName = upload.instanceName
	}

	info, apiErr := matchingSnap(instanceName, cref)
	if apiErr != nil {
		return nil, nil, apiErr
	}

	db := assertstate.DB(st)

	csi, err := snapasserts.DeriveComponentSideInfo(cref.ComponentName, upload.tmpPath, info, model, db)
	if err != nil {
		if !errors.Is(err, &asserts.NotFoundError{}) {
			return nil, nil, BadRequest(err.Error())
		}

		msg := "cannot find signatures with metadata for snap/component"
		if upload.filename != "" {
			msg = fmt.Sprintf("%s %q", msg, upload.filename)
		}

		if !flags.DevMode {
			if upload.filename != "" {
				msg = fmt.Sprintf("%s %q", msg, upload.filename)
			}
			return nil, nil, BadRequest(msg)
		}

		csi = snap.NewComponentSideInfo(cref, snap.Revision{})
	}

	// make sure that we've got a resource pair for this component and snap
	// revision. installing via snapstate checks this too, but we might as well
	// fail early.
	if !flags.DevMode {
		if _, err := assertstate.SnapResourcePair(st, csi, info); err != nil {
			return nil, nil, MissingSnapResourcePair(csi, info.Revision)
		}
	}

	// this should be impossible since we're looking up the assertions based on
	// the hash of the component file, but check just in case
	if csi.Component != cref {
		return nil, nil, BadRequest("component name in filename does not match component name in metadata")
	}

	compInfo, err := readComponentInfoFromCont(upload.tmpPath, csi)
	if err != nil {
		return nil, nil, BadRequest("cannot read component metadata: %v", err)
	}

	return compInfo, info, nil
}

func readComponentInfoDangerous(
	upload *uploadedContainer,
	matchingSnap func(instanceName string, cref naming.ComponentRef) (*snap.Info, *apiError),
) (*snap.ComponentInfo, *snap.Info, *apiError) {
	compInfo, err := readComponentInfoFromCont(upload.tmpPath, nil)
	if err != nil {
		return nil, nil, BadRequest("cannot read component metadata: %v", err)
	}

	compInfo.ComponentSideInfo = snap.ComponentSideInfo{
		Component: compInfo.Component,
		Revision:  snap.R(0),
	}

	// if no instance was provided in the request we use the snap name from
	// the component
	instanceName := upload.instanceName
	if instanceName == "" {
		instanceName = compInfo.Component.SnapName
	}

	info, apiErr := matchingSnap(instanceName, compInfo.Component)
	if apiErr != nil {
		return nil, nil, apiErr
	}

	return compInfo, info, nil
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

// installedSnapInfoMatcher returns a function that looks for the components
// associated snap in the set of installed snaps.
func installedSnapInfoMatcher(st *state.State) func(string, naming.ComponentRef) (*snap.Info, *apiError) {
	return func(instanceName string, cref naming.ComponentRef) (*snap.Info, *apiError) {
		info, err := installedSnapInfo(st, instanceName)
		if err != nil {
			if errors.Is(err, state.ErrNoState) {
				return nil, SnapNotInstalled(instanceName, fmt.Errorf("snap owning %q not installed", cref))
			}
			return nil, BadRequest("cannot retrieve information for %q: %v", instanceName, err)
		}
		return info, nil
	}
}

// uploadedOrInstalledSnapInfoMatcher returns a function that can be used to
// match a snap name against a set of snaps that were uploaded. If the snap is
// not found in the uploads, then it falls back to looking at the installed set
// of snaps.
func uploadedOrInstalledSnapInfoMatcher(st *state.State, snaps []sideloadSnapInfo) func(string, naming.ComponentRef) (*snap.Info, *apiError) {
	return func(instanceName string, cref naming.ComponentRef) (*snap.Info, *apiError) {
		for _, sn := range snaps {
			// this is safe, since instance names are not supported when
			// sideloading multiple blobs at once
			if sn.info.RealName == instanceName {
				return sn.info, nil
			}
		}

		info, err := installedSnapInfo(st, instanceName)
		if err != nil {
			if errors.Is(err, state.ErrNoState) {
				return nil, SnapNotInstalled(instanceName, fmt.Errorf("snap owning %q is neither installed nor provided to sideload", cref))
			}
			return nil, BadRequest("cannot retrieve information for %q: %v", instanceName, err)
		}
		return info, nil
	}
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
	tmpf, err := os.CreateTemp(dirs.SnapBlobDir, dirs.LocalInstallBlobTempPrefix+"*")
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
	chg := newChange(st, trySnapChangeKind, msg, []*state.TaskSet{tset}, []string{info.InstanceName()})
	chg.Set("api-data", map[string]any{
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
