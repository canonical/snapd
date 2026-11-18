<!--
    SPDX-FileCopyrightText: 2025 Canonical Ltd
    SPDX-License-Identifier: GPL-3.0-only
-->

# Snapd REST API OpenAPI specification

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

Redocly lints and bundles the OpenAPI specification. The published reference is rendered with Swagger UI in the [snap documentation](https://snapcraft.io/docs/reference/development/snapd-rest-api/).

## Contents

This directory is structured to modularly build a complete OpenAPI
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

- **components**: Reusable components like schemas, responses, and security schemes.
  - **errors**: Defines the various error responses that the API can return.
  - **parameters**: Defines reusable parameters for API operations.
  - **responses**: Defines reusable responses for API operations.
  - **schemas**: Defines the data models used in the API.
  - **security**: Defines the security schemes used by the API.
- **paths**: The individual API paths, with each file corresponding to
  an endpoint.

The `tools` directory contains files used to perform additional functionality:

- **visualize.py**: Generates graphs showing the relation between
  endpoints and their dependency schemas. All endpoints possessing the
  same tag will be grouped in the same graph.
- **post-process.py**: Injects formatting into the generated webpage.
  Currently used to create dark mode documentation webpage.
- **dark-theme.css**: Contains the color definitions to use for dark mode.

For more detailed information on the project structure and how to update the
specification, please see [UPDATING.md](UPDATING.md).

## Preview the specification

`make build` renders a Redocly preview of the specification. Although useful, this preview differs from the published reference, which uses Swagger UI.

To preview the Swagger UI rendering, run the follwoing command from `docs/api`.

```sh
sudo docker run --rm \
  -p 8080:8080 \
  -v "$PWD:/usr/share/nginx/html/spec:ro" \
  -e URL=/spec/openapi.yaml \
  swaggerapi/swagger-ui
```

Open `http://localhost:8080` to view the preview.
