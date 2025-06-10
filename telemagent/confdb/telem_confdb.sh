# snapcraft create-key "telem-key"
snap sign -k "vm-key" telem-agent-confdb.json > telem-agent-confdb.assert
snap ack telem-agent-confdb.assert
