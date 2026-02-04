# SPDX-FileCopyrightText: 2025 Canonical Ltd
# SPDX-License-Identifier: GPL-3.0-only

# --- Docker Command Definitions ---
# Redocly CLI: Mounts current directory to /spec for API linting/building.
REDOCLY_CMD = docker run --rm -v "$(CURDIR):/spec" redocly/cli
# REUSE tool: Mounts to /spec and sets it as the working directory for compliance checks.
REUSE_CMD = docker run --rm -v "$(CURDIR):/spec" -w /spec fsfe/reuse

# --- Main Targets ---
# Default target: runs all linting, the documentation build and visuals generation.
all: lint build visuals

# Run all linting checks. This target calls the specific linters.
lint: clean lint-api lint-reuse

# Build the static HTML documentation.
build:
	@echo "--- Building documentation... ---"
	$(REDOCLY_CMD) build-docs ./openapi.yaml
	@echo "--- Documentation built successfully: redoc-static.html ---"
	@echo "--- Injecting dark theme CSS... ---"
	sudo chown ${USER} redoc-static.html
	python3 tools/post-process.py redoc-static.html tools/dark-theme.css "</head>"

# Clean up generated files.
clean: clean-build clean-visuals
	@echo "--- All generated files removed. ---"

clean-build:
	@echo "--- Removing generated documentation... ---"
	@rm -f redoc-static.html

clean-visuals:
	@echo "--- Removing generated visuals... ---"
	@rm -f openapi-bundled.yaml
	@rm -rf visuals

# --- Specific Linting Targets ---
# Lint the OpenAPI specification with Redocly.
lint-api:
	@echo "--- Linting OpenAPI specification (Redocly)... ---"
	$(REDOCLY_CMD) lint ./openapi.yaml

# Check for REUSE licensing and copyright compliance.
lint-reuse:
	@echo "--- Checking for REUSE compliance... ---"
	$(REUSE_CMD) lint

# --- Visuals Generation ---
# Bundle the OpenAPI specification into a single file.
bundle:
	@echo "--- Bundling OpenAPI specification... ---"
	$(REDOCLY_CMD) bundle ./openapi.yaml -o ./openapi-bundled.yaml

# Generate DOT files from the OpenAPI spec.
dots: bundle
	@echo "--- Generating DOT files... ---"
	python3 tools/visualize.py --max-edges=50
	python3 tools/visualize.py --dark --max-edges=50
	@rm -f openapi-bundled.yaml

# Convert DOT files to SVG.
svg: dots
	@echo "--- Converting DOT files to SVG... ---"
	@find visuals/light -name "*.dot" -exec sh -c 'dot -Tsvg "$$0" -o "$$0.svg"' {} \;
	@find visuals/dark -name "*.dot" -exec sh -c 'dot -Tsvg "$$0" -o "$$0.svg"' {} \;
	@rm -rf visuals/*/*.dot

# Generate all visuals (DOT and SVG).
visuals: svg

# Phony targets are not actual files.
.PHONY: all lint lint-api lint-reuse build clean clean-build clean-visuals bundle dots svg visuals