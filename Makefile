# Cross-platform Makefile for the Skybound Runner GoAkt game.
#
# Works with GNU Make on both Windows and Linux/macOS. On Windows, recipes
# run under cmd.exe (the default make shell); on Unix they run under /bin/sh.
# The OS is detected via the built-in $(OS) variable, which GNU Make sets to
# "Windows_NT" on Windows only.
#
# Usage:
#   make            build the client + server
#   make run        build then start the server (http://localhost:8080)
#   make web        recompile only the TypeScript client
#   make clean      remove build artifacts
#   make help       list all targets

BINARY := skybound-runner

ifeq ($(OS),Windows_NT)
	BIN       := bin/$(BINARY).exe
	RUN_BIN   := bin\$(BINARY).exe
	MKDIR_BIN := if not exist bin mkdir bin
	CLEAN_BIN := if exist bin rmdir /S /Q bin
else
	BIN       := bin/$(BINARY)
	RUN_BIN   := ./bin/$(BINARY)
	MKDIR_BIN := mkdir -p bin
	CLEAN_BIN := rm -rf bin
endif

.PHONY: all web build run deps tidy clean help

## all: build the client and server (default target)
all: build

## deps: install client (pnpm) and Go dependencies
deps:
	pnpm install
	go mod download

## tidy: sync go.mod / go.sum
tidy:
	go mod tidy

## web: install client deps and compile the TypeScript client to web/main.js
web:
	pnpm install
	pnpm run build

## build: compile the client, then build the Go binary into ./bin
build: web
	$(MKDIR_BIN)
	go build -o "$(BIN)" .

## run: build then start the server on http://localhost:8080
run: build
	$(RUN_BIN)

## clean: remove build artifacts
clean:
	$(CLEAN_BIN)

## help: list available targets
help:
	@echo Skybound Runner (GoAkt) - available make targets:
	@echo   make all     - build client + server (default)
	@echo   make build   - compile client and Go binary into ./bin
	@echo   make run     - build then start the server (http://localhost:8080)
	@echo   make web     - recompile only the TypeScript client (pnpm)
	@echo   make deps    - install pnpm + Go dependencies
	@echo   make tidy    - sync go.mod / go.sum
	@echo   make clean   - remove build artifacts
