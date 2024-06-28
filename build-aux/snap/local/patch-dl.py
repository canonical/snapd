#!/usr/bin/env python3

# patch-dl calls patchelf on ELF binaries with an interpreter to update
# the path of that interpreter.

import argparse
import os
import shutil
import subprocess
import sys
import tempfile

from elftools.elf.elffile import ELFFile


parser = argparse.ArgumentParser()

craft_prime = os.environ.get('CRAFT_PRIME')
parser.add_argument('--prime', nargs=1, default=craft_prime, required=(craft_prime is None))

craft_part_install = os.environ.get('CRAFT_PART_INSTALL')
parser.add_argument('--install', nargs=1, default=craft_part_install, required=(craft_part_install is None))

parser.add_argument('linker')

args = parser.parse_args()

def is_shared_exec(path):
    with open(path, 'rb') as f:
        if f.read(4) != b'\x7fELF':
            return False
        f.seek(0, 0)
        elf = ELFFile(f)
        for segment in elf.iter_segments():
            # TODO: use iter_segments(type='PT_INTERP')
            if segment['p_type'] == 'PT_INTERP':
                return True
        return False

owned_executables = []

for dirpath, dirnames, filenames in os.walk(args.install):
    for filename in filenames:
        path = os.path.join(dirpath, filename)
        if os.path.islink(path):
            continue
        if not is_shared_exec(path):
            continue
        rel = os.path.relpath(path, args.install)
        # Now we need to know if the file in $CRAFT_PRIME is actually
        # owned by the current part and see if it is hard-linked to a
        # corresponding file in $CRAFT_PART_INSTALL.
        #
        # Even if we break the hard-links before, subsequent builds will
        # re-introduce the hard-links in `crafctl default` call in the
        # `override-prime`.
        prime_path = os.path.join(args.prime, rel)
        install_st = os.lstat(path)
        prime_st = os.lstat(path)
        if install_st.st_dev != prime_st.st_dev:
            continue
        if install_st.st_ino != prime_st.st_ino:
            continue
        owned_executables.append(prime_path)

for path in owned_executables:
    # Because files in $CRAFT_PRIME, $CRAFT_STAGE, and $CRAFT_PART_INSTALL are hard-linked,
    # we need to copy the file first to avoid writing back to the content of other directories.
    with tempfile.NamedTemporaryFile(dir=os.path.dirname(path),
                                     prefix='{}-'.format(os.path.basename(path))) as f:
        with open(path, 'rb') as orig:
            shutil.copyfileobj(orig, f)
        f.flush()
        print(f'Running patchelf for "{f.name}"', file=sys.stderr)
        subprocess.run(['patchelf', '--set-interpreter', args.linker, f.name], check=True)
        shutil.copystat(path, f.name)
        os.unlink(path)
        os.link(f.name, path)
