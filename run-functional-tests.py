#!/usr/bin/env python3

import os
import shutil
import subprocess
import sys


HERE = os.path.dirname(os.path.abspath(__file__))
BASE_DIR = '/tmp/snappy-test'
IMAGE_DIR = "{base}/image".format(base=BASE_DIR)
DEBS_DIR = "{base}/debs".format(base=BASE_DIR)
DEBS_TESTBED_PATH = '/tmp/snappy-debs'
OUTPUT_DIR = "{base}/output".format(base=BASE_DIR)
IMAGE_TARGET = "{dir}/snappy.img".format(dir=IMAGE_DIR)


def prepare_target_dir(target_dir):
    if os.path.exists(target_dir):
        shutil.rmtree(target_dir)
    os.makedirs(target_dir)

    return target_dir


def create_image(release='15.04', channel='edge'):
    """Creates the image to be used in the test

    """
    print("Creating image...")
    prepare_target_dir(IMAGE_DIR)
    return subprocess.check_output(
        'sudo ubuntu-device-flash'
        ' --verbose core {release}'
        ' -o {image}'
        ' --channel {channel}'
        ' --developer-mode'.format(
            release=release,
            image=IMAGE_TARGET,
            channel=channel
        ), shell=True)


def build_debs():
    print("Building debs...")
    prepare_target_dir(DEBS_DIR)
    return subprocess.check_output([
        'bzr', 'bd',
        '--result-dir={}'.format(DEBS_DIR),
        HERE,
        '--', '-uc', '-us',
    ])


def adt_run():
    prepare_target_dir(OUTPUT_DIR)
    return subprocess.check_output([
        'adt-run',
        '-B',
        '--setup-commands',
        'touch /run/autopkgtest_no_reboot.stamp',
        '--setup-commands',
        'mount -o remount,rw /',
        '--setup-commands',
        "dpkg -i {debs_dir}/*deb".format(debs_dir=DEBS_TESTBED_PATH),
        '--setup-commands',
        'sync; sleep 2; mount -o remount,ro /',
        '--override-control', 'debian/integration-tests/control',
        '--built-tree', HERE,
        '--output-dir', OUTPUT_DIR,
        "--copy={orig_debs_dir}:{target_debs_dir}".format(
            orig_debs_dir=DEBS_DIR,
            target_debs_dir=DEBS_TESTBED_PATH),
        '---',
        'ssh',
        '-s',
        '/usr/share/autopkgtest/ssh-setup/snappy',
        '--', '-i', IMAGE_TARGET,
    ])

def main():
    build_debs()

    create_image()

    adt_run()

    return 0

if __name__ == '__main__':
    sys.exit(main())
