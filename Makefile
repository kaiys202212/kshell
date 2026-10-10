BINARY := kshell

.PHONY: build test vet lint install clean cross build-desktop

build:
	go build -o dist/$(BINARY)$(shell go env GOEXE) ./cmd/kshell

test:
	go test ./... -count=1

vet:
	go vet ./...

lint: vet test

install:
	go install ./cmd/kshell

cross:
	GOOS=windows GOARCH=amd64 go build -o dist/$(BINARY)-windows-amd64.exe ./cmd/kshell
	GOOS=linux GOARCH=amd64 go build -o dist/$(BINARY)-linux-amd64 ./cmd/kshell
	GOOS=darwin GOARCH=arm64 go build -o dist/$(BINARY)-darwin-arm64 ./cmd/kshell

build-desktop:
	./build.sh -Desktop

build-desktop-v:
	./build.sh -Desktop -Version dev

clean:
	rm -rf dist
