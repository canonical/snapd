package snappy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
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

	if t.Template == "" {
		t.Template = "default"
	}

	if t.Template == "default" && len(t.PolicyGroups) == 0 {
		t.PolicyGroups = []string{"networking"}
	}

	outStr, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}

	return outStr, nil
}

func handleApparmor(buildDir string, m *packageYaml, hookName string, s *SecurityDefinitions) error {

	// legacy use of "Integration" - the user should
	// use the new format, nothing needs to be done
	_, hasApparmor := m.Integration[hookName]["apparmor"]
	_, hasApparmorProfile := m.Integration[hookName]["apparmor-profile"]
	if hasApparmor || hasApparmorProfile {
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

func getAaProfile(m *packageYaml, name string, s *SecurityDefinitions) string {
	// check if there is a specific apparmor profile
	if s.SecurityPolicy != nil && s.SecurityPolicy.Apparmor != "" {
		return s.SecurityPolicy.Apparmor
	}
	// ... or apparmor.json
	if s.SecurityTemplate != "" {
		return s.SecurityTemplate
	}

	// FIXME: we need to generate a default aa profile here instead
	// of relying on a default one shipped by the package
	return fmt.Sprintf("%s_%s_%s", m.Name, filepath.Base(name), m.Version)
}
