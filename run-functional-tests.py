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


def create_image(image, release='15.04', channel='edge'):
    """Creates the image to be used in the test

    """
    print("Creating image...")
    prepare_target_dir(os.path.dirname(image))
    return subprocess.check_output(
        'sudo ubuntu-device-flash'
        ' --verbose core {release}'
        ' -o {image}'
        ' --channel {channel}'
        ' --developer-mode'.format(
            release=release,
            image=image,
            channel=channel
        ), shell=True)


def build_debs(src_dir, debs_dir):
    print("Building debs...")
    prepare_target_dir(debs_dir)
    return subprocess.check_output([
        'bzr-buildpackage',
        '--result-dir={}'.format(debs_dir),
        src_dir,
        '--', '-uc', '-us',
    ])


def adt_run(src_dir, image_target, debs_dir, output_dir, debs_testbed_path):
    prepare_target_dir(output_dir)
    return subprocess.check_output([
        'adt-run',
        '-B',
        '--setup-commands',
        'touch /run/autopkgtest_no_reboot.stamp',
        '--setup-commands',
        'mount -o remount,rw /',
        '--setup-commands',
        "dpkg -i {debs_dir}/*deb".format(debs_dir=debs_testbed_path),
        '--setup-commands',
        'sync; sleep 2; mount -o remount,ro /',
        '--unbuilt-tree', src_dir,
        '--output-dir', output_dir,
        "--copy={orig_debs_dir}:{target_debs_dir}".format(
            orig_debs_dir=debs_dir,
            target_debs_dir=debs_testbed_path),
        '---',
        'ssh',
        '-s',
        '/usr/share/autopkgtest/ssh-setup/snappy',
        '--', '-i', image_target,
    ])


def compile_tests(src_dir):
    print("Compiling tests...")
    return subprocess.check_output([
        'go',
        'test',
        '-c',
        '-o snappy'
    ], cwd="{base}/debian/tests/".format(base=src_dir))


def main():
    build_debs(HERE, DEBS_DIR)

    create_image(IMAGE_TARGET)

    compile_tests(HERE)

    adt_run(HERE,
            IMAGE_TARGET,
            DEBS_DIR,
            OUTPUT_DIR,
            DEBS_TESTBED_PATH)

    return 0

if __name__ == '__main__':
    sys.exit(main())
