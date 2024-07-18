// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fdestate

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
)

// ServiceManager is responsible for starting and stopping snap services.
type FDEManager struct {
	state *state.State
}

type fdeManagerKey struct{}

func Manager(st *state.State, runner *state.TaskRunner) *FDEManager {
	m := &FDEManager{
		state: st,
	}

	boot.ProvideResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, unlocker boot.Unlocker) error {
		return resealLocked(m.state, modeenv, expectReseal)
	})

	st.Lock()
	st.Cache(fdeManagerKey{}, m)
	st.Unlock()

	return m
}

func (m *FDEManager) Ensure() error {
	return nil
}

func (m *FDEManager) Stop() {
	boot.ProvideResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, unlocker boot.Unlocker) error {
		return fmt.Errorf("fde manager is disabled")
	})
}

/*
func getManager(st *state.State) (*FDEManager, error) {
	c := st.Cached(fdeManagerKey{})
	if c == nil {
		return nil, fmt.Errorf("no FDE manager found")
	}
	manager := c.(*FDEManager)
	if manager == nil {
		return nil, fmt.Errorf("FDE manager found has wrong type")
	}

	return manager, nil
}
*/

func resealWithHookLocked(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	return boot.ResealKeyToModeenvUsingFDESetupHook(dirs.GlobalRootDir, modeenv, expectReseal)
}

func resealWithSecbootLocked(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	st.Unlock()
	defer st.Lock()
	return boot.ResealKeyToModeenvSecboot(dirs.GlobalRootDir, modeenv, expectReseal)
}

func resealNextGenLocked(st *state.State, modeenv *boot.Modeenv) error {
	return fmt.Errorf("not implemented")
}

func resealLocked(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	/*
		manager, err := getManager(st)
		if err != nil {
			return err
		}
	*/

	if !boot.IsModeeenvLocked() {
		return fmt.Errorf("modeenv is not locked")
	}

	method, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	if err == device.ErrNoSealedKeys {
		return nil
	}
	if err != nil {
		return err
	}
	switch method {
	case device.SealingMethodFDESetupHook:
		return resealWithHookLocked(st, modeenv, expectReseal)
	case device.SealingMethodTPM, device.SealingMethodLegacyTPM:
		return resealWithSecbootLocked(st, modeenv, expectReseal)
	case device.SealingMethodNextGeneration:
		return resealNextGenLocked(st, modeenv)
	default:
		return fmt.Errorf("unknown key sealing method: %q", method)
	}
}

func Reseal(st *state.State, modeenv *boot.Modeenv, expectReseal bool) error {
	st.Lock()
	defer st.Unlock()

	return resealLocked(st, modeenv, expectReseal)
}

type keyDigest struct {
	Algorithm string `json:"algorithm"`
	Salt      []byte `json:"salt"`
	Digest    []byte `json:"digest"`
}

type serializedModel struct {
	SeriesValue    string             `json:"series"`
	BrandIDValue   string             `json:"brand-id"`
	ModelValue     string             `json:"model"`
	ClassicValue   bool               `json:"classic"`
	GradeValue     asserts.ModelGrade `json:"grade"`
	SignKeyIDValue string             `json:"sign-key-id"`
}

func (m *serializedModel) Series() string {
	return m.SeriesValue
}

func (m *serializedModel) BrandID() string {
	return m.BrandIDValue
}

func (m *serializedModel) Model() string {
	return m.ModelValue
}

func (m *serializedModel) Classic() bool {
	return m.ClassicValue
}

func (m *serializedModel) Grade() asserts.ModelGrade {
	return m.GradeValue
}

func (m *serializedModel) SignKeyID() string {
	return m.SignKeyIDValue
}

var _ secboot.ModelForSealing = (*serializedModel)(nil)

func wrapModel(m secboot.ModelForSealing) *serializedModel {
	return &serializedModel{
		SeriesValue:    m.Series(),
		BrandIDValue:   m.BrandID(),
		ModelValue:     m.Model(),
		ClassicValue:   m.Classic(),
		GradeValue:     m.Grade(),
		SignKeyIDValue: m.SignKeyID(),
	}
}

type keyslotRoleParams struct {
	Models         []serializedModel `yaml:"models,omitempty"`
	BootModes      []string          `yaml:"boot-modes,omitempty"`
	Tpm2PcrProfile []byte            `yaml:"tpm2-pcr-profile,omitempty"`
}

type keyslotRoleInfo struct {
	Params                  keyslotRoleParams            `yaml:"params"`
	ContainerSpecificParams map[string]keyslotRoleParams `yaml:"container-specific-params"`
}

func (m *FDEManager) getPlatformKeyDigest() *keyDigest {
	var ret keyDigest
	m.state.Get("platform-key-digest", &ret)
	return &ret
}

func (m *FDEManager) setPlatformKeyDigest(value *keyDigest) {
	m.state.Set("platform-key-digest", value)
}

func (m *FDEManager) getKeyslotRoleInfo(role string) *keyslotRoleInfo {
	var ret map[string]*keyslotRoleInfo
	m.state.Get("keyslot-role-infos", &ret)
	return ret[role]
}

func (m *FDEManager) setKeyslotRoleInfo(role string, value *keyslotRoleInfo) {
	var ret map[string]*keyslotRoleInfo
	m.state.Get("keyslot-role-infos", &ret)
	if value == nil {
		delete(ret, role)
	} else {
		ret[role] = value
	}
	m.state.Set("keyslot-role-infos", &ret)
}

func (m *FDEManager) getTpm2PcrPolicyRevocationCounter() uint64 {
	var ret uint64
	m.state.Get("tpm2-pcr-policy-revocation-counter", &ret)
	return ret
}

func (m *FDEManager) setTpm2PcrPolicyRevocationCounter(value uint64) {
	m.state.Set("tpm2-pcr-policy-revocation-counter", value)
}

func init() {
	boot.ProvideResealKeyToModeenv(func(rootdir string, modeenv *boot.Modeenv, expectReseal bool, unlocker boot.Unlocker) error {
		return fmt.Errorf("fde manager is disabled")
	})
}
