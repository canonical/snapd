#!/usr/bin/env python3

import argparse
import logging
import yaml
import sys


def parse_arguments():
    parser = argparse.ArgumentParser(description="ensure pc gadget has ubuntu-save")
    parser.add_argument(
        "gadgetyaml", type=argparse.FileType("r"), help="path to gadget.yaml input file"
    )
    return parser.parse_args()


def main(opts):
    gadget_yaml = yaml.safe_load(opts.gadgetyaml)

    structs = gadget_yaml["volumes"]["pc"]["structure"]
    save_idx = -1
    for idx, s in enumerate(structs):
        role = s.get("role", "")
        if role == "system-save":
            logging.info("system-save structure already present")
            # already has ubuntu-save
            return
        if role == "system-data":
            # ubuntu-save precedes ubuntu-data
            save_idx = idx
            break
    if save_idx == -1:
        raise RuntimeError("cannot find a suitable place to insert ubuntu-save")

    ubuntu_save = {
        "name": "ubuntu-save",
        "role": "system-save",
        # TODO:UC20: update when pc-amd64-gadget changes
        "size": "16M",
        "filesystem": "ext4",
        "type": "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
    }
    structs.insert(save_idx, ubuntu_save)
    yaml.dump(gadget_yaml, stream=sys.stdout, indent=2, default_flow_style=False)


if __name__ == "__main__":
    logging.basicConfig(level=logging.DEBUG)
    opts = parse_arguments()
    main(opts)
