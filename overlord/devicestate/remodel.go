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
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
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
	initialDevice(device *auth.DeviceState) error
	// associate associates the remodel context with the change
	// and caches it
	associate(chg *state.Change)
	// setTriedRecoverySystemLabel records the label of a good recovery
	// system created during remodel
	setRecoverySystemLabel(label string)
}

// remodelCtx returns a remodeling context for the given transition.
// It constructs and caches a dedicated store as needed as well.
func remodelCtx(st *state.State, oldModel, newModel *asserts.Model) (remodelContext, error) {
	var remodCtx remodelContext

	devMgr := deviceMgr(st)

	switch kind := ClassifyRemodel(oldModel, newModel); kind {
	case UpdateRemodel:
		// simple context for the simple case
		groundCtx := groundDeviceContext{
			model:      newModel,
			systemMode: devMgr.SystemMode(SysAny),
		}
		remodCtx = &updateRemodelContext{baseRemodelContext{
			groundDeviceContext: groundCtx,

			oldModel:  oldModel,
			deviceMgr: devMgr,
			st:        st,
		}}
	case StoreSwitchRemodel:
		remodCtx = newNewStoreRemodelContext(st, devMgr, newModel, oldModel)
	case ReregRemodel:
		remodCtx = &reregRemodelContext{
			newStoreRemodelContext: newNewStoreRemodelContext(st, devMgr, newModel, oldModel),
		}
	default:
		return nil, fmt.Errorf("unsupported remodel: %s", kind)
	}

	device, err := devMgr.device()
	if err != nil {
		return nil, err
	}
	if err := remodCtx.initialDevice(device); err != nil {
		return nil, err
	}

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
	// groundDeviceContext will carry the new device model
	groundDeviceContext
	oldModel *asserts.Model

	deviceMgr *DeviceManager
	st        *state.State

	recoverySystemLabel string
}

func (rc *baseRemodelContext) ForRemodeling() bool {
	return true
}

func (rc *baseRemodelContext) GroundContext() snapstate.DeviceContext {
	return &groundDeviceContext{
		model:      rc.oldModel,
		systemMode: rc.systemMode,
	}
}

func (rc *baseRemodelContext) initialDevice(*auth.DeviceState) error {
	// do nothing
	return nil
}

func (rc *baseRemodelContext) cacheViaChange(chg *state.Change, remodCtx remodelContext) {
	chg.State().Cache(remodelCtxKey{chg.ID()}, remodCtx)
}

func (rc *baseRemodelContext) init(chg *state.Change) {
	chg.Set("new-model", string(asserts.Encode(rc.model)))
}

func (rc *baseRemodelContext) SystemMode() string {
	return rc.systemMode
}

func (rc *baseRemodelContext) setRecoverySystemLabel(label string) {
	rc.recoverySystemLabel = label
}

// updateRunModeSystem updates the device context used during boot and makes a
// record of the new seeded system.
func (rc *baseRemodelContext) updateRunModeSystem() error {
	hasSystemSeed, err := checkForSystemSeed(rc.st, &rc.groundDeviceContext)
	if err != nil {
		return fmt.Errorf("cannot look up ubuntu seed role: %w", err)
	}

	if rc.model.Grade() == asserts.ModelGradeUnset || !hasSystemSeed {
		// nothing special for non-UC20 systems or systems without a real seed
		// partition
		return nil
	}
	if rc.recoverySystemLabel == "" {
		return fmt.Errorf("internal error: recovery system label is unset during remodel finish")
	}
	// for UC20 systems we need record the fact that a new model is used for
	// booting and consider a new recovery system as as seeded
	oldDeviceContext := rc.GroundContext()
	newDeviceContext := &rc.groundDeviceContext
	err = boot.DeviceChange(oldDeviceContext, newDeviceContext, rc.st.Unlocker())
	if err != nil {
		return fmt.Errorf("cannot switch device: %v", err)
	}
	if err := rc.deviceMgr.recordSeededSystem(rc.st, &seededSystem{
		System:    rc.recoverySystemLabel,
		Model:     rc.model.Model(),
		BrandID:   rc.model.BrandID(),
		Revision:  rc.model.Revision(),
		Timestamp: rc.model.Timestamp(),
		SeedTime:  time.Now(),
	}); err != nil {
		return fmt.Errorf("cannot record a new seeded system: %v", err)
	}

	rc.st.Set("default-recovery-system", rc.recoverySystemLabel)

	if err := boot.MarkRecoveryCapableSystem(rc.recoverySystemLabel); err != nil {
		return fmt.Errorf("cannot mark system %q as recovery capable", rc.recoverySystemLabel)
	}
	return nil
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
	// nothing special to do as part of the finish action, so just run the
	// update boot step
	return rc.updateRunModeSystem()
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
}

