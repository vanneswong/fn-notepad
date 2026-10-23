.PHONY: help build test vet run icons fpk fpk-arm64 clean

ARCH ?= amd64
DATA ?= ./data
ADDR ?= 127.0.0.1:18180

help:
	@echo "make run        启动本地开发服务 (http://$(ADDR))"
	@echo "make build      编译服务端二进制到 bin/notepad"
	@echo "make test       运行单元测试"
	@echo "make vet        静态检查"
	@echo "make icons      生成图标到 build/icons"
	@echo "make fpk        打包 fnOS 安装包 (默认 amd64)"
	@echo "make fpk-arm64  打包 arm64 安装包"

build:
	GOCACHE=$${GOCACHE:-/tmp/go-cache} go build -buildvcs=false -o bin/notepad ./cmd/notepad

run:
	GOCACHE=$${GOCACHE:-/tmp/go-cache} go run -buildvcs=false ./cmd/notepad -addr $(ADDR) -data $(DATA)

test:
	GOCACHE=$${GOCACHE:-/tmp/go-cache} go test -buildvcs=false ./...

vet:
	GOCACHE=$${GOCACHE:-/tmp/go-cache} go vet -buildvcs=false ./...

icons:
	GOCACHE=$${GOCACHE:-/tmp/go-cache} go run -buildvcs=false ./tools/icongen -out build/icons

fpk:
	./scripts/build-fpk.sh $(ARCH)

fpk-arm64:
	./scripts/build-fpk.sh arm64

clean:
	rm -rf bin dist
