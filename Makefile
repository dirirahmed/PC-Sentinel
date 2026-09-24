# Convenience targets. On Windows without make, run the commands directly
# (see README "Building").

BIN := bin/pcsentinel$(if $(filter Windows_NT,$(OS)),.exe,)

.PHONY: build web go test test-go test-web vet fmt run clean windows

build: web go

web:
	cd web && npm install && npm run build

go:
	go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/pcsentinel

windows: web
	GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o bin/pcsentinel.exe ./cmd/pcsentinel

test: test-go test-web

test-go: vet
	go test ./...
	go test -race ./...

test-web:
	cd web && npm test

vet:
	gofmt -l . | (! grep .) || (echo "gofmt needed" && exit 1)
	go vet ./...

run: go
	$(BIN) -open=false

clean:
	rm -rf bin web/dist/assets web/dist/index.html web/dist/favicon.svg
