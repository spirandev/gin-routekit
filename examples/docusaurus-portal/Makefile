.PHONY: build-portal run start

# Generates the real Docusaurus build into go/portal-build/ (requires Node 20+).
build-portal:
	npm ci
	npm run build

# Builds the portal, then runs the Go demo with everything embedded.
run: build-portal
	go run ./go

# Runs the demo with the committed placeholder build (no Node required).
start:
	go run ./go
