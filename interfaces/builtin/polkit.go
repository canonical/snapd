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

package builtin

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	_ "golang.org/x/crypto/sha3"
	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/interfaces/apparmor"
	"github.com/snapcore/snapd/interfaces/polkit"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/polkit/validate"
	"github.com/snapcore/snapd/snap"
)

const polkitSummary = `allows installing polkit rules and/or access to polkitd to check authorisation`

const polkitBaseDeclarationPlugs = `
  polkit:
    allow-installation: false
    deny-auto-connection: true
`

const polkitBaseDeclarationSlots = `
  polkit:
    allow-installation:
      slot-snap-type:
        - core
    deny-auto-connection: true
`

const polkitConnectedPlugAppArmor = `
# Description: Can talk to polkitd's CheckAuthorization API

#include <abstractions/dbus-strict>

dbus (send)
    bus=system
    path="/org/freedesktop/PolicyKit1/Authority"
    interface="org.freedesktop.PolicyKit1.Authority"
    member="{,Cancel}CheckAuthorization"
    peer=(label=unconfined),
dbus (send)
    bus=system
    path="/org/freedesktop/PolicyKit1/Authority"
    interface="org.freedesktop.PolicyKit1.Authority"
    member="RegisterAuthenticationAgentWithOptions"
    peer=(label=unconfined),
dbus (send)
    bus=system
    path="/org/freedesktop/PolicyKit1/Authority"
    interface="org.freedesktop.DBus.Properties"
    peer=(label=unconfined),
dbus (send)
    bus=system
    path="/org/freedesktop/PolicyKit1/Authority"
    interface="org.freedesktop.DBus.Introspectable"
    member="Introspect"
    peer=(label=unconfined),
`

type polkitInterface struct {
	commonInterface
}

func (iface *polkitInterface) getActionPrefix(attribs interfaces.Attrer) (string, error) {
	var prefix string
	if err := attribs.Attr("action-prefix", &prefix); err != nil {
		return "", err
	}
	if err := interfaces.ValidateDBusBusName(prefix); err != nil {
		return "", fmt.Errorf("plug has invalid action-prefix: %q", prefix)
	}
	return prefix, nil
}

func readPolkitPolicy(filename, actionPrefix string) (polkit.Policy, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf(`cannot read file %q: %v`, filename, err)
	}

	// Check that the file content is a valid polkit policy file
	actionIDs, err := validate.ValidatePolicy(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf(`cannot validate policy file %q: %v`, filename, err)
	}

	// Check that the action IDs in the policy file match the action prefix
	for _, id := range actionIDs {
		if id != actionPrefix && !strings.HasPrefix(id, actionPrefix+".") {
			return nil, fmt.Errorf(`policy file %q contains unexpected action ID %q`, filename, id)
		}
	}

	return polkit.Policy(content), nil
}

func (iface *polkitInterface) addPolkitPolicies(spec *polkit.Specification, plug *interfaces.ConnectedPlug) error {
	actionPrefix, err := iface.getActionPrefix(plug)
	if err != nil {
		return err
	}

	mountDir := plug.Snap().MountDir()
	policyFiles, err := filepath.Glob(filepath.Join(mountDir, "meta", "polkit", plug.Name()+".*.policy"))
	if err != nil {
		return err
	}
	if len(policyFiles) == 0 {
		return fmt.Errorf("cannot find any policy files for plug %q", plug.Name())
	}
	for _, filename := range policyFiles {
		suffix := strings.TrimSuffix(filepath.Base(filename), ".policy")
		policy, err := readPolkitPolicy(filename, actionPrefix)
		if err != nil {
			return err
		}
		if err := spec.AddPolicy(suffix, policy); err != nil {
			return err
		}
	}
	return nil
}

type polkitInstallRule struct {
	Name, Sha3_384 string
}

func (iface *polkitInterface) parseAndValidateInstallRules(attribs interfaces.Attrer) ([]polkitInstallRule, error) {
	var ruleEntries []map[string]string
	if err := attribs.Attr("install-rules", &ruleEntries); err != nil {
		return nil, err
	}
	if len(ruleEntries) == 0 {
		return nil, fmt.Errorf(`"install-rules" must have at least one entry`)
	}
	rules := make([]polkitInstallRule, len(ruleEntries))
	for i, ruleEntry := range ruleEntries {
		rule := polkitInstallRule{}
		for key, val := range ruleEntry {
			switch key {
			case "name":
				if err := validate.ValidateRuleFileName(val); err != nil {
					return nil, err
				}
				rule.Name = val
			case "sha3-384":
				rule.Sha3_384 = val
			default:
				return nil, fmt.Errorf(`unexpected key %q for "install-rules" entry`, key)
			}
		}
		if rule.Name == "" {
			return nil, fmt.Errorf(`key "name" is required for "install-rules" entry`)
		}
		if rule.Sha3_384 == "" {
			return nil, fmt.Errorf(`key "sha3-384" is required for "install-rules" entry`)
		}
		rules[i] = rule
	}
	return rules, nil
}

const maxPolkitRuleFileSize = 128 * 1024