func newNewStoreRemodelContext(st *state.State, devMgr *DeviceManager, newModel, oldModel *asserts.Model) *newStoreRemodelContext {
	rc := &newStoreRemodelContext{}
	groundCtx := groundDeviceContext{
		model:      newModel,
		systemMode: devMgr.SystemMode(SysAny),
	}
	rc.baseRemodelContext = baseRemodelContext{
		groundDeviceContext: groundCtx,
		oldModel:            oldModel,

		deviceMgr: devMgr,
		st:        st,
	}
	rc.store = devMgr.newStore(rc.deviceBackend())
	return rc
}

func (rc *newStoreRemodelContext) Kind() RemodelKind {
	return StoreSwitchRemodel
}

func (rc *newStoreRemodelContext) associate(chg *state.Change) {
	rc.remodelChange = chg
	rc.cacheViaChange(chg, rc)
}

func (rc *newStoreRemodelContext) initialDevice(device *auth.DeviceState) error {
	device1 := *device
	// we will need a new one, it might embed the store as well
	device1.SessionMacaroon = ""
	rc.deviceState = &device1
	return nil
}

func (rc *newStoreRemodelContext) init(chg *state.Change) {
	rc.baseRemodelContext.init(chg)

	chg.Set("device", rc.deviceState)
	rc.deviceState = nil
}

func (rc *newStoreRemodelContext) Init(chg *state.Change) {
	rc.init(chg)

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
	if err := rc.deviceMgr.setDevice(remodelDevice); err != nil {
		return err
	}
	return rc.updateRunModeSystem()
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
	return b.model, nil
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

// reregRemodelContext: remodel for a change of brand/model
type reregRemodelContext struct {
	*newStoreRemodelContext

	origModel  *asserts.Model
	origSerial *asserts.Serial
}

func (rc *reregRemodelContext) Kind() RemodelKind {
	return ReregRemodel
}

func (rc *reregRemodelContext) associate(chg *state.Change) {
	rc.remodelChange = chg
	rc.cacheViaChange(chg, rc)
}

func (rc *reregRemodelContext) initialDevice(device *auth.DeviceState) error {
	origModel, err := findModel(rc.st)
	if err != nil {
		return err
	}
	origSerial, err := findSerial(rc.st, nil)
	if err != nil {
		return fmt.Errorf("cannot find current serial before proceeding with re-registration: %v", err)
	}
	rc.origModel = origModel
	rc.origSerial = origSerial

	// starting almost from scratch with only device-key
	rc.deviceState = &auth.DeviceState{
		Brand: rc.model.BrandID(),
		Model: rc.model.Model(),
		KeyID: device.KeyID,
	}
	return nil
}

func (rc *reregRemodelContext) Init(chg *state.Change) {
	rc.init(chg)

	rc.associate(chg)
}

// reregRemodelContext impl of registrationContext

func (rc *reregRemodelContext) Device() (*auth.DeviceState, error) {
	return rc.device()
}

func (rc *reregRemodelContext) GadgetForSerialRequestConfig() string {
	return rc.origModel.Gadget()
}

func (rc *reregRemodelContext) SerialRequestExtraHeaders() map[string]interface{} {
	return map[string]interface{}{
		"original-brand-id": rc.origSerial.BrandID(),
		"original-model":    rc.origSerial.Model(),
		"original-serial":   rc.origSerial.Serial(),
	}
}

func (rc *reregRemodelContext) SerialRequestAncillaryAssertions() []asserts.Assertion {
	return []asserts.Assertion{rc.model, rc.origSerial}
}

func (rc *reregRemodelContext) FinishRegistration(serial *asserts.Serial) error {
	device, err := rc.device()
	if err != nil {
		return err
	}

	device.Serial = serial.Serial()
	rc.setCtxDevice(device)
	return nil
}
