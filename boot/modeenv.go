// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2022 Canonical Ltd
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

package boot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/mvo5/goconfigparser"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snapdenv"
)

type bootAssetsMap map[string][]string

// bootCommandLines is a list of kernel command lines. The command lines are
// marshalled as JSON as a comma can be present in the module parameters.
type bootCommandLines []string

// Modeenv is a file on UC20 that provides additional information
// about the current mode (run,recover,install)
type Modeenv struct {
	Mode           string `key:"mode"`
	RecoverySystem string `key:"recovery_system"`
	// CurrentRecoverySystems is a list of labels corresponding to recovery
	// systems that have been tested or are in the process of being tried,
	// thus only the run key is resealed for these systems.
	CurrentRecoverySystems []string `key:"current_recovery_systems"`
	// GoodRecoverySystems is a list of labels corresponding to recovery
	// systems that were tested and are prepared to use for recovering.
	// The fallback keys are resealed for these systems.
	GoodRecoverySystems []string `key:"good_recovery_systems"`
	Base                string   `key:"base"`
	TryBase             string   `key:"try_base"`
	BaseStatus          string   `key:"base_status"`
	// Gadget is the currently active gadget snap
	Gadget         string   `key:"gadget"`
	CurrentKernels []string `key:"current_kernels"`
	// Model, BrandID, Grade, SignKeyID describe the properties of current
	// device model.
	Model          string `key:"model"`
	BrandID        string `key:"model,secondary"`
	Classic        bool   `key:"classic"`
	Grade          string `key:"grade"`
	ModelSignKeyID string `key:"model_sign_key_id"`
	// TryModel, TryBrandID, TryGrade, TrySignKeyID describe the properties
	// of the candidate model.
	TryModel          string `key:"try_model"`
	TryBrandID        string `key:"try_model,secondary"`
	TryGrade          string `key:"try_grade"`
	TryModelSignKeyID string `key:"try_model_sign_key_id"`
	// BootFlags is the set of boot flags. Whether this applies for the current
	// or next boot is not indicated in the modeenv. When the modeenv is read in
	// the initramfs these flags apply to the current boot and are copied into
	// a file in /run that userspace should read instead of reading from this
	// key. When setting boot flags for the next boot, then this key will be
	// written to and used by the initramfs after rebooting.
	BootFlags []string `key:"boot_flags"`
	// CurrentTrustedBootAssets is a map of a run bootloader's asset names to
	// a list of hashes of the asset contents. Typically the first entry in
	// the list is a hash of an asset the system currently boots with (or is
	// expected to have booted with). The second entry, if present, is the
	// hash of an entry added when an update of the asset was being applied
	// and will become the sole entry after a successful boot.
	CurrentTrustedBootAssets bootAssetsMap `key:"current_trusted_boot_assets"`
	// CurrentTrustedRecoveryBootAssetsMap is a map of a recovery bootloader's
	// asset names to a list of hashes of the asset contents. Used similarly
	// to CurrentTrustedBootAssets.
	CurrentTrustedRecoveryBootAssets bootAssetsMap `key:"current_trusted_recovery_boot_assets"`
	// CurrentKernelCommandLines is a list of the expected kernel command
	// lines when booting into run mode. It will typically only be one
	// element for normal operations, but may contain two elements during
	// update scenarios.
	CurrentKernelCommandLines bootCommandLines `key:"current_kernel_command_lines"`
	// TODO:UC20 add a per recovery system list of kernel command lines

	// read is set to true when a modenv was read successfully
	read bool

	// originRootdir is set to the root whence the modeenv was
	// read from, and where it will be written back to
	originRootdir string

	// extrakeys is all the keys in the modeenv we read from the file but don't
	// understand, we keep track of this so that if we read a new modeenv with
	// extra keys and need to rewrite it, we will write those new keys as well
	extrakeys map[string]string
}

var modeenvKnownKeys = make(map[string]bool)

