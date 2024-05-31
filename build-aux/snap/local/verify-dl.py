#!/usr/bin/env python3

# verify-dl verifies all ELF binaries with an interpreter have the
# correct path for its interpreter.

import os
import sys

from elftools.elf.elffile import ELFFile

errors = 0

for dirpath, dirnames, filenames in os.walk(sys.argv[1]):
    for filename in filenames:
        path = os.path.join(dirpath, filename)
        if os.path.islink(path):
            continue
        with open(path, 'rb') as f:
            if f.read(4) != b'\x7fELF':
                continue
            f.seek(0, 0)
            elf = ELFFile(f)
            for segment in elf.iter_segments():
                # TODO: use iter_segments(type='PT_INTERP')
                if segment['p_type'] == 'PT_INTERP' and segment.get_interp_name() != sys.argv[2]:
                    print('{}: Expected interpreter to be "{}", got "{}"'.format(path, sys.argv[2], segment.get_interp_name()), file=sys.stderr)
                    errors +=1

sys.exit(errors)
