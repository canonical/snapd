#!/usr/bin/python3

import sys
import argparse

import yaml


def parse_arguments():
    parser = argparse.ArgumentParser(
        description="pc gadget yaml variant generator for test"
    )
    parser.add_argument(
        "--system-seed", action="store_true", help="also modify system-seed"
    )
    parser.add_argument(
        "--system-bios", action="store_true", help="also modify system-bios"
    )
    parser.add_argument(
        "gadgetyaml", type=argparse.FileType("r"), help="path to gadget.yaml input file"
    )
    parser.add_argument("variant", help="test data variant", choices=["v1", "v2"])
    return parser.parse_args()


def match_name(name):
    return lambda struct: struct.get("name", "") == name


def match_role_with_fallback(role):
    def match(struct):
        if "role" in struct:
            struct_role = struct["role"]
        elif "filesystem-label" in struct:
            # fallback to filesystem-label
            struct_role = struct["filesystem-label"]
        else:
            return False
        return role == struct_role

    return match


def must_find_struct(structs, matcher):
    found = [s for s in structs if matcher(s)]
    if len(found) != 1:
        raise RuntimeError("found {} matches among: {}".format(len(found), structs))
    return found[0]


def may_find_struct(structs, matcher):
    found = [s for s in structs if matcher(s)]
    if len(found) != 1:
        return None
    return found[0]


def bump_update_edition(update):
    if update is None:
        return {"edition": 1}
    if "edition" not in update:
        update["edition"] = 1
    else:
        update["edition"] += 1
    return update


def make_v1(doc, system_seed):
    # add new files to 'EFI System' partition, add new image file to 'BIOS
    # Boot', bump update edition for both
    structs = doc["volumes"]["pc"]["structure"]
    # "EFI System" in UC16/UC18, or just system-boot in UC20
    efisystem = must_find_struct(structs, match_role_with_fallback("system-boot"))
    biosboot = may_find_struct(structs, match_name("BIOS Boot"))

    # from UC16/UC18 gadgets:
    #
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
    efisystem["content"].append({"source": "foo.cfg", "target": "foo.cfg"})
    efisystem["update"] = bump_update_edition(efisystem.get("update"))

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
    if biosboot and "content" in biosboot:
        biosboot["content"].append({"image": "foo.img"})
        biosboot["update"] = bump_update_edition(biosboot.get("update"))

    if system_seed:
        # from UC20 gadget:
        #
        # - name: ubuntu-seed
        #   role: system-seed
        #   filesystem: vfat
        #   # UEFI will boot the ESP partition by default first
        #   (not)type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        #   size: 1200M
        #   update:
        #     edition: 2
        #   content:
        #     - source: grubx64.efi
        #       target: EFI/boot/grubx64.efi
        #     - source: shim.efi.signed
        #       target: EFI/boot/bootx64.efi
        #     - source: grub-recovery.conf
        #       target: EFI/ubuntu/grub.cfg
        systemseed = must_find_struct(structs, match_role_with_fallback("system-seed"))
        systemseed["content"].append(
            {"source": "foo-seed.cfg", "target": "foo-seed.cfg"}
        )
        systemseed["update"] = bump_update_edition(systemseed.get("update"))

    return doc


def make_v2(doc, system_seed, system_bios):
    # appply v1, add more new files to 'EFI System' partition, preserve one of
    # the updated files, to 'BIOS Boot', bump update edition for both

    doc = make_v1(doc, system_seed)

    structs = doc["volumes"]["pc"]["structure"]
    efisystem = must_find_struct(structs, match_role_with_fallback("system-boot"))
    if system_bios:
        biosboot = must_find_struct(structs, match_name("BIOS Boot"))
    else:
        biosboot = may_find_struct(structs, match_name("BIOS Boot"))

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
    efisystem["content"].append({"source": "bar.cfg", "target": "bar.cfg"})
    efisystem["update"] = bump_update_edition(efisystem.get("update"))
    efisystem["update"]["preserve"] = efisystem["update"].get("preserve", []) + [
        "foo.cfg",
        "bar.cfg",
    ]
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
    if system_bios: 
        biosboot["update"] = bump_update_edition(biosboot.get("update"))

    if system_seed:
        # only in UC20 gadgets
        systemseed = must_find_struct(structs, match_role_with_fallback("system-seed"))
        # we already appended foo-boot.cfg, bump the edition so that it gets
        # updated
        systemseed["update"] = bump_update_edition(systemseed.get("update"))

    return doc


def main(opts):
    doc = yaml.safe_load(opts.gadgetyaml)

    if opts.variant == "v1":
        make_v1(doc, opts.system_seed)
    elif opts.variant == "v2":
        make_v2(doc, opts.system_seed, opts.system_bios)

    yaml.dump(doc, sys.stdout)


if __name__ == "__main__":
    opts = parse_arguments()
    main(opts)
