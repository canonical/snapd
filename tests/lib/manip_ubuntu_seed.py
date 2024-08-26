#!/usr/bin/env python3

import os
import sys
import yaml


if len(sys.argv) < 2:
    print('2 arguments are required: gadget.yaml and size')
    sys.exit(1)

gadget = sys.argv[1]
size = sys.argv[2]

with open(gadget, 'r') as f:
    data = yaml.safe_load(f)

for entry in data['volumes']['pc']['structure']:
    if entry.get('role') == 'system-seed':
        entry['size'] = size

with open(sys.argv[1], 'w') as f:
    yaml.dump(data, f)
