#!/usr/bin/python3

import glob
import os
import shutil
import subprocess
import sys


HERE = os.path.dirname(os.path.abspath(__file__))
BASE_DIR = '/tmp/snappy-test'
IMAGE_FILENAME = 'snappy.img'


def prepare_target_dir(target):
    target_dir = "{base_dir}/{target}".format(
        base_dir=BASE_DIR,
        target=target)
    if os.path.exists(target_dir):
        shutil.rmtree(target_dir)
    os.makedirs(target_dir)

    return target_dir


def prepare_image_dir():
    return prepare_target_dir('image')


def prepare_debs_dir():
    return prepare_target_dir('debs')


def prepare_output_dir():
    return prepare_target_dir('output')


def create_image(image, release='15.04', channel='edge'):
    """Creates the image to be used in the test

    """
    print("Creating image...")
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
    return subprocess.check_output([
        'bzr-buildpackage',
        '--result-dir={}'.format(debs_dir),
        src_dir,
        '--', '-uc', '-us',
    ])


def adt_run(src_dir, image_target, debs_dir, output_dir):
    debs_testbed_path = '/tmp/snappy-debs'
    return subprocess.check_output([
        'adt-run',
        '-B',
        '--setup-commands',
        'touch /run/autopkgtest_no_reboot.stamp',
        '--setup-commands',
        'mount -o remount,rw /',
        '--setup-commands',
        "dpkg -i {debs_dir}/*deb".format(debs_dir=debs_testbed_path),
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
    debs_dir = prepare_debs_dir()
    build_debs(HERE, debs_dir)

    image_dir = prepare_image_dir()
    image_target = "{dir}/{file}".format(dir=image_dir, file=IMAGE_FILENAME)
    create_image(image_target)

    compile_tests(HERE)

    output_dir = prepare_output_dir()
    adt_run(HERE, image_target, debs_dir, output_dir)

    return 0

if __name__ == '__main__':
    sys.exit(main())
