.PHONY: build test test-race test-integration cover cover-integration cover-html dev-up dev-down lint fmt tidy clean ci

BIN       := debeasy
PKG       := ./...
COVER_OUT := coverage.out
COMPOSE   := docker compose -f docker-compose.dev.yml

build:
	go build -ldflags="-s -w" -o $(BIN) ./cmd/debeasy

test:
	go test -count=1 $(PKG)

test-race:
	go test -count=1 -race $(PKG)

# Requires the dev pg/mysql containers to be running — `make dev-up` first.
# Skipped tests print "not reachable" and are a no-op otherwise.
test-integration:
	go test -count=1 -tags=integration $(PKG)

cover:
	go test -count=1 -coverprofile=$(COVER_OUT) -covermode=atomic $(PKG)
	@go tool cover -func=$(COVER_OUT) | tail -1

# Full coverage including the live-DB driver tests.
cover-integration:
	go test -count=1 -tags=integration -coverprofile=$(COVER_OUT) -covermode=atomic $(PKG)
	@go tool cover -func=$(COVER_OUT) | tail -1

cover-html: cover
	go tool cover -html=$(COVER_OUT) -o coverage.html
	@echo "open ./coverage.html"

dev-up:
	$(COMPOSE) up -d

dev-down:
	$(COMPOSE) down

lint:
	@command -v golangci-lint >/dev/null || { \
	  echo "golangci-lint not installed. install with:"; \
	  echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$$(go env GOPATH)/bin v2.1.6"; \
	  exit 1; \
	}
	golangci-lint run $(PKG)

fmt:
	gofmt -s -w .
	goimports -local github.com/pfortini/debeasy -w .

tidy:
	go mod tidy

clean:
	rm -f $(BIN) $(COVER_OUT) coverage.html

# CI convenience: what the pipeline runs
ci: lint test-race cover
