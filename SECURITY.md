# Security policy

## Supported versions
<!-- Include start supported versions -->
Snapd has two types of releases:

- Major releases, that introduce complete or partial features along with bug
  fixes
- Minor releases, as required to fix one or more bugs or security
  vulnerabilities

We do a single release at a time, and only support the latest release with
follow-up minor releases for bugfixes or security fixes up to the next major
release.

A Snapd release typically includes releasing Snapd snaps to the snap store as
well as Snapd debs to supported Ubuntu releases. Minor releases for the purpose
of security fixes are done in a private Snapd repository and the fixes merged
back into public repository when it is ready to be disclosed.

<!-- Include end supported versions -->

## What qualifies as a security issue

Without custom flags at installation, snaps are confined to a restrictive
security sandbox with no access to system resources outside of the snap other
than whats allowed by store approved interfaces. Any vulnerability that allows
snaps to operate outside of the intended restrictions qualifies as a security
issue.

## Reporting a vulnerability

The easiest way to report a security issue is through
[GitHub](https://github.com/canonical/snapd/security/advisories/new). See
[Privately reporting a security
vulnerability](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing/privately-reporting-a-security-vulnerability)
for instructions.

Alternatively, please email [security@ubuntu.com](mailto:security@ubuntu.com) with a description of the issue, the
steps you took to create the issue, affected versions, and, if known, mitigations for the issue.

The Snapd GitHub admins will be notified of the issue and will work with you
to determine whether the issue qualifies as a security issue and, if so, in
which component. We will then handle figuring out a fix, getting a CVE
assigned and coordinating the release of the fix to the Snapd snap and the
various Ubuntu releases and Linux distributions.

The [Ubuntu Security disclosure and embargo
policy](https://ubuntu.com/security/disclosure-policy) contains more
information about what you can expect when you contact us, and what we
expect from you.
