#!/usr/bin/python3

import sys
import yaml

with open(sys.argv[1]) as f:
    gadget_yaml = yaml.load(f)

i = 0
structure = gadget_yaml["volumes"]["pc"]["structure"]
while i < len(structure):
    entry = structure[i]
    if entry["name"] == "ubuntu-seed":
        edition = entry["update"]["edition"]
        entry["update"]["edition"] = edition+1
        break
    i += 1

with open(sys.argv[1], "w") as f:
    yaml.dump(seed, stream=f, indent=2, default_flow_style=False)

