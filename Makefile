.PHONY: build
build:
	go build -o embedded-openfga

.PHONE: run

run:
	docker compose up --build -d

.PHONY: stop
stop:
	docker compose down

.PHONY: lint
lint:
	golangci-lint run

.PHONY: lint-fix
lint-fix:
	golangci-lint run --fix ./...
