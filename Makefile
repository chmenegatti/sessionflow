# SessionFlow — atalhos de desenvolvimento.
# `make` ou `make help` lista os alvos.

BIN          := bin/sessionflow
SERVER_BIN   := bin/sessionflow-server
SERVER_PORT  ?= 18745
FRONTEND     := frontend
VERSION      := $(shell cat VERSION)
BUILDER      := sessionflow-builder:$(VERSION)
DIST         := dist

.DEFAULT_GOAL := help
.PHONY: help deps dev build run server test test-race cover vet lint vuln typecheck test-web check bindings clean release test-install

help: ## Lista os alvos
	@awk 'BEGIN {FS = ":.*## "} /^[a-zA-Z_-]+:.*## / {printf "  \033[36m%-11s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

deps: ## Instala as dependências do frontend
	cd $(FRONTEND) && npm install

dev: ## Roda em modo desenvolvimento (hot reload)
	wails3 dev

build: ## Build de produção → bin/sessionflow
	wails3 build

run: build ## Builda e executa o app
	./$(BIN)

server: ## UI no navegador, só teste (porta: SERVER_PORT=18745)
	cd $(FRONTEND) && npm run build
	go build -tags server,production -o $(SERVER_BIN) .
	@echo "Abrindo em http://localhost:$(SERVER_PORT) (Ctrl+C para parar)"
	WAILS_SERVER_PORT=$(SERVER_PORT) ./$(SERVER_BIN)

test: ## Testes do backend
	go test ./...

test-race: ## Testes com detector de corrida
	go test -race ./...

cover: ## Testes com cobertura (relatório em bin/coverage.html)
	@mkdir -p bin
	go test -coverprofile=bin/coverage.out ./...
	go tool cover -func=bin/coverage.out | tail -1
	go tool cover -html=bin/coverage.out -o bin/coverage.html

vet: ## go vet
	go vet ./...

lint: ## staticcheck (go install honnef.co/go/tools/cmd/staticcheck@latest)
	@command -v staticcheck >/dev/null || { echo "staticcheck não instalado"; exit 1; }
	staticcheck ./...

vuln: ## govulncheck (go install golang.org/x/vuln/cmd/govulncheck@latest)
	@command -v govulncheck >/dev/null || { echo "govulncheck não instalado"; exit 1; }
	govulncheck ./...

typecheck: ## Typecheck do frontend
	cd $(FRONTEND) && npm run typecheck

test-web: ## Testes do frontend (Vitest)
	cd $(FRONTEND) && npm test

check: vet lint test-race typecheck test-web ## Tudo o que roda antes de um commit

bindings: ## Regenera os bindings TypeScript
	wails3 task common:generate:bindings

release: ## Build de release em contêiner (Ubuntu 24.04) → dist/: AppImage, .deb, .rpm, tar.gz, SHA256SUMS
	docker build --network host -t $(BUILDER) -f packaging/docker/Dockerfile.build packaging/docker
	rm -rf $(DIST) && mkdir -p $(DIST)
	docker run --rm --network host -v "$(CURDIR)":/src:ro -v "$(CURDIR)/$(DIST)":/out \
		-e HOST_UID=$$(id -u) -e HOST_GID=$$(id -g) $(BUILDER) bash /src/packaging/scripts/build-release.sh

test-install: ## Instala e executa os pacotes de dist/ em contêineres limpos (Ubuntu, Debian, Fedora)
	packaging/scripts/test-install.sh $(DIST)

clean: ## Remove binários e artefatos de build
	rm -rf bin $(DIST) $(FRONTEND)/dist
	mkdir -p $(FRONTEND)/dist && touch $(FRONTEND)/dist/.gitkeep # o go:embed exige um arquivo
