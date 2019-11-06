OPERATOR_NAME := attacher

.PHONY: ensure run build-linux-amd64

run: build
	./bin/$(OPERATOR_NAME)

build:
	rm -f bin/$(OPERATOR_NAME)
	GO111MODULE=on go build -mod vendor -v -i -o bin/$(OPERATOR_NAME) ./main.go

ensure:
	GO111MODULE=on go mod tidy
	GO111MODULE=on go mod vendor

build-linux-amd64:
	rm -f bin/linux/$(OPERATOR_NAME)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GO111MODULE=on go build -mod vendor -v -i -o bin/linux/$(OPERATOR_NAME) ./main.go
