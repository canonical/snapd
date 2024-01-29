#!/usr/bin/python3

from io import StringIO
import os
import unittest

import changelog

fake_news_md = """
# New in snapd 2.60.3:
* Fix bug in the "private" plug attribute of the shared-memory
  interface that can result in a crash when upgrading from an
  old version of snapd.
* Fix missing integration of the /etc/apparmor.d/tunables/home.d/
  apparmor to support non-standard home directories
"""

expected_changelog_entry = """
    - Fix bug in the "private" plug attribute of the shared-memory
      interface that can result in a crash when upgrading from an old
      version of snapd.
    - Fix missing integration of the /etc/apparmor.d/tunables/home.d/
      apparmor to support non-standard home directories
"""[1:]


class TestChangelogReadNewsMd(unittest.TestCase):
    def setUp(self):
        self.news_md = StringIO(fake_news_md)

    def test_happy(self):
        changelog_entry = changelog.read_changelogs_news_md(
            self.news_md, "2.60.3")
        self.assertEqual(changelog_entry, expected_changelog_entry)

    def test_version_not_found(self):
        with self.assertRaises(RuntimeError) as cm:
            changelog.read_changelogs_news_md(self.news_md, "1.1")
        self.assertEqual(str(cm.exception), 'cannot find expected version "1.1" in first header, found "New in snapd 2.60.3:"')

    def test_deb_email_happy(self):
        original = os.environ.get('DEBEMAIL')
        os.environ['DEBEMAIL'] = "FirstName LastName <firstname.lastname@canonical.com>"
        try:
            changelog.validate_env_deb_email()
        finally:
            if original:
                os.environ['DEBEMAIL'] = original

    def test_deb_email_errors(self):
        original = os.environ.get('DEBEMAIL')
        os.environ['DEBEMAIL'] = ""
        with self.assertRaises(RuntimeError) as e:
            changelog.validate_env_deb_email()
        self.assertEqual(str(e.exception), 'cannot find environment variable "DEBEMAIL", please provide DEBEMAIL="FirstName LastName <valid-email-address>"')
        os.environ['DEBEMAIL'] = "FirstName LastName <firstname.lastname.com>"
        with self.assertRaises(RuntimeError) as e:
            changelog.validate_env_deb_email()
        self.assertEqual(str(e.exception), 'environment variable "DEBEMAIL" uses incorrect format, expecting DEBEMAIL="FirstName LastName <valid-email-address>"')
        if original:
            os.environ['DEBEMAIL'] = original
