.PHONY: build-portal build-portal-openapi run start export-openapi clean-api-docs

# Runtime flow (Option B): cleans any generated API docs, then builds the
# portal into go/portal-build/ (requires Node 20+).
build-portal: clean-api-docs
	npm ci
	npm run build

# Builds the portal, then runs the Go demo with everything embedded.
run: build-portal
	go run ./go

# Runs the demo with the committed placeholder build (no Node required).
start:
	go run ./go

# Exports openapi.json for the optional build-time pipeline (Option A).
export-openapi:
	go run ./export

# Build-time flow (Option A): exports the contract, generates the versioned
# MDX API docs from it and builds the portal with the plugin enabled.
build-portal-openapi: export-openapi
	OPENAPI_DOCS_PLUGIN=1 npm run gen-api-docs
	OPENAPI_DOCS_PLUGIN=1 npm run build

# Removes the generated API docs (required before switching back to the
# runtime flow, because the generated MDX needs the openapi-docs theme).
clean-api-docs:
	OPENAPI_DOCS_PLUGIN=1 npm run clean-api-docs || rm -rf docs/api
