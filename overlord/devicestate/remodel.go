// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storecontext"
)

/*

This is the central logic to setup and mediate the access to the to-be
device state and dedicated store during remodeling and drive the
re-registration, leveraging the snapstate.DeviceContext/DeviceCtx and
storecontext.DeviceBackend mechanisms and also registrationContext.

Different context implementations will be used depending on the kind
of remodel, and those will play the roles/implement as needed
snapstate.DeviceContext, storecontext.DeviceBackend and
registrationContext:

* same brand/model, brand store => updateRemodel
  this is just a contextual carrier for the new model

* same brand/model different brand store => storeSwitchRemodel this
  mediates access to device state kept on the remodel change, it also
  creates a store that uses that and refers to the new brand store

* different brand/model, maybe different brand store => reregRemodel
  similar to storeSwitchRemodel case after a first phase that performs
  re-registration where the context plays registrationContext's role
  (NOT IMPLEMENTED YET)

*/

// RemodelKind designates a kind of remodeling.
type RemodelKind int

const (
	// same brand/model, brand store
	UpdateRemodel RemodelKind = iota
	// same brand/model, different brand store
	StoreSwitchRemodel
	// different brand/model, maybe different brand store
	ReregRemodel
)

func (k RemodelKind) String() string {
	switch k {
	case UpdateRemodel:
		return "revision update remodel"
	case StoreSwitchRemodel:
		return "store switch remodel"
	case ReregRemodel:
		return "re-registration remodel"
	}
	panic(fmt.Sprintf("internal error: unknown remodel kind: %d", k))
}

// ClassifyRemodel returns what kind of remodeling is going from oldModel to newModel.
func ClassifyRemodel(oldModel, newModel *asserts.Model) RemodelKind {
	if oldModel.BrandID() != newModel.BrandID() {
		return ReregRemodel
	}
	if oldModel.Model() != newModel.Model() {
		return ReregRemodel
	}
	if oldModel.Store() != newModel.Store() {
		return StoreSwitchRemodel
	}
	return UpdateRemodel
}

type remodelCtxKey struct {
	chgID string
}

func cachedRemodelCtx(chg *state.Change) (remodelContext, bool) {
	key := remodelCtxKey{chg.ID()}
	remodCtx, ok := chg.State().Cached(key).(remodelContext)
	return remodCtx, ok
}

func cleanupRemodelCtx(chg *state.Change) {
	chg.State().Cache(remodelCtxKey{chg.ID()}, nil)
}

// A remodelContext mediates the correct and isolated device state
// access and evolution during a remodel.
// All remodelContexts are at least a DeviceContext.
type remodelContext interface {
	Init(chg *state.Change)
	Finish() error
	snapstate.DeviceContext

	Kind() RemodelKind

	// initialDevice takes the current/initial device state
	// when setting up the remodel context
	initialDevice(device *auth.DeviceState)
	// associate associates the remodel context with the change
	// and caches it
	associate(chg *state.Change)
}

// remodelCtx returns a remodeling context for the given transition.
// It constructs and caches a dedicated store as needed as well.
func remodelCtx(st *state.State, oldModel, newModel *asserts.Model) (remodelContext, error) {
	var remodCtx remodelContext

	devMgr := deviceMgr(st)

	switch kind := ClassifyRemodel(oldModel, newModel); kind {
	case UpdateRemodel:
		// simple context for the simple case
		remodCtx = &updateRemodelContext{baseRemodelContext{newModel}}
	case StoreSwitchRemodel:
		storeSwitchCtx := &newStoreRemodelContext{
			baseRemodelContext: baseRemodelContext{newModel},
			st:                 st,
			deviceMgr:          devMgr,
		}
		storeSwitchCtx.store = devMgr.newStore(storeSwitchCtx.deviceBackend())
		remodCtx = storeSwitchCtx
	// TODO: support ReregRemodel
	default:
		return nil, fmt.Errorf("unsupported remodel: %s", kind)
	}

	device, err := devMgr.device()
	if err != nil {
		return nil, err
	}
	remodCtx.initialDevice(device)

	return remodCtx, nil
}

// remodelCtxFromTask returns a possibly cached remodeling context associated
// with the task via its change, if task is nil or the task change
// is not a remodeling it will return ErrNoState.
func remodelCtxFromTask(t *state.Task) (remodelContext, error) {
	if t == nil {
		return nil, state.ErrNoState
	}
	chg := t.Change()
	if chg == nil {
		return nil, state.ErrNoState
	}

	var encNewModel string
	if err := chg.Get("new-model", &encNewModel); err != nil {
		return nil, err
	}

	// shortcut, cached?
	if remodCtx, ok := cachedRemodelCtx(chg); ok {
		return remodCtx, nil
	}

	st := t.State()
	oldModel, err := findModel(st)
	if err != nil {
		return nil, fmt.Errorf("internal error: cannot find old model during remodel: %v", err)
	}
	newModelA, err := asserts.Decode([]byte(encNewModel))
	if err != nil {
		return nil, err
	}
	newModel, ok := newModelA.(*asserts.Model)
	if !ok {
		return nil, fmt.Errorf("internal error: cannot use a remodel new-model, wrong type")
	}

	remodCtx, err := remodelCtx(st, oldModel, newModel)
	if err != nil {
		return nil, err
	}
	remodCtx.associate(chg)
	return remodCtx, nil
}

