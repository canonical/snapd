#!/bin/bash -ex
  if [ "$TRUST_TEST_KEYS" = "false" ]; then
      echo "This test needs test keys to be trusted"
      exit
  fi

  echo "Waiting for the system to be seeded"
  remote.exec "sudo snap wait system seed.loaded"

  echo "secure boot is enabled on the nested vm"
  remote.exec "xxd /sys/firmware/efi/efivars/SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c" |
      MATCH "00000000: 0600 0000 01\s+....."

  echo "Check we have the right model from snap model"
  remote.exec "sudo snap model --verbose" | MATCH "grade:\s+${MODEL_GRADE}"

  cmdlineExtraDang="extradang.val=1 extradang.flag"
  remoteCmd="sudo snap set system system.boot.dangerous-cmdline-extra=\"$cmdlineExtraDang\""
  if [ "$MODEL_GRADE" = "dangerous" ]; then
      if ! remote.exec "$remoteCmd"; then
          echo "Failure setting dangerous-cmdline-extra"
          exit 1
      fi
      boot_id="$(tests.nested boot-id)"
      echo "Rebooting"
      remote.exec "sudo reboot" || true
      tests.nested wait-for reboot "$boot_id"

      remote.exec "sudo cat /proc/cmdline" | MATCH "$cmdlineExtraDang"
  else
      if remote.exec "$remoteCmd"; then
          echo "Unexpected success setting dangerous-cmdline-extra for model $MODEL_GRADE"
          exit 1
      fi
      echo "Failure setting dangerous-cmdline-extra (as expected)"
  fi

  cmdlineExtra="extra.val=1 extra.flag"
  if [ "$MODEL_GRADE" = "dangerous" ]; then
      remote.exec "sudo snap set system system.boot.dangerous-cmdline-extra="
  fi
  remote.exec "sudo snap set system system.boot.cmdline-extra=\"$cmdlineExtra\""

  boot_id="$(tests.nested boot-id)"
  echo "Rebooting"
  tests.nested "sudo reboot" || true
  tests.nested wait-for reboot "$boot_id"

  remote.exec "sudo cat /proc/cmdline" | MATCH "$cmdlineExtra"
