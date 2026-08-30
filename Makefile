.PHONY: build test vet preview shellcheck compose-check

CONTENT ?= ../masepi
OUTPUT ?= dist

build:
	go run ./cmd/masepi build -content "$(CONTENT)" -output "$(OUTPUT)"

test:
	go test ./cmd/masepi ./internal/site

vet:
	go vet ./cmd/masepi ./internal/site

preview: build
	go run ./cmd/masepi serve -dir "$(OUTPUT)"

shellcheck:
	sh -n install.sh start.sh stop.sh restart.sh update.sh reload-tls.sh scripts/common.sh scripts/deploy.sh

compose-check:
	docker compose --env-file .env config --quiet
