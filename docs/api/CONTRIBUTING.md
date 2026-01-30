<!--
    SPDX-FileCopyrightText: 2025 Canonical Ltd
    SPDX-License-Identifier: GPL-3.0-only
-->

# Contributing to the Snapd REST OpenAPI Documentation

First off, thank you for considering contributing to this project!
We're an open-source project and welcome community contributions.
Every little bit helps, and we appreciate your effort.

This document provides a set of guidelines for contributing to our OpenAPI specification.

## Code of Conduct

This project and everyone participating in it is governed by our [Code of Conduct](CODE_OF_CONDUCT.md).
By participating, you are expected to uphold this code. Please report unacceptable behavior.

---

## Getting Started

To contribute, you'll first need to sign the **Canonical Contributor Agreement**.
This is the easiest way for you to give us permission to use your contributions.
In effect, you’re giving us a license, but you still own the copyright—
so you retain the right to modify your code and use it in other projects.

You can sign the agreement here: **[ubuntu.com/legal/contributors](https://ubuntu.com/legal/contributors)**

If you have any questions, please reach out to us on our forum: [forum.snapcraft.io/c/snapd/5](https://forum.snapcraft.io/c/snapd/5)

---

## How Can I Contribute?

There are many ways to contribute to the project.

### **Reporting Bugs**

If you find a bug in the API specification (e.g., an incorrect data type, a missing field, or a confusing description),
please **[open a new issue](https://github.com/canonical/snapd-rest-openapi/issues)** in our issue tracker.

Please include:
* A clear and descriptive title.
* A detailed description of the bug, including the specific endpoint and field.
* What you expected to happen.
* What actually happened.

### **Suggesting Enhancements**

If you have an idea for a new feature or an improvement to an existing one,
please **[open a new issue](https://github.com/rnfudge02/canonical/issues)** to discuss it.
This allows us to coordinate our efforts and prevent duplication of work.

### **Your First Code Contribution**

Unsure where to begin? You can start by looking through issues tagged with `good first issue` or `help wanted`.
These are typically smaller changes that are a great way to get familiar with the project's workflow.

---

## Making Changes: The Workflow

Contributions are submitted through a **pull request** (PR) created from a fork of this repository.
Before making a PR, ensure that the specification is valid using the Makefile.

### **1. Setup Your Environment**

Fork the repository, clone it to your local machine, and create a new branch with a descriptive name.

```bash
# Clone your fork
git clone [https://github.com/YOUR_USERNAME/snapd-rest-openapi.git](https://github.com/rnfudge02/snapd-rest-openapi.git)
cd snapd-rest-openapi

# Create a new branch
git checkout -b feature/some-change
```
