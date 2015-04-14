package snappy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strings"
)

type apparmorJSONTemplate struct {
	Template      string   `json:"template"`
	PolicyGroups  []string `json:"policy_groups,omitempty"`
	PolicyVendor  string   `json:"policy_vendor"`
	PolicyVersion float64  `json:"policy_version"`
}

func generateApparmorJSONContent(s *SecurityDefinitions) ([]byte, error) {
	t := apparmorJSONTemplate{
		Template:      s.SecurityTemplate,
		PolicyGroups:  s.SecurityCaps,
		PolicyVendor:  "ubuntu-snappy",
		PolicyVersion: 1.3,
	}

	// FIXME: this is snappy specific, on other systems like the
	//        phone we may want different defaults.
	if t.Template == "" && len(t.PolicyGroups) == 0 {
		t.PolicyGroups = []string{"networking"}
	}

	if t.Template == "" {
		t.Template = "default"
	}

	outStr, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}

	return outStr, nil
}

func handleApparmor(buildDir string, m *packageYaml, hookName string, s *SecurityDefinitions) error {

	// ensure we have a hook
	if _, ok := m.Integration[hookName]; !ok {
		m.Integration[hookName] = clickAppHook{}
	}

	// legacy use of "Integration" - the user should
	// use the new format, nothing needs to be done
	_, hasApparmor := m.Integration[hookName]["apparmor"]
	_, hasApparmorProfile := m.Integration[hookName]["apparmor-profile"]
	if hasApparmor || hasApparmorProfile {
		return nil
	}

	// see if we have a custom security policy
	if s.SecurityPolicy != nil && s.SecurityPolicy.Apparmor != "" {
		m.Integration[hookName]["apparmor-profile"] = s.SecurityPolicy.Apparmor
		return nil
	}

	// see if we have a security override
	if s.SecurityOverride != nil && s.SecurityOverride.Apparmor != "" {
		m.Integration[hookName]["apparmor"] = s.SecurityOverride.Apparmor
		return nil
	}

	// generate apparmor template
	apparmorJSONFile := filepath.Join("meta", hookName+".apparmor")
	securityJSONContent, err := generateApparmorJSONContent(s)
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(buildDir, apparmorJSONFile), securityJSONContent, 0644); err != nil {
		return err
	}

	m.Integration[hookName]["apparmor"] = apparmorJSONFile

	return nil
}

func getSecurityProfile(m *packageYaml, appName string) string {
	cleanedName := strings.Replace(appName, "/", "-", -1)
	return fmt.Sprintf("%s_%s_%s", m.Name, cleanedName, m.Version)
}
