#!/bin/bash

snap_install() {
    local PARAMS=$@
    local CONDITIONS='timeout \| connection refused'
    local RETRIES=2
    local SLEEP=2

    local ITER=0
    until [ $ITER -gt $RETRIES ]; do
        ERROR=$(snap install $PARAMS 2>&1> /dev/null)
        if [ -z "$ERROR" ]; then 
            break
        elif [[ $( echo $ERROR | grep -c "$CONDITIONS") -ge 1 ]]; then
            ITER=$[$ITER+1]
            sleep $SLEEP
            echo $ERROR >&2
            if [ $ITER -le $RETRIES ]; then
                echo "Retrying install $ITER" >&2
            fi
        else
            echo $ERROR >&2
            break
        fi
    done
}
