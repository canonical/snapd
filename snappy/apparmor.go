package snappy

import (
	"encoding/json"
)

type apparmorJSONTemplate struct {
	Template      string   `json:"template"`
	PolicyGroups  []string `json:"policy_groups,omitempty"`
	PolicyVendor  string   `json:"policy_vendor"`
	PolicyVersion float64  `json:"policy_version"`
}

func generateApparmorJSONContent(binary Binary) ([]byte, error) {
	t := apparmorJSONTemplate{
		Template:      binary.SecurityTemplate,
		PolicyGroups:  binary.SecurityCaps,
		PolicyVendor:  "ubuntu-snappy",
		PolicyVersion: 1.3,
	}

	if t.Template == "" {
		t.Template = "default"
	}

	if t.Template == "default" && len(t.PolicyGroups) == 0 {
		t.PolicyGroups = []string{"network"}
	}

	outStr, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}

	return outStr, nil
}
