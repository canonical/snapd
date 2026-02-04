<!--
    SPDX-FileCopyrightText: 2025 Canonical Ltd
    SPDX-License-Identifier: GPL-3.0-only
-->

# snapd-rest-openapi

A complete reimplementation of the [snapd REST API
documentation](https://snapcraft.io/docs/snapd-api) using the [OpenAPI 3]
(https://swagger.io/specification/) specification.

## Existing process

The [snapd](https://github.com/canonical/snapd/) REST API documentation is
manually created and updated whenever there are functional or syntactical
changes to the API.

This requires a snapd developer to be aware of the API modifications they make,
and to track those changes until they've been merged into the code base. It's
then their responsibility to update the REST API documentation manually.

### Existing format

The existing REST API documentation is written in Markdown, using
[markdown-it](https://github.com/markdown-it/markdown-it) and hosted on the
[Discourse-based](https://www.discourse.org/)
[forum.snapcraft.io](https://forum.snapcraft.io/t/snapd-rest-api/17954). From
there, it's published directly to the [official
documentation](https://snapcraft.io/docs).

The REST API Markdown file is tightly structured using headings, subheadings,
bullets and code blocks. These are manually added and adjusted when the API
changes. There is currently no automation, and no testing, and edits often
breaks the consistency and output of the source document.

Moving to an OpenAPI-based source document is intended to solve these problems.

## Repository contents

The repository is structured to modularly build a complete OpenAPI
specification. The main `openapi.yaml` file serves as the entry point,
referencing the various components defined in the `v2/` directory.

```
.
└── v2
│   ├── components
│   │   ├── errors
│   │   ├── parameters
│   │   ├── responses
│   │   ├── schemas
│   │   └── security
│   └── paths
└── openapi.yaml
└── tools
```

The `v2/` directory contains the individual OpenAPI components:
*   **components**: Reusable components like schemas, responses, and security schemes.
    *   **errors**: Defines the various error responses that the API can return.
    *   **parameters**: Defines reusable parameters for API operations.
    *   **responses**: Defines reusable responses for API operations.
    *   **schemas**: Defines the data models used in the API.
    *   **security**: Defines the security schemes used by the API.
*   **paths**: The individual API paths, with each file corresponding to
    an endpoint.

The `tools` directory contains files used to perform additional functionality:
*   **visualize.py**: Generates graphs showing the relation between
    endpoints and their dependency schemas. All endpoints possessing the
    same tag will be grouped in the same graph.
*   **post-process.py**: Injects formatting into the generated webpage.
    Currently used to create dark mode documentation webpage.
*   **dark-theme.css**: Contains the color definitions to use for dark mode.

For more detailed information on the project structure and how to update the
specification, please see [UPDATING.md](UPDATING.md).
