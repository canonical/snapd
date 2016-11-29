#!/bin/sh
echo "setup fake gpg pinentry environment"
cat > /tmp/pinentry-fake <<'EOF'
#!/bin/sh
set -e
echo "OK Pleased to meet you"
while true; do
  read line
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
EOF
chmod +x /tmp/pinentry-fake
mkdir -pm 0700 $HOME/.snap/gnupg/
echo pinentry-program /tmp/pinentry-fake > $HOME/.snap/gnupg/gpg-agent.conf
