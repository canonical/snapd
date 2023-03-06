#!/usr/bin/env python3

import os
import sys
import time

vm_name = "build-snapd"
# Memory in GB
vm_memory = 16
# number of cores assigned to the VM
vm_cpu_cores = 16
# disk space for the VM in GB
vm_disk_space = 10

def run_command(command):
    print(f"Running {command}")
    os.system(f"multipass exec {vm_name} -- {command}")

def create_vm():
    print("Creating VM")
    # first, delete any old VM
    os.system(f"multipass delete {vm_name}")
    os.system(f"multipass purge")
    time.sleep(2)
    os.system(f"multipass launch xenial -n {vm_name} -m {vm_memory}G -c {vm_cpu_cores} -d {vm_disk_space}G")

if (len(sys.argv) > 1):
    if (sys.argv[1] != "init"):
        print("Unknown command. Use:")
        print("    build-snapd [init]")
        sys.exit(-1)
    create_vm()
    run_command("sudo apt update")
    run_command("sudo apt dist-upgrade -y")
    run_command("sudo snap install lxd")
    run_command("sudo lxd.migrate -yes")
    run_command("lxd init --auto")
    run_command("sudo snap install snapcraft --classic --channel=4.x")
    run_command('bash -c "echo export SNAPCRAFT_BUILD_ENVIRONMENT=lxd >> /home/ubuntu/.bashrc"')
    run_command("mkdir -p /home/ubuntu/basedir")
    os.system(f"multipass stop {vm_name}")
    os.system(f"multipass mount -t native . {vm_name}:/home/ubuntu/basedir/")
    os.system(f"multipass start {vm_name}")

run_command('bash')
