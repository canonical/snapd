// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/state"
)

var (
	serialModelCmd = &Command{
		Path:        "/v2/model/serial",
		GET:         getSerial,
		POST:        postSerial,
		ReadAccess:  openAccess{},
		WriteAccess: rootAccess{},
	}
	modelCmd = &Command{
		Path:        "/v2/model",
		POST:        postModel,
		GET:         getModel,
		ReadAccess:  openAccess{},
		WriteAccess: rootAccess{},
	}
)

var (
	devicestateRemodel = devicestate.Remodel
	sideloadSnapsInfo  = sideloadInfo
)

type postModelData struct {
	NewModel string `json:"new-model"`
	Offline  bool   `json:"offline"`
}

func postModel(c *Command, r *http.Request, _ *auth.UserState) Response {
	contentType := r.Header.Get("Content-Type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		// assume json body, as type was not enforced in the past
		mediaType = "application/json"
	}

	switch mediaType {
	case "application/json":
		// If json content type we get only the new model assertion and
		// the rest is either downloaded from the store or already installed.
		return remodelJSON(c, r)
	case "multipart/form-data":
		// multipart/form-data content type can be used to sideload
		// part of the things necessary for a remodel.
		return remodelForm(c, r, params)
	default:
		return BadRequest("unexpected media type %q", mediaType)
	}
}

func modelFromData(data []byte) (*asserts.Model, error) {
	rawNewModel, err := asserts.Decode(data)
	if err != nil {
		return nil, fmt.Errorf("cannot decode new model assertion: %v", err)
	}
	newModel, ok := rawNewModel.(*asserts.Model)
	if !ok {
		return nil, fmt.Errorf("new model is not a model assertion: %v", rawNewModel.Type())
	}

	return newModel, nil
}

func remodelJSON(c *Command, r *http.Request) Response {
	var data postModelData
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&data); err != nil {
		return BadRequest("cannot decode request body into remodel operation: %v", err)
	}
	newModel, err := modelFromData([]byte(data.NewModel))
	if err != nil {
		return BadRequest(err.Error())
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	chg, err := devicestateRemodel(st, newModel, nil, nil, devicestate.RemodelOptions{
		Offline: data.Offline,
	})
	if err != nil {
		return BadRequest("cannot remodel device: %v", err)
	}
	ensureStateSoon(st)

	return AsyncResponse(nil, chg.ID())
}

func readOfflineRemodelForm(form *Form) (*asserts.Model, []*uploadedSnap, *asserts.Batch, *apiError) {
	// New model
	model := form.Values["new-model"]
	if len(model) != 1 {
		return nil, nil, nil,
			BadRequest("one model assertion is expected (%d found)", len(model))
	}
	newModel, err := modelFromData([]byte(model[0]))
	if err != nil {
		return nil, nil, nil, BadRequest(err.Error())
	}

	// Snap files
	var snapFiles []*uploadedSnap
	if len(form.FileRefs["snap"]) > 0 {
		snaps, errRsp := form.GetSnapFiles()
		if errRsp != nil {
			return nil, nil, nil, errRsp
		}
		snapFiles = snaps
	}

	// Assertions
	formAsserts := form.Values["assertion"]
	batch := asserts.NewBatch(nil)
	for _, a := range formAsserts {
		_, err := batch.AddStream(strings.NewReader(a))
		if err != nil {
			return nil, nil, nil, BadRequest("cannot decode assertion: %v", err)
		}
	}

	return newModel, snapFiles, batch, nil
}

func startOfflineRemodelChange(st *state.State, newModel *asserts.Model,
	snapFiles []*uploadedSnap, batch *asserts.Batch, pathsToNotRemove *[]string) (
	*state.Change, *apiError) {

	st.Lock()
	defer st.Unlock()

	// Include assertions in the DB, we need them as soon as
	// we create the snap.SideInfo struct in sideloadSnapsInfo.
	if err := assertstate.AddBatch(st, batch,
		&asserts.CommitOptions{Precheck: true}); err != nil {
		return nil, BadRequest("error committing assertions: %v", err)
	}

	// Build snaps information. Note that here we do not set flags as we
	// expect all snaps to have assertions (although maybe we will need to
	// consider the classic snaps case in the future).
	slInfo, apiErr := sideloadSnapsInfo(st, snapFiles, sideloadFlags{})
	if apiErr != nil {
		return nil, apiErr
	}

	*pathsToNotRemove = make([]string, len(slInfo.sideInfos))
	for i, psi := range slInfo.sideInfos {
		// Move file to the same name of what a downloaded one would have
		dest := filepath.Join(dirs.SnapBlobDir,
			fmt.Sprintf("%s_%s.snap", psi.RealName, psi.Revision))
		os.Rename(slInfo.tmpPaths[i], dest)
		// Avoid trying to remove a file that does not exist anymore
		(*pathsToNotRemove)[i] = slInfo.tmpPaths[i]
		slInfo.tmpPaths[i] = dest
	}

	// Now create and start the remodel change
	chg, err := devicestateRemodel(st, newModel, slInfo.sideInfos, slInfo.tmpPaths, devicestate.RemodelOptions{
		// since this is the codepath that parses the form, offline is implcit
		// because local snaps are being provided.
		Offline: true,
	})
	if err != nil {
		return nil, BadRequest("cannot remodel device: %v", err)
	}
	ensureStateSoon(st)

	return chg, nil
}

