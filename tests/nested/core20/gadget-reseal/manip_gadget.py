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
    matched = [s for s in structs if s["name"] == "ubuntu-seed"]
    if not matched:
        raise RuntimeError("ubuntu-seed not found among: {}".format(structs))

    ubuntu_seed = matched[0]
    ubuntu_seed["update"] = bump_update_edition(ubuntu_seed.get("update"))

    yaml.dump(gadget_yaml, stream=sys.stdout, indent=2, default_flow_style=False)


if __name__ == "__main__":
    opts = parse_arguments()
    main(opts)
