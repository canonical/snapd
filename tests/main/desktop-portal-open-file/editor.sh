#!/bin/sh
filename="$1"
echo "Editing $filename"

echo "$filename" >> /tmp/editor-history.txt
