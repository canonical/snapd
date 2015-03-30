package snappy

import (
	"encoding/json"
)

type apparmorJSONTemplate struct {
	Template      string   `json:"template"`
	PolicyGroups  []string `json:"policy_groups"`
	PolicyVendor  string   `json:"policy_vendor"`
	PolicyVersion float64  `json:"policy_version"`
}

func generateApparmorJSONContent(binary Binary) ([]byte, error) {
	t := apparmorJSONTemplate{
		Template:      "default",
		PolicyGroups:  binary.SecurityCaps,
		PolicyVendor:  "ubuntu-snappy",
		PolicyVersion: 1.3,
	}

	if len(t.PolicyGroups) == 0 {
		t.PolicyGroups = []string{"default"}
	}

	outStr, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return nil, err
	}

	return outStr, nil
}
