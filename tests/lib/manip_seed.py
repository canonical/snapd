import sys
import yaml
from os import path

with open(sys.argv[1]) as f:
    seed = yaml.load(f)

i = 0
snaps = seed["snaps"]
while i < len(snaps):
    entry = snaps[i]
    if entry["name"] == "pc":
        snaps[i] = {
            "name": "pc",
            "unasserted": True,
            "file": "pc_x1.snap",
        }
        break
    i += 1

for snap in sys.argv[2:]:
    # test-snapd-with-configure_123.snap -> test-snapd-configure
    snapname = path.basename(snap).split("_")[0]
    snaps.append(
        {"name": snapname, "channel": "edge", "file": snap,}
    )

with open(sys.argv[1], "w") as f:
    yaml.dump(seed, stream=f, indent=2, default_flow_style=False)
