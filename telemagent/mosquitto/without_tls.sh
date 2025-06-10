#!/bin/bash

topic="test/topic"
message="Hello mProxy"
host=localhost
port=1884

echo "Subscribing to topic ${topic} without TLS..."
mosquitto_sub -V 5 -h $host -p $port -t $topic &
sub_pid=$!
sleep 1

cleanup() {
    echo "Cleaning up..."
    kill $sub_pid
}

# Trap the EXIT and ERR signals and call the cleanup function
trap cleanup EXIT

echo "Publishing to topic ${topic} without TLS..."
mosquitto_pub -V 5 -h $host -p $port -t $topic -m "${message}" -q 1
sleep 1