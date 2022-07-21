Steps for executing snapd's spread suite on a running ubuntu-core instance:

* Execute the console-conf setup on the device

* From the host, set up the `SPREAD_EXTERNAL_ADDRESS` environment variable with
the ip and port of the running instance:
```
$ export SPREAD_EXTERNAL_ADDRESS=<instance_ip>:<instance_port>
```
* From the snapd project's root execute the script to setup ssh access to the
instance:
```
$ ./tests/lib/external/prepare-ssh.sh <instance_ip> <instance_port> <launchpad_id>
```
The default values for ip and port are `localhost`, `8022`. This script assumes that
the user created by console-conf has the same name as the user executing the
script, if that's not the case you can pass the created username as a third argument
to the script.

* From the snapd project's root execute the suite selecting the type of system of
the instance (spread.yaml file lists all supported systems) by executing the command:
```
$ spread external:ubuntu-core-20-64
```
