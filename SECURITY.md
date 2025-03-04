# Security policy

## Supported versions
<!-- Include start supported versions -->
snapd has two types of releases:

- Major releases: Introduce partial/complete features along with bug fixes.
- Minor releases: Fix bugs or security vulnerabilities.

A snapd release typically involves publishing snapd snaps to the Snap Store and
snapd debs to supported Ubuntu releases. Minor releases containing security
fixes are developed in a private snapd repository, with fixes merged back into
the public repository once they are ready for disclosure.

The latest snapd snap major release receives support through minor releases
until the next major release. Similarly, snapd debs for Ubuntu versions within
standard support receive minor releases up to the next major release. For
Ubuntu versions outside standard support, snapd debs continue receiving minor
releases typically based on the last major release before the end of standard
support.

<!-- Include end supported versions -->

## What qualifies as a security issue

By default, snaps are confined within a restrictive security sandbox,
preventing access to system resources beyond what is explicitly allowed by
store-approved interfaces. Any vulnerability that enables a snap to operate
outside of these intended restrictions qualifies as a security issue.

## Reporting a vulnerability

The easiest way to report a security issue is through
[GitHub](https://github.com/canonical/snapd/security/advisories/new). See
[Privately reporting a security
vulnerability](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing/privately-reporting-a-security-vulnerability)
for instructions.

The snapd GitHub admins will be notified of the issue and will work with you
to determine whether the issue qualifies as a security issue and, if so, in
which component. We will then handle figuring out a fix, getting a CVE
assigned and coordinating the release of the fix to the snapd snap and the
various Ubuntu releases and Linux distributions.

The [Ubuntu Security disclosure and embargo
policy](https://ubuntu.com/security/disclosure-policy) contains more
information about what you can expect when you contact us, and what we
expect from you.
