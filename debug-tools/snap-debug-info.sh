#!/bin/bash

# some commands need either root or sudo permissions, so check for that early
if [ "$(id -u)" != 0 ]; then
    if ! sudo echo "authentication as root successful"; then
        echo "this script needs to be run as root or use sudo permission"
        exit 1
    fi
fi

h1(){ echo -e "\n==================== $* ===================="; }
h2(){ echo -e "\n========== $* =========="; }
h3(){ echo -e "\n===== $* ====="; }

h1 "SNAP VERSION"; snap version
h1 "SNAP WHOAMI"; snap whoami
h1 "SNAP MODEL"; snap model --verbose
h1 "SNAP MODEL SERIAL"; snap model --serial --verbose
h1 "SNAP LIST"; snap list --all
h1 "SNAP SERVICES"; snap services
h1 "SNAP CONNECTIONS"; snap connections

h1 "PER-SNAP CONNECTIONS"
for sn in $(snap list | awk 'NR>1 {print $1}'); do
    h2 "PER-SNAP $sn CONNECTIONS"
    snap connections "$sn"
done
h1 "SNAP CHANGES"
snap changes --abs-time

GADGET_SNAP="$(snap list | awk '($6 ~ /.*gadget.*$/) {print $1}')"
if [ -z "$GADGET_SNAP" ]; then
    # could be a serious bug/problem or otherwise could be just on a classic
    # device
    h1 "NO GADGET SNAP DETECTED"
else
    h1 "GADGET SNAP GADGET.YAML"
    cat /snap/"$(snap list | awk '($6 ~ /.*gadget.*$/) {print $1}')"/current/meta/gadget.yaml
fi

h1 "SNAP CHANGES (in Doing)"
# print off the output of snap tasks <chg> for every chg that is in Doing state
for chg in $(snap changes | tail -n +2 | grep -Po '(?:[0-9]+\s+Doing)' | awk '{print $1}'); do
    h3 "tasks for $chg"
    snap tasks "$chg" --abs-time
done

h1 "SNAP CHANGES (in Error)"
# same as above, just for Error instead of Doing
for chg in $(snap changes | tail -n +2 | grep -Po '(?:[0-9]+\s+Error)' | awk '{print $1}'); do
    h3 "tasks for $chg"
    snap tasks "$chg" --abs-time
done

h1 "VALIDATION SET ASSERTIONS"
snap known validation-set

# sudo needed for these commands
h1 "VALIDATION SETS"; sudo snap validate
h1 "OFFLINE SNAP CHANGES"; sudo snap debug state --abs-time --changes /var/lib/snapd/state.json
h1 "SNAPD STACKTRACE"; sudo snap debug stacktraces
h1 "SNAP SYSTEM CONFIG"; sudo snap get system -d
h1 "SNAPD JOURNAL"; sudo journalctl --no-pager -u snapd
h1 "SNAPD.SERVICE STATUS"; sudo systemctl --no-pager status snapd
h1 "UPTIME"; uptime
h1 "DATE (IN UTC)"; date --utc
h1 "DISK SPACE"; df -h
h1 "DENIED MESSAGES"; sudo journalctl --no-pager | grep DENIED
