#!/usr/bin/python3

import sys
from elftools.elf.elffile import ELFFile

with open(sys.argv[1], 'rb') as f:
    if f.read(4) != b'\x7fELF':
        sys.exit(1)
    f.seek(0, 0)
    elf = ELFFile(f)
    for segment in elf.iter_segments(type='PT_INTERP'):
        sys.exit(0)
sys.exit(1)
