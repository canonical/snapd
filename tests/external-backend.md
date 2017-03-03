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
$ ./tests/lib/external/prepare-ssh.sh <instance_ip> <instance_port>
```
The default values for ip and port are `localhost`, `8022`. This script assumes that
the user created by console-conf has the same name as the user executing the
script, if that's not the case you can pass the created username as a third argument
to the script.

* From the snapd project's root execute the suite selecting the type of system of
the instance, currently `ubuntu-core-16-64`, `ubuntu-core-16-32`, `ubuntu-core-16-arm-32` and `ubuntu-core-16-arm-64` are supported:
```
$ spread -v -reuse external:ubuntu-core-16-64
```
* You can execute again the suite by just reissuing the spread command, no need
to run the prepare script again.