func remodelForm(c *Command, r *http.Request, contentTypeParams map[string]string) Response {
	boundary := contentTypeParams["boundary"]
	mpReader := multipart.NewReader(r.Body, boundary)
	form, errRsp := readForm(mpReader)
	if errRsp != nil {
		return errRsp
	}

	// we are in charge of the temp files, until they're handed off to the change
	var pathsToNotRemove []string

	// TODO: temp files are not removed if devicestate.Remodel returns an error
	// right now. change this to work how postSystemsActionForm does it.
	defer func() {
		form.RemoveAllExcept(pathsToNotRemove)
	}()

	// Read needed form data
	newModel, snapFiles, batch, errRsp := readOfflineRemodelForm(form)
	if errRsp != nil {
		return errRsp
	}

	// Create and start the change using the form data
	chg, errRsp := startOfflineRemodelChange(c.d.overlord.State(),
		newModel, snapFiles, batch, &pathsToNotRemove)
	if errRsp != nil {
		return errRsp
	}

	return AsyncResponse(nil, chg.ID())
}

// getModel gets the current model assertion using the DeviceManager
func getModel(c *Command, r *http.Request, _ *auth.UserState) Response {
	opts, err := parseHeadersFormatOptionsFromURL(r.URL.Query())
	if err != nil {
		return BadRequest(err.Error())
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	devmgr := c.d.overlord.DeviceManager()

	model, err := devmgr.Model()
	if errors.Is(err, state.ErrNoState) {
		return &apiError{
			Status:  404,
			Message: "no model assertion yet",
			Kind:    client.ErrorKindAssertionNotFound,
			Value:   "model",
		}
	}
	if err != nil {
		return InternalError("accessing model failed: %v", err)
	}

	if opts.jsonResult {
		modelJSON := clientutil.ModelAssertJSON{}

		modelJSON.Headers = model.Headers()
		if !opts.headersOnly {
			modelJSON.Body = string(model.Body())
		}

		return SyncResponse(modelJSON)
	}

	return AssertResponse([]asserts.Assertion{model}, false)
}

// getSerial gets the current serial assertion using the DeviceManager
func getSerial(c *Command, r *http.Request, _ *auth.UserState) Response {
	opts, err := parseHeadersFormatOptionsFromURL(r.URL.Query())
	if err != nil {
		return BadRequest(err.Error())
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	devmgr := c.d.overlord.DeviceManager()

	serial, err := devmgr.Serial()
	if errors.Is(err, state.ErrNoState) {
		return &apiError{
			Status:  404,
			Message: "no serial assertion yet",
			Kind:    client.ErrorKindAssertionNotFound,
			Value:   "serial",
		}
	}
	if err != nil {
		return InternalError("accessing serial failed: %v", err)
	}

	if opts.jsonResult {
		serialJSON := clientutil.ModelAssertJSON{}

		serialJSON.Headers = serial.Headers()
		if !opts.headersOnly {
			serialJSON.Body = string(serial.Body())
		}

		return SyncResponse(serialJSON)
	}

	return AssertResponse([]asserts.Assertion{serial}, false)
}

type postSerialData struct {
	Action                    string `json:"action"`
	NoRegistrationUntilReboot bool   `json:"no-registration-until-reboot"`
}

var devicestateDeviceManagerUnregister = (*devicestate.DeviceManager).Unregister

func postSerial(c *Command, r *http.Request, _ *auth.UserState) Response {
	var postData postSerialData

	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&postData); err != nil {
		return BadRequest("cannot decode serial action data from request body: %v", err)
	}
	if decoder.More() {
		return BadRequest("spurious content after serial action")
	}
	switch postData.Action {
	case "forget":
	case "":
		return BadRequest("missing serial action")
	default:
		return BadRequest("unsupported serial action %q", postData.Action)
	}

	st := c.d.overlord.State()
	st.Lock()
	defer st.Unlock()

	devmgr := c.d.overlord.DeviceManager()

	unregOpts := &devicestate.UnregisterOptions{
		NoRegistrationUntilReboot: postData.NoRegistrationUntilReboot,
	}
	err := devicestateDeviceManagerUnregister(devmgr, unregOpts)
	if err != nil {
		return InternalError("forgetting serial failed: %v", err)
	}

	return SyncResponse(nil)
}
