NAME=usql
BUILDDIR=build
BASEPATH := $(shell pwd)

LDFLAGS=-w -s

USQLBUILD=CGO_ENABLED=0 go build -trimpath

CURRENT_OS_ARCH = $(shell go env GOOS)-$(shell go env GOARCH)

SRCFILE=main.go run.go

build:
	$(USQLBUILD) -o $(BUILDDIR)/$(NAME)-$(CURRENT_OS_ARCH) $(SRCFILE)