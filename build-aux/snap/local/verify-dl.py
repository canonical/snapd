#!/usr/bin/env python3

# verify-dl verifies all ELF binaries with an interpreter have the
# correct path for its interpreter.

import os
import sys
import logging
import argparse

from elftools.elf.elffile import ELFFile


def parse_arguments() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="verify requested interpreter of ELF binaries"
    )
    craft_prime = os.environ.get("CRAFT_PRIME")
    parser.add_argument(
        "--prime",
        default=craft_prime,
        required=(craft_prime is None),
        help="snapcraft part priming location (or rootdir of a snap)",
    )
    parser.add_argument("interp", help="Expected interpreter")

    return parser.parse_args()


def main(opts) -> None:
    errors: list[str] = []

    for dirpath, _, filenames in os.walk(opts.prime):
        for filename in filenames:
            path = os.path.join(dirpath, filename)
            if os.path.islink(path):
                continue
            with open(path, "rb") as f:
                if f.read(4) != b"\x7fELF":
                    continue
                f.seek(0, 0)

                logging.debug("checking ELF binary: %s", path)

                elf = ELFFile(f)
                for segment in elf.iter_segments():
                    # TODO: use iter_segments(type='PT_INTERP')
                    if (
                        segment["p_type"] == "PT_INTERP"
                        and segment.get_interp_name() != opts.interp
                    ):
                        logging.error(
                            '%s: expected interpreter to be "%s", got "%s"',
                            path,
                            sys.argv[2],
                            segment.get_interp_name(),
                        )
                        errors.append(path)

    if errors:
        badlist = "\n".join(["- " + n for n in errors])
        raise RuntimeError(f"found binaries with incorrect ELF interpreter:\n{badlist}")


if __name__ == "__main__":
    logging.basicConfig(level=logging.DEBUG)
    main(parse_arguments())
