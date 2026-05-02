BINARY ?= thoth-operator
IMAGE ?= ghcr.io/atensecurity/thoth-operator:0.1.0

.PHONY: fmt
fmt:
	gofmt -w ./api ./controllers ./cmd ./internal

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: test
test:
	go test ./...

.PHONY: build
build:
	CGO_ENABLED=0 go build -o bin/$(BINARY) ./cmd/manager

.PHONY: docker-build
docker-build:
	docker build -t $(IMAGE) .

.PHONY: install-crd
install-crd:
	kubectl apply -f config/crd/bases/platform.atensecurity.com_thothtenants.yaml

.PHONY: deploy
deploy:
	kubectl apply -f config/rbac/role.yaml
	kubectl apply -f config/manager/manager.yaml
