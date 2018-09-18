#!/bin/bash

str_to_one_line(){
    echo "$1" | tr '\r\n' ' ' | tr -s ' '
}