func init() {
	st := reflect.TypeOf(Modeenv{})
	num := st.NumField()
	for i := 0; i < num; i++ {
		f := st.Field(i)
		if f.PkgPath != "" {
			// unexported
			continue
		}
		key := f.Tag.Get("key")
		if key == "" {
			panic(fmt.Sprintf("modeenv %s field has no key tag", f.Name))
		}
		const secondaryModifier = ",secondary"
		if strings.HasSuffix(key, secondaryModifier) {
			// secondary field in a group fields
			// corresponding to one file key
			key := key[:len(key)-len(secondaryModifier)]
			if !modeenvKnownKeys[key] {
				panic(fmt.Sprintf("modeenv %s field marked as secondary for not yet defined key %q", f.Name, key))
			}
			continue
		}
		if modeenvKnownKeys[key] {
			panic(fmt.Sprintf("modeenv key %q repeated on %s", key, f.Name))
		}
		modeenvKnownKeys[key] = true
	}
}

func modeenvFile(rootdir string) string {
	if rootdir == "" {
		rootdir = dirs.GlobalRootDir
	}
	return dirs.SnapModeenvFileUnder(rootdir)
}

// ReadModeenv attempts to read the modeenv file at
// <rootdir>/var/lib/snapd/modeenv.
func ReadModeenv(rootdir string) (*Modeenv, error) {
	if snapdenv.Preseeding() {
		return nil, fmt.Errorf("internal error: modeenv cannot be read during preseeding")
	}

	modeenvPath := modeenvFile(rootdir)
	cfg := goconfigparser.New()
	cfg.AllowNoSectionHeader = true
	mylog.Check(cfg.ReadFile(modeenvPath))

	// TODO:UC20: should we check these errors and try to do something?
	m := Modeenv{
		read:          true,
		originRootdir: rootdir,
		extrakeys:     make(map[string]string),
	}
	unmarshalModeenvValueFromCfg(cfg, "recovery_system", &m.RecoverySystem)
	unmarshalModeenvValueFromCfg(cfg, "current_recovery_systems", &m.CurrentRecoverySystems)
	unmarshalModeenvValueFromCfg(cfg, "good_recovery_systems", &m.GoodRecoverySystems)
	unmarshalModeenvValueFromCfg(cfg, "boot_flags", &m.BootFlags)

	unmarshalModeenvValueFromCfg(cfg, "mode", &m.Mode)
	if m.Mode == "" {
		return nil, fmt.Errorf("internal error: mode is unset")
	}
	unmarshalModeenvValueFromCfg(cfg, "base", &m.Base)
	unmarshalModeenvValueFromCfg(cfg, "base_status", &m.BaseStatus)
	unmarshalModeenvValueFromCfg(cfg, "gadget", &m.Gadget)
	unmarshalModeenvValueFromCfg(cfg, "try_base", &m.TryBase)

	// current_kernels is a comma-delimited list in a string
	unmarshalModeenvValueFromCfg(cfg, "current_kernels", &m.CurrentKernels)
	var bm modeenvModel
	unmarshalModeenvValueFromCfg(cfg, "model", &bm)
	m.BrandID = bm.brandID
	m.Model = bm.model
	unmarshalModeenvValueFromCfg(cfg, "classic", &m.Classic)
	// expect the caller to validate the grade
	unmarshalModeenvValueFromCfg(cfg, "grade", &m.Grade)
	unmarshalModeenvValueFromCfg(cfg, "model_sign_key_id", &m.ModelSignKeyID)
	var tryBm modeenvModel
	unmarshalModeenvValueFromCfg(cfg, "try_model", &tryBm)
	m.TryBrandID = tryBm.brandID
	m.TryModel = tryBm.model
	unmarshalModeenvValueFromCfg(cfg, "try_grade", &m.TryGrade)
	unmarshalModeenvValueFromCfg(cfg, "try_model_sign_key_id", &m.TryModelSignKeyID)

	unmarshalModeenvValueFromCfg(cfg, "current_trusted_boot_assets", &m.CurrentTrustedBootAssets)
	unmarshalModeenvValueFromCfg(cfg, "current_trusted_recovery_boot_assets", &m.CurrentTrustedRecoveryBootAssets)
	unmarshalModeenvValueFromCfg(cfg, "current_kernel_command_lines", &m.CurrentKernelCommandLines)

	// save all the rest of the keys we don't understand
	keys := mylog.Check2(cfg.Options(""))

	for _, k := range keys {
		if !modeenvKnownKeys[k] {
			val := mylog.Check2(cfg.Get("", k))

			m.extrakeys[k] = val
		}
	}

	return &m, nil
}

