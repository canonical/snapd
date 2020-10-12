#!/usr/bin/python3

import sys
import argparse

import yaml


def parse_arguments():
    parser = argparse.ArgumentParser(description="pc gadget yaml generator for test")
    parser.add_argument(
        "gadgetyaml", type=argparse.FileType("r"), help="path to gadget.yaml input file"
    )
    return parser.parse_args()


def must_find_struct(structs, name):
    found = [s for s in structs if s["name"] == name]
    if len(found) != 1:
        raise RuntimeError("no structure with name {} among: {}".format(name, structs))
    return found[0]


def bump_update_edition(update):
    if update is None:
        return {"edition": 1}
    if "edition" not in update:
        update["edition"] = 1
    else:
        update["edition"] += 1
    return update


def main(opts):
    gadget_yaml = yaml.safe_load(opts.gadgetyaml)

    structs = gadget_yaml["volumes"]["pc"]["structure"]
    ubuntu_seed = must_find_struct(structs, "ubuntu-seed")
    ubuntu_seed["update"] = bump_update_edition(ubuntu_seed.get("update"))

    # trigger an update, so that device lookup is performed
    mbr = must_find_struct(structs, "mbr")
    mbr["update"] = bump_update_edition(mbr.get("update"))

    yaml.dump(gadget_yaml, stream=sys.stdout, indent=2, default_flow_style=False)


if __name__ == "__main__":
    opts = parse_arguments()
    main(opts)
