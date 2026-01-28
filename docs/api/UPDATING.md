<!--
    SPDX-FileCopyrightText: 2025 Canonical Ltd
    SPDX-License-Identifier: GPL-3.0-only
-->

# Updating the OpenAPI specification

In order to update the OpenAPI Specification, the following procedure should be
followed to reduce the likelihood of errors occurring.

## 1. Identify schema requiring updates

The schema files are referenced from within other schemas, responses, and paths.
As such, modifying these files will often have larger than intended effects on
the documentation as a whole, and as such should be handled first. The
visualization tool invoked via `make visuals` may aid in this work

### 1.1 Divergence of schema

The following example will use 3 files, defined below:
- File A: A schema that is referenced by File B and C.
- File B: A response file that needs updating as a result of changes to the
  repo.
- File C: A path file that is not modified as a result of repo changes.

When updating the repo, begin with checking file A, as it is a schema. For the
purpose of the example, assume the change in the response affects the output,
which is a reference to file A.

To resolve this issue, the schema will need to diverge. If this accurately
reflects the change in Go structures, then documentation should be simple.
However, sometimes this is not the case.

When this happens, the recommended approach is to create a copy of the original
schema and denote the copy with a suffix indicating the divergence cause. If the
original structure diverges for multiple reasons, a prefix may be used as well.
Some examples of this in the documentation are:
- AppActionX - The suffix denotes the changes were a result of differing
  functionality.
- PromptConstraintsX, ReplyConstraintsX - The suffix denotes the interface the
  documentation refers to. In this case the prefix (e.g. Prompt/Reply) denotes
  that another divergence occurred as a result of functionality.

The copied file, with the modified name, should be updated to the new
representation, and the required references should be updated to point to the
modified file.

## 2. Update responses and errors
After the required schemas have been updated, the next files to target should be
responses. These rely on schemas, and errors (see below), and are relied upon by
paths. If the code changes made modify the output a user sees (e.g. with `snap
debug api`) then a response file likely needs updating.

### 2.1 Updating error schema
Errors are a subset of Schemas used solely by responses, and are thus tied to a
path indirectly through the use of response references. If new errors have been
added, or existing errors require modification, these generally do not require
divergence, but updates to the responses they are contained in may be needed.

## 3. Paths
Paths are mostly composed of references to schemas and responses. For certain
components, if they are not reused elsewhere, it is possible to define them
inline within the path. For components defined inline, they are completely self
contained, and as such changes to these files do not propagate to other
dependencies.

## 4. OpenAPI.yaml
The master record of every file in the project. Every schema, response, error,
and path is defined here with an assigned name. Technically, for the project to
pass linting, all that is needed are certain metadata blocks, and a path, that
individually passes linting. If a path is added to the project, it will not be
linted unless it is defined in the main OpenAPI.yaml file. If a schema is not
referenced within a path, unless it is defined in the main OpenAPI.yaml file, it
will not be linted.

The main OpenAPI.yaml file has tags that can be used to group operations. If a
new tag is created, it will need to be applied to all relevant operations within
their respective path files.

## 5. Security (if needed)
The security files are a subset of schema that define the security requirements
required to interact with an operation. They are defined per operation in a path
file. The currently documented schema are:
- OpenAccess ([]) - denoted by empty brackets, means there are no authentication
  requirements for the operation.
- PeerAuth - Describes how the daemon uses unix peer socket authentication to
  ensure the user has proper permission for using an operation.
- RootAuth - Describes which daemon endpoints require root access. The
  distinction between this file and `PeerAuth` is not well documented, and will
  be addressed in a future PR. Endpoints tagged as `Root` have been carried over
  from the previous documentation.

## 6. Theming
The project utilizes [Redocly](https://redocly.com/) for the documentation
bundling and generation. We tweak the default bundle with the following
post-processing steps to allow for theming changes, and for both light
and dark compatible display modes:
1. Once the API contents have been updated in the documentation, begin by
  viewing the output and ensuring it generates as intended. If it does not, use
  `redocly.yaml`, located in the root directory of the project, to modify the
  light mode webpage to the desired page formatting.
2. After ensuring the light mode displays in a desirable layout with the
  appropriate formatting, view the dark mode page. If there are any
  inconsistencies, weird coloring, etc. proceed to the next step.
3. To modify the expression of dark mode, the build process uses the file
  `tools/post-process.py` to apply the contents of `tools/dark-theme.css`
  to the project. To change the expression of dark mode:
    - Modify `tools/post-process.py` if there is additional formatting changes
    that should be made to the dark mode version of the webpage.
    - Modify `tools/dark-theme.css` to change the colors that are applied to the
    dark mode webpage.