func readPolkitRule(filename string, installRules []polkitInstallRule) (polkit.Rule, error) {
	// Find matching install-rules entry
	base := filepath.Base(filename)
	var exptectedHash string
	for _, installRule := range installRules {
		if installRule.Name == base {
			exptectedHash = installRule.Sha3_384
			break
		}
	}
	if exptectedHash == "" {
		return nil, fmt.Errorf(`no matching "install-rule" entry found for %q`, filename)
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Check rule file size
	finfo, err := f.Stat()
	if err != nil {
		return nil, err
	}
	fsize := finfo.Size()
	if fsize > maxPolkitRuleFileSize {
		return nil, fmt.Errorf(`%q is %d bytes, max rule file size is %d`, filename, fsize, maxPolkitRuleFileSize)
	}
	// Read rule and compute sha3-384 hash of matched file
	h := crypto.SHA3_384.New()
	content := &bytes.Buffer{}
	w := io.MultiWriter(content, h)
	if _, err := io.Copy(w, f); err != nil {
		return nil, fmt.Errorf("cannot obtain hash of %q: %w", filename, err)
	}
	hash, err := asserts.EncodeDigest(crypto.SHA3_384, h.Sum(nil))
	if err != nil {
		return nil, err
	}
	if hash != exptectedHash {
		return nil, fmt.Errorf("unexpected hash digest of %q, expected %q, found %q", filename, exptectedHash, hash)
	}
	// Hash matched, return rule content
	return polkit.Rule(content.Bytes()), nil
}

func (iface *polkitInterface) addPolkitRules(spec *polkit.Specification, plug *interfaces.ConnectedPlug) error {
	installRules, err := iface.parseAndValidateInstallRules(plug)
	if err != nil {
		return err
	}

	mountDir := plug.Snap().MountDir()
	ruleFiles, err := filepath.Glob(filepath.Join(mountDir, "meta", "polkit", plug.Name()+".*.rules"))
	if err != nil {
		return err
	}
	if len(ruleFiles) == 0 {
		return fmt.Errorf("cannot find any rule files for plug %q", plug.Name())
	}
	for _, filename := range ruleFiles {
		suffix := strings.TrimSuffix(filepath.Base(filename), ".rules")
		rule, err := readPolkitRule(filename, installRules)
		if err != nil {
			return err
		}
		if err := spec.AddRule(suffix, rule); err != nil {
			return err
		}
	}
	return err
}

type polkitMissingAttrErr struct {
	snapName string
}

func (err *polkitMissingAttrErr) Error() string {
	return fmt.Sprintf(`snap %q must have at least one of ("action-prefix", "install-rules") attributes set for interface "polkit"`, err.snapName)
}

func (iface *polkitInterface) PolkitConnectedPlug(spec *polkit.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	policyErr := iface.addPolkitPolicies(spec, plug)
	if policyErr != nil && !errors.Is(policyErr, snap.AttributeNotFoundError{}) {
		return policyErr
	}
	ruleErr := iface.addPolkitRules(spec, plug)
	if ruleErr != nil && !errors.Is(ruleErr, snap.AttributeNotFoundError{}) {
		return ruleErr
	}
	return nil
}

func (iface *polkitInterface) AppArmorConnectedPlug(spec *apparmor.Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	// Allow talking to polkitd's CheckAuthorization API only when "action-prefix" is set.
	_, err := iface.getActionPrefix(plug)
	if err == nil {
		spec.AddSnippet(polkitConnectedPlugAppArmor)
	}
	if errors.Is(err, snap.AttributeNotFoundError{}) {
		// "action-prefix" can be unset.
		return nil
	}
	return err
}

func (iface *polkitInterface) BeforePreparePlug(plug *snap.PlugInfo) error {
	// At least one of ("action-prefix", "install-rules") attributes must be set.
	_, policyErr := iface.getActionPrefix(plug)
	if policyErr != nil && !errors.Is(policyErr, snap.AttributeNotFoundError{}) {
		return policyErr
	}
	if policyErr == nil && !canWriteToDir(dirs.SnapPolkitPolicyDir) {
		return fmt.Errorf(`cannot use "action-prefix" attribute: %q is not writable`, dirs.SnapPolkitPolicyDir)
	}

	_, ruleErr := iface.parseAndValidateInstallRules(plug)
	if ruleErr != nil && !errors.Is(ruleErr, snap.AttributeNotFoundError{}) {
		return ruleErr
	}
	if ruleErr == nil && !canWriteToDir(dirs.SnapPolkitRuleDir) {
		return fmt.Errorf(`cannot use "install-rules" attribute: %q is not writable`, dirs.SnapPolkitRuleDir)
	}

	// Check if both attributes are not set.
	if policyErr != nil && ruleErr != nil {
		return &polkitMissingAttrErr{plug.Snap.InstanceName()}
	}
	return nil
}

var (
	// polkitDaemonPath1 is the path of polkitd on core<24.
	polkitDaemonPath1 = "/usr/libexec/polkitd"
	// polkitDaemonPath2 is the path of polkid on core>=24.
	polkitDaemonPath2 = "/usr/lib/polkit-1/polkitd"
)

// hasPolkitDaemonExecutableOnCore checks known paths on core for the presence of
// the polkit daemon executable. This function can be shortened but keep it like
// this for readability.
func hasPolkitDaemonExecutableOnCore() bool {
	return osutil.IsExecutable(polkitDaemonPath1) || osutil.IsExecutable(polkitDaemonPath2)
}

func canWriteToDir(dir string) bool {
	return unix.Access(dir, unix.W_OK) == nil
}

func (iface *polkitInterface) StaticInfo() interfaces.StaticInfo {
	info := iface.commonInterface.StaticInfo()
	// We must have the polkit daemon present on the system and be able to write
	// to either the polkit actions directory or the polkit rules directory.
	info.ImplicitOnCore = hasPolkitDaemonExecutableOnCore() && (canWriteToDir(dirs.SnapPolkitPolicyDir) || canWriteToDir(dirs.SnapPolkitRuleDir))
	return info
}

func init() {
	registerIface(&polkitInterface{
		commonInterface{
			name:    "polkit",
			summary: polkitSummary,
			// implicitOnCore is computed dynamically
			implicitOnClassic:    true,
			baseDeclarationPlugs: polkitBaseDeclarationPlugs,
			baseDeclarationSlots: polkitBaseDeclarationSlots,
		},
	})
}