// deepEqual compares two modeenvs to ensure they are textually the same. It
// does not consider whether the modeenvs were read from disk or created purely
// in memory. It also does not sort or otherwise mutate any sub-objects,
// performing simple strict verification of sub-objects.
func (m *Modeenv) deepEqual(m2 *Modeenv) bool {
	b := mylog.Check2(json.Marshal(m))

	b2 := mylog.Check2(json.Marshal(m2))

	return bytes.Equal(b, b2)
}

// Copy will make a deep copy of a Modeenv.
func (m *Modeenv) Copy() (*Modeenv, error) {
	// to avoid hard-coding all fields here and manually copying everything, we
	// take the easy way out and serialize to json then re-import into a
	// empty Modeenv
	b := mylog.Check2(json.Marshal(m))

	m2 := &Modeenv{}
	mylog.Check(json.Unmarshal(b, m2))

	// manually copy the unexported fields as they won't be in the JSON
	m2.read = m.read
	m2.originRootdir = m.originRootdir
	return m2, nil
}

// Write outputs the modeenv to the file where it was read, only valid on
// modeenv that has been read.
func (m *Modeenv) Write() error {
	if m.read {
		return m.WriteTo(m.originRootdir)
	}
	return fmt.Errorf("internal error: must use WriteTo with modeenv not read from disk")
}

// WriteTo outputs the modeenv to the file at <rootdir>/var/lib/snapd/modeenv.
func (m *Modeenv) WriteTo(rootdir string) error {
	if snapdenv.Preseeding() {
		return fmt.Errorf("internal error: modeenv cannot be written during preseeding")
	}

	modeenvPath := modeenvFile(rootdir)
	mylog.Check(os.MkdirAll(filepath.Dir(modeenvPath), 0755))

	buf := bytes.NewBuffer(nil)
	if m.Mode == "" {
		return fmt.Errorf("internal error: mode is unset")
	}
	marshalModeenvEntryTo(buf, "mode", m.Mode)
	marshalModeenvEntryTo(buf, "recovery_system", m.RecoverySystem)
	marshalModeenvEntryTo(buf, "current_recovery_systems", m.CurrentRecoverySystems)
	marshalModeenvEntryTo(buf, "good_recovery_systems", m.GoodRecoverySystems)
	marshalModeenvEntryTo(buf, "boot_flags", m.BootFlags)
	marshalModeenvEntryTo(buf, "base", m.Base)
	marshalModeenvEntryTo(buf, "try_base", m.TryBase)
	marshalModeenvEntryTo(buf, "base_status", m.BaseStatus)
	marshalModeenvEntryTo(buf, "gadget", m.Gadget)
	marshalModeenvEntryTo(buf, "current_kernels", strings.Join(m.CurrentKernels, ","))
	if m.Model != "" || m.Grade != "" {
		if m.Model == "" {
			return fmt.Errorf("internal error: model is unset")
		}
		if m.BrandID == "" {
			return fmt.Errorf("internal error: brand is unset")
		}
		marshalModeenvEntryTo(buf, "model", &modeenvModel{brandID: m.BrandID, model: m.Model})
	}
	if m.Classic {
		marshalModeenvEntryTo(buf, "classic", true)
	}
	// TODO: complain when grade or key are unset
	marshalModeenvEntryTo(buf, "grade", m.Grade)
	marshalModeenvEntryTo(buf, "model_sign_key_id", m.ModelSignKeyID)
	if m.TryModel != "" || m.TryGrade != "" {
		if m.TryModel == "" {
			return fmt.Errorf("internal error: try model is unset")
		}
		if m.TryBrandID == "" {
			return fmt.Errorf("internal error: try brand is unset")
		}
		marshalModeenvEntryTo(buf, "try_model", &modeenvModel{brandID: m.TryBrandID, model: m.TryModel})
	}
	marshalModeenvEntryTo(buf, "try_grade", m.TryGrade)
	marshalModeenvEntryTo(buf, "try_model_sign_key_id", m.TryModelSignKeyID)
	marshalModeenvEntryTo(buf, "current_trusted_boot_assets", m.CurrentTrustedBootAssets)
	marshalModeenvEntryTo(buf, "current_trusted_recovery_boot_assets", m.CurrentTrustedRecoveryBootAssets)
	marshalModeenvEntryTo(buf, "current_kernel_command_lines", m.CurrentKernelCommandLines)

	// write all the extra keys at the end
	// sort them for test convenience
	extraKeys := make([]string, 0, len(m.extrakeys))
	for k := range m.extrakeys {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		marshalModeenvEntryTo(buf, k, m.extrakeys[k])
	}
	mylog.Check(osutil.AtomicWriteFile(modeenvPath, buf.Bytes(), 0644, 0))

	return nil
}