type baseRemodelContext struct {
	newModel *asserts.Model
}

func (rc baseRemodelContext) ForRemodeling() bool {
	return true
}

func (rc baseRemodelContext) Model() *asserts.Model {
	return rc.newModel
}

func (rc baseRemodelContext) initialDevice(*auth.DeviceState) {
	// do nothing
}

func (rc baseRemodelContext) cacheViaChange(chg *state.Change, remodCtx remodelContext) {
	chg.State().Cache(remodelCtxKey{chg.ID()}, remodCtx)
}

func (rc baseRemodelContext) init(chg *state.Change) {
	chg.Set("new-model", string(asserts.Encode(rc.newModel)))
}

// updateRemodelContext: model assertion revision-only update remodel
// (no change to brand/model or store)
type updateRemodelContext struct {
	baseRemodelContext
}

func (rc *updateRemodelContext) Kind() RemodelKind {
	return UpdateRemodel
}

func (rc *updateRemodelContext) associate(chg *state.Change) {
	rc.cacheViaChange(chg, rc)
}

func (rc *updateRemodelContext) Init(chg *state.Change) {
	rc.init(chg)

	rc.associate(chg)
}

func (rc *updateRemodelContext) Store() snapstate.StoreService {
	return nil
}

func (rc *updateRemodelContext) Finish() error {
	// nothing more to do
	return nil
}

// newStoreRemodelContext: remodel needing a new store session
// (for change of store (or brand/model))
type newStoreRemodelContext struct {
	baseRemodelContext

	// device state storage before this is associate with a change
	deviceState *auth.DeviceState
	// the associated change
	remodelChange *state.Change

	store snapstate.StoreService

	st        *state.State
	deviceMgr *DeviceManager
}

func (rc *newStoreRemodelContext) Kind() RemodelKind {
	return StoreSwitchRemodel
}

func (rc *newStoreRemodelContext) associate(chg *state.Change) {
	rc.remodelChange = chg
	rc.cacheViaChange(chg, rc)
}

func (rc *newStoreRemodelContext) initialDevice(device *auth.DeviceState) {
	device1 := *device
	// we will need a new one, it might embed the store as well
	device1.SessionMacaroon = ""
	rc.deviceState = &device1
}

func (rc *newStoreRemodelContext) Init(chg *state.Change) {
	rc.init(chg)
	chg.Set("device", rc.deviceState)
	rc.deviceState = nil

	rc.associate(chg)
}

func (rc *newStoreRemodelContext) Store() snapstate.StoreService {
	return rc.store
}

func (rc *newStoreRemodelContext) device() (*auth.DeviceState, error) {
	var err error
	var device auth.DeviceState
	if rc.remodelChange == nil {
		// no remodelChange yet
		device = *rc.deviceState
	} else {
		err = rc.remodelChange.Get("device", &device)
	}
	return &device, err
}

func (rc *newStoreRemodelContext) setCtxDevice(device *auth.DeviceState) {
	if rc.remodelChange == nil {
		// no remodelChange yet
		rc.deviceState = device
	} else {
		rc.remodelChange.Set("device", device)
	}
}

func (rc *newStoreRemodelContext) Finish() error {
	// expose the device state of the remodel with the new session
	// to the rest of the system
	remodelDevice, err := rc.device()
	if err != nil {
		return err
	}
	return rc.deviceMgr.setDevice(remodelDevice)
}

func (rc *newStoreRemodelContext) deviceBackend() storecontext.DeviceBackend {
	return &remodelDeviceBackend{rc}
}

type remodelDeviceBackend struct {
	*newStoreRemodelContext
}

func (b remodelDeviceBackend) Device() (*auth.DeviceState, error) {
	return b.device()
}

func (b remodelDeviceBackend) SetDevice(device *auth.DeviceState) error {
	b.setCtxDevice(device)
	return nil
}

func (b remodelDeviceBackend) Model() (*asserts.Model, error) {
	return b.newModel, nil
}

func (b remodelDeviceBackend) Serial() (*asserts.Serial, error) {
	// this the shared logic, also correct for the rereg case
	// we should lookup the serial with the remodeling device state
	device, err := b.device()
	if err != nil {
		return nil, err
	}
	return findSerial(b.st, device)
}
