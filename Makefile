VERSION?=development

.PHONE: install
install:
	@echo "==> Installing binary...${FOO_POO} ${VERSION}"
	@go install \
		--ldflags="-s -w \
			-X github.com/bringg/jenkins-autoscaler/cmd.version=${VERSION}" \
		./cmd/jas/

.PHONY: test
test: ginkgo_run_args = -r --always-emit-ginkgo-writer --randomize-all --randomize-suites --fail-on-pending --timeout=120s --race --cover --trace --compilers=2 -coverprofile=cover.out --output-dir=. --junit-report=junit.xml
test:
	@echo "==> Running tests..."
	@ginkgo ${ginkgo_run_args} $(ARGS)

.PHONY: lint
lint:
	@echo "==> Running lints..."
	@golangci-lint run

.PHONY: tools
tools:
	@echo "==> installing tools from tools.go..."
	@awk -F'"' '/_/ {print $$2}' tools/tools.go | xargs -tI % go install %

.PHONY: generate_mocks
generate_mocks: mocks = ./pkg/testing/mocks
generate_mocks:
	@echo "==> generating mocks..."
	@rm -rf ./pkg/testing/mocks
	@mkdir -p ./pkg/testing/mocks
	@mockgen -source=./pkg/dispatcher/dispatcher.go -destination=$(mocks)/dispatcher/scaler_mock.go Scalerer
	@mockgen -source=./pkg/scaler/client/client.go -destination=$(mocks)/scaler/client_mock.go Jenkinser
	@mockgen -source=./pkg/backend/registry.go -destination=$(mocks)/backend/backend_mock.go Backend Instance