// modelForSealing is a helper type that implements
// github.com/snapcore/secboot.SnapModel interface.
type modelForSealing struct {
	brandID        string
	model          string
	classic        bool
	grade          asserts.ModelGrade
	modelSignKeyID string
}

// verify interface match
var _ secboot.ModelForSealing = (*modelForSealing)(nil)

func (m *modelForSealing) BrandID() string           { return m.brandID }
func (m *modelForSealing) SignKeyID() string         { return m.modelSignKeyID }
func (m *modelForSealing) Model() string             { return m.model }
func (m *modelForSealing) Classic() bool             { return m.classic }
func (m *modelForSealing) Grade() asserts.ModelGrade { return m.grade }
func (m *modelForSealing) Series() string            { return release.Series }

// modelUniqueID returns a unique ID which can be used as a map index of the
// provided model.
func modelUniqueID(m secboot.ModelForSealing) string {
	return fmt.Sprintf("%s/%s,%s,%s", m.BrandID(), m.Model(), m.Grade(), m.SignKeyID())
}

// ModelForSealing returns a wrapper implementing
// github.com/snapcore/secboot.SnapModel interface which describes the current
// model.
func (m *Modeenv) ModelForSealing() secboot.ModelForSealing {
	return &modelForSealing{
		brandID:        m.BrandID,
		model:          m.Model,
		classic:        m.Classic,
		grade:          asserts.ModelGrade(m.Grade),
		modelSignKeyID: m.ModelSignKeyID,
	}
}

// TryModelForSealing returns a wrapper implementing
// github.com/snapcore/secboot.SnapModel interface which describes the candidate
// or try model.
func (m *Modeenv) TryModelForSealing() secboot.ModelForSealing {
	return &modelForSealing{
		brandID:        m.TryBrandID,
		model:          m.TryModel,
		classic:        m.Classic,
		grade:          asserts.ModelGrade(m.TryGrade),
		modelSignKeyID: m.TryModelSignKeyID,
	}
}

func (m *Modeenv) setModel(model *asserts.Model) {
	m.Model = model.Model()
	m.BrandID = model.BrandID()
	m.Grade = string(model.Grade())
	m.ModelSignKeyID = model.SignKeyID()
}

func (m *Modeenv) setTryModel(model *asserts.Model) {
	m.TryModel = model.Model()
	m.TryBrandID = model.BrandID()
	m.TryGrade = string(model.Grade())
	m.TryModelSignKeyID = model.SignKeyID()
}

func (m *Modeenv) clearTryModel() {
	m.TryModel = ""
	m.TryBrandID = ""
	m.TryGrade = ""
	m.TryModelSignKeyID = ""
}

type modeenvValueMarshaller interface {
	MarshalModeenvValue() (string, error)
}

type modeenvValueUnmarshaller interface {
	UnmarshalModeenvValue(value string) error
}

