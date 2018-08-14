#!/bin/bash

set -e

echo "OK Pleased to meet you"
while true; do
    read -r line
    case $line in
        GETPIN)
            echo "D pass"
            echo "OK"
            ;;
        BYE)
            exit 0
            ;;
        *)
            echo "OK I'm not very smart"
            ;;
    esac
done
