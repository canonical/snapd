#!/usr/bin/python3

import sys
import argparse

import yaml


def parse_arguments():
    parser = argparse.ArgumentParser(
        description='gadget yaml generator for tests')
    parser.add_argument('gadgetyaml', type=argparse.FileType('r'),
                        help='path to gadget.yaml input file')
    parser.add_argument('variant', help='test data variant',
                        choices=['v1', 'v2'])
    return parser.parse_args()


def must_find_struct(structs, structure_type):
    cans = [s for s in structs if s['type'] == structure_type]
    if len(cans) != 1:
        raise RuntimeError('unexpected number of structures: {}'.format(cans))
    return cans[0]


def make_v1(doc):
    # add new files to 'EFI System' partition, add new image file to 'BIOS
    # Boot', bump update edition for both
    structs = doc['volumes']['pc']['structure']
    efisystem = must_find_struct(structs,
                                 'EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B')
    biosboot = must_find_struct(structs,
                                'DA,21686148-6449-6E6F-744E-656564454649')

    # - name: EFI System
    #   (not)type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
    #   filesystem: vfat
    #   filesystem-label: system-boot
    #   size: 50M
    #   content:
    #     - source: grubx64.efi
    #       target: EFI/boot/grubx64.efi
    #     - source: shim.efi.signed
    #       target: EFI/boot/bootx64.efi
    #     - source: mmx64.efi
    #       target: EFI/boot/mmx64.efi
    #     - source: grub.cfg
    #       target: EFI/ubuntu/grub.cfg
    #     # drop a new file
    #     - source: foo.cfg
    #       target: foo.cfg
    #  update:
    #      edition: 1
    efisystem['content'].append({'source': "foo.cfg", 'target': 'foo.cfg'})
    efisystem['update'] = {'edition': 1}
    # - name: BIOS Boot
    #   (not)type: DA,21686148-6449-6E6F-744E-656564454649
    #   size: 1M
    #   offset: 1M
    #   offset-write: mbr+92
    #   content:
    #     - image: pc-core.img
    #     # write new content right after the previous one
    #     - image: foo.img
    #   update:
    #       edition: 1
    biosboot['content'].append({'image': 'foo.img'})
    biosboot['update'] = {'edition': 1}
    return doc


def make_v2(doc):
    # appply v1, add more new files to 'EFI System' partition, preserve one of
    # the updated files, to 'BIOS Boot', bump update edition for both

    doc = make_v1(doc)

    structs = doc['volumes']['pc']['structure']
    efisystem = must_find_struct(structs,
                                 'EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B')
    biosboot = must_find_struct(structs,
                                'DA,21686148-6449-6E6F-744E-656564454649')
    # - name: EFI System
    #   (not)type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
    #   filesystem: vfat
    #   filesystem-label: system-boot
    #   size: 50M
    #   content:
    #     - source: grubx64.efi
    #       target: EFI/boot/grubx64.efi
    #     - source: shim.efi.signed
    #       target: EFI/boot/bootx64.efi
    #     - source: mmx64.efi
    #       target: EFI/boot/mmx64.efi
    #     - source: grub.cfg
    #       target: EFI/ubuntu/grub.cfg
    #     # drop a new file
    #     - source: foo.cfg
    #       target: foo.cfg
    #     - source: bar.cfg
    #       target: bar.cfg
    #  update:
    #      edition: 2
    #      preserve: [foo.cfg, bar.cfg]
    efisystem['content'].append({'source': 'bar.cfg', 'target': 'bar.cfg'})
    efisystem['update'] = {
        'edition': 2,
        'preserve': ['foo.cfg', 'bar.cfg'],
    }
    # - name: BIOS Boot
    #   (not)type: DA,21686148-6449-6E6F-744E-656564454649
    #   size: 1M
    #   offset: 1M
    #   offset-write: mbr+92
    #   content:
    #     - image: pc-core.img
    #     # content is modified again
    #     - image: foo.img
    #   update:
    #       edition: 2
    biosboot['update'] = {'edition': 2}

    return doc


def main(opts):
    doc = yaml.safe_load(opts.gadgetyaml)

    if opts.variant == 'v1':
        make_v1(doc)
    elif opts.variant == 'v2':
        make_v2(doc)

    yaml.dump(doc, sys.stdout)


if __name__ == '__main__':
    opts = parse_arguments()
    main(opts)