// marshalModeenvEntryTo marshals to out what as value for an entry
// with the given key. If what is empty this is a no-op.
func marshalModeenvEntryTo(out io.Writer, key string, what interface{}) error {
	var asString string
	switch v := what.(type) {
	case string:
		if v == "" {
			return nil
		}
		asString = v
	case []string:
		if len(v) == 0 {
			return nil
		}
		asString = asModeenvStringList(v)
	case bool:
		asString = strconv.FormatBool(v)
	default:
		if vm, ok := what.(modeenvValueMarshaller); ok {
			marshalled := mylog.Check2(vm.MarshalModeenvValue())

			asString = marshalled
		} else if jm, ok := what.(json.Marshaler); ok {
			marshalled := mylog.Check2(jm.MarshalJSON())

			asString = string(marshalled)
			if asString == "null" {
				//  no need to keep nulls in the modeenv
				return nil
			}
		} else {
			return fmt.Errorf("internal error: cannot marshal unsupported type %T value %v for key %q", what, what, key)
		}
	}
	_ := mylog.Check2(fmt.Fprintf(out, "%s=%s\n", key, asString))
	return err
}

// unmarshalModeenvValueFromCfg unmarshals the value of the entry with
// the given key to dest. If there's no such entry dest might be left
// empty.
func unmarshalModeenvValueFromCfg(cfg *goconfigparser.ConfigParser, key string, dest interface{}) error {
	if dest == nil {
		return fmt.Errorf("internal error: cannot unmarshal to nil")
	}
	kv, _ := cfg.Get("", key)

	switch v := dest.(type) {
	case *string:
		*v = kv
	case *[]string:
		*v = splitModeenvStringList(kv)
	case *bool:
		if kv == "" {
			*v = false
			return nil
		}

		*v = mylog.Check2(strconv.ParseBool(kv))

	default:
		if vm, ok := v.(modeenvValueUnmarshaller); ok {
			mylog.Check(vm.UnmarshalModeenvValue(kv))

			return nil
		} else if jm, ok := v.(json.Unmarshaler); ok {
			if len(kv) == 0 {
				// leave jm empty
				return nil
			}
			mylog.Check(jm.UnmarshalJSON([]byte(kv)))

			return nil
		}
		return fmt.Errorf("internal error: cannot unmarshal value %q for unsupported type %T", kv, dest)
	}
	return nil
}

func splitModeenvStringList(v string) []string {
	if v == "" {
		return nil
	}
	split := strings.Split(v, ",")
	// drop empty strings
	nonEmpty := make([]string, 0, len(split))
	for _, one := range split {
		if one != "" {
			nonEmpty = append(nonEmpty, one)
		}
	}
	if len(nonEmpty) == 0 {
		return nil
	}
	return nonEmpty
}

func asModeenvStringList(v []string) string {
	return strings.Join(v, ",")
}

type modeenvModel struct {
	brandID, model string
}

func (m *modeenvModel) MarshalModeenvValue() (string, error) {
	return fmt.Sprintf("%s/%s", m.brandID, m.model), nil
}

func (m *modeenvModel) UnmarshalModeenvValue(brandSlashModel string) error {
	if bsmSplit := strings.SplitN(brandSlashModel, "/", 2); len(bsmSplit) == 2 {
		if bsmSplit[0] != "" && bsmSplit[1] != "" {
			m.brandID = bsmSplit[0]
			m.model = bsmSplit[1]
		}
	}
	return nil
}

func (b bootAssetsMap) MarshalJSON() ([]byte, error) {
	asMap := map[string][]string(b)
	return json.Marshal(asMap)
}

func (b *bootAssetsMap) UnmarshalJSON(data []byte) error {
	var asMap map[string][]string
	mylog.Check(json.Unmarshal(data, &asMap))

	*b = bootAssetsMap(asMap)
	return nil
}

func (s bootCommandLines) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string(s))
}

func (s *bootCommandLines) UnmarshalJSON(data []byte) error {
	var asList []string
	mylog.Check(json.Unmarshal(data, &asList))

	*s = bootCommandLines(asList)
	return nil
}
