.DEFAULT_GOAL := help

SWIFT_FORMAT_TARGETS := WendyAgentCore/Sources WendyAgentCore/Tests WendyAgentMac/Sources WendyE2ETests/Sources WendyE2ETests/Tests
SWIFT_LINT_TARGETS := WendyAgentCore/Sources WendyAgentCore/Tests WendyAgentMac/Sources

ifeq ($(OS),Windows_NT)
DETECTED_HOST_OS := windows
DETECTED_HOST_ENV := windows
else
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Darwin)
DETECTED_HOST_OS := macos
DETECTED_HOST_ENV := unix
else ifeq ($(UNAME_S),Linux)
DETECTED_HOST_OS := linux
DETECTED_HOST_ENV := unix
else
DETECTED_HOST_OS := unknown
DETECTED_HOST_ENV := unix
endif
endif

HOST_OS ?= $(DETECTED_HOST_OS)
HOST_ENV ?= $(DETECTED_HOST_ENV)

ifeq ($(HOST_ENV),windows)
override SHELL := powershell.exe
.SHELLFLAGS := -NoProfile -ExecutionPolicy Bypass -Command
else
SHELL := /bin/bash
endif

RUN_E2E_TEST_WINDOWS = @if (-not $$PSVersionTable) { throw 'Windows E2E targets require PowerShell.' }; ./Scripts/E2ETest.ps1 --default-run-id "$(1)" $(2)
RUN_E2E_RUN_WINDOWS = @if (-not $$PSVersionTable) { throw 'Windows E2E targets require PowerShell.' }; ./Scripts/E2ERun.ps1 --default-run-id "$(1)" $(2)

OPEN_E2E_REPORT_UNIX = report_path="$(1)"; if [ -f "$$report_path" ]; then if [ "$$(uname -s)" = "Darwin" ]; then open "$$report_path" || echo "HTML report: $$report_path"; else echo "HTML report: $$report_path"; fi; else echo "HTML report not found: $$report_path"; fi
RUN_E2E_TEST_UNIX = @run_id="$${WENDY_E2E_RUN_ID:-$(1)}"; \
	WENDY_E2E_RUN_ID="$$run_id" bash ./Scripts/E2ETest.sh $(2)
RUN_E2E_RUN_UNIX = @run_id="$${WENDY_E2E_RUN_ID:-$(1)}"; \
	status=0; \
	WENDY_E2E_RUN_ID="$$run_id" bash ./Scripts/E2ETest.sh $(2) || status=$$?; \
	bash ./Scripts/E2EReview.sh --run-dir "Build/e2e/$$run_id" || { step_status=$$?; [ $$status -eq 0 ] && status=$$step_status; }; \
	bash ./Scripts/E2EReport.sh --run-dir "Build/e2e/$$run_id" || { step_status=$$?; [ $$status -eq 0 ] && status=$$step_status; }; \
	$(call OPEN_E2E_REPORT_UNIX,Build/e2e/$$run_id/report.html); \
	exit $$status

.PHONY: help help-windows help-unix
help: help-$(HOST_ENV) ## Show this help message

help-windows:
	@if (-not $$PSVersionTable) { throw 'Windows make recipes require PowerShell.' }; Write-Output 'Usage:'; Write-Output '  make <target>'; Write-Output ''; Write-Output 'Targets:'; Select-String -Path '$(firstword $(MAKEFILE_LIST))' -Pattern '^[a-zA-Z0-9_-]+:.*?\#\# ' | ForEach-Object { $$parts = $$_.Line -split ':.*?\#\# ', 2; '  {0,-24} {1}' -f $$parts[0], $$parts[1] }

help-unix:
	@echo 'Usage:'; echo '  make <target>'; echo ''; echo 'Targets:'; grep -E '^[a-zA-Z0-9_-]+:.*?\#\# .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?\#\# "}; {printf "  %-24s %s\n", $$1, $$2}'

.PHONY: format format-windows format-unix
format: format-$(HOST_ENV) ## Format Swift code using swift-format

format-windows:
	swift format --recursive --in-place $(SWIFT_FORMAT_TARGETS)

format-unix:
	swift format --recursive --in-place $(SWIFT_FORMAT_TARGETS)

.PHONY: lint lint-windows lint-unix
lint: lint-$(HOST_ENV) ## Lint Swift code using swift-format

lint-windows:
	swift format lint --recursive --strict $(SWIFT_LINT_TARGETS)

lint-unix:
	bash ./Scripts/Lint.sh

.PHONY: test test-windows test-unix
test: test-$(HOST_ENV) ## Run Swift package tests

test-windows:
	@if (-not $$PSVersionTable) { throw 'Windows test target requires PowerShell.' }; New-Item -ItemType Directory -Force -Path Build/SwiftPM | Out-Null; Push-Location WendyAgentCore; try { swift test --scratch-path ../Build/SwiftPM; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE } } finally { Pop-Location }

test-unix:
	bash ./Scripts/Test.sh

.PHONY: e2e-test e2e-test-windows e2e-test-unix e2e-run e2e-run-windows e2e-run-unix
e2e-test e2e-run: DEVICE ?= wendyos-raspberry-pi-5.local
e2e-test: e2e-test-$(HOST_ENV) ## Run Swift E2E tests locally and write raw artifacts

e2e-test-windows:
	$(call RUN_E2E_TEST_WINDOWS,windows-to-raspberry-pi-5,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

e2e-test-unix:
	$(call RUN_E2E_TEST_UNIX,local,--output-dir Build/e2e --cli-root-dir Build/e2e --agent-root-dir Build/e2e --parallel)

e2e-run: e2e-run-$(HOST_ENV) ## Run Swift E2E tests locally, review results, render the report, and open it

e2e-run-windows:
	$(call RUN_E2E_RUN_WINDOWS,windows-to-raspberry-pi-5,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

e2e-run-unix:
	$(call RUN_E2E_RUN_UNIX,local,--output-dir Build/e2e --cli-root-dir Build/e2e --agent-root-dir Build/e2e --parallel)

.PHONY: e2e-test-jetson-orin-nano e2e-test-jetson-orin-nano-windows e2e-test-jetson-orin-nano-unix e2e-run-jetson-orin-nano e2e-run-jetson-orin-nano-windows e2e-run-jetson-orin-nano-unix
e2e-test-jetson-orin-nano e2e-run-jetson-orin-nano: DEVICE ?= wendyos-jetson-orin-nano.local
e2e-test-jetson-orin-nano: e2e-test-jetson-orin-nano-$(HOST_ENV) ## Run Swift E2E tests against Jetson Orin Nano and write raw artifacts

e2e-test-jetson-orin-nano-windows:
	$(call RUN_E2E_TEST_WINDOWS,jetson-orin-nano,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

e2e-test-jetson-orin-nano-unix:
	$(call RUN_E2E_TEST_UNIX,jetson-orin-nano,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

e2e-run-jetson-orin-nano: e2e-run-jetson-orin-nano-$(HOST_ENV) ## Run Swift E2E tests against Jetson Orin Nano, review results, render the report, and open it

e2e-run-jetson-orin-nano-windows:
	$(call RUN_E2E_RUN_WINDOWS,jetson-orin-nano,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

e2e-run-jetson-orin-nano-unix:
	$(call RUN_E2E_RUN_UNIX,jetson-orin-nano,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

.PHONY: e2e-test-raspberry-pi-5 e2e-test-raspberry-pi-5-windows e2e-test-raspberry-pi-5-unix e2e-run-raspberry-pi-5 e2e-run-raspberry-pi-5-windows e2e-run-raspberry-pi-5-unix
e2e-test-raspberry-pi-5 e2e-run-raspberry-pi-5: DEVICE ?= wendyos-raspberry-pi-5.local
e2e-test-raspberry-pi-5: e2e-test-raspberry-pi-5-$(HOST_ENV) ## Run Swift E2E tests against Raspberry Pi 5 and write raw artifacts

e2e-test-raspberry-pi-5-windows:
	$(call RUN_E2E_TEST_WINDOWS,raspberry-pi-5,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

e2e-test-raspberry-pi-5-unix:
	$(call RUN_E2E_TEST_UNIX,raspberry-pi-5,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

e2e-run-raspberry-pi-5: e2e-run-raspberry-pi-5-$(HOST_ENV) ## Run Swift E2E tests against Raspberry Pi 5, review results, render the report, and open it

e2e-run-raspberry-pi-5-windows:
	$(call RUN_E2E_RUN_WINDOWS,raspberry-pi-5,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

e2e-run-raspberry-pi-5-unix:
	$(call RUN_E2E_RUN_UNIX,raspberry-pi-5,--output-dir Build/e2e --agent-os wendyOS --agent-address "$(DEVICE)")

.PHONY: e2e-test-mac-mini e2e-test-mac-mini-windows e2e-test-mac-mini-unix e2e-run-mac-mini e2e-run-mac-mini-windows e2e-run-mac-mini-unix
e2e-test-mac-mini e2e-run-mac-mini: DEVICE ?= mac-mini.local
e2e-test-mac-mini: e2e-test-mac-mini-$(HOST_ENV) ## Run Swift E2E tests against Mac mini and write raw artifacts

e2e-test-mac-mini-windows:
	$(call RUN_E2E_TEST_WINDOWS,mac-mini,--output-dir Build/e2e --agent-os macOS --agent-address "$(DEVICE)")

e2e-test-mac-mini-unix:
	$(call RUN_E2E_TEST_UNIX,mac-mini,--output-dir Build/e2e --agent-os macOS --agent-address "$(DEVICE)")

e2e-run-mac-mini: e2e-run-mac-mini-$(HOST_ENV) ## Run Swift E2E tests against Mac mini, review results, render the report, and open it

e2e-run-mac-mini-windows:
	$(call RUN_E2E_RUN_WINDOWS,mac-mini,--output-dir Build/e2e --agent-os macOS --agent-address "$(DEVICE)")

e2e-run-mac-mini-unix:
	$(call RUN_E2E_RUN_UNIX,mac-mini,--output-dir Build/e2e --agent-os macOS --agent-address "$(DEVICE)")

.PHONY: reference reference-windows reference-unix
reference: reference-$(HOST_ENV) ## Generate Swift E2E HTML reference documentation and open it

reference-windows:
	@if (-not $$PSVersionTable) { throw 'Windows reference target requires PowerShell.' }; Remove-Item -LiteralPath Build/Reference -Recurse -Force -ErrorAction SilentlyContinue; New-Item -ItemType Directory -Force -Path Build/Reference | Out-Null; Push-Location WendyE2ETests; try { swift run swift-e2e-testing reference --format html --output ../Build/Reference Tests/WendyE2ETests; if ($$LASTEXITCODE -ne 0) { exit $$LASTEXITCODE } } finally { Pop-Location }; Start-Process Build/Reference/index.html

reference-unix:
	rm -rf Build/Reference
	mkdir -p Build/Reference
	cd WendyE2ETests && swift run swift-e2e-testing reference --format html --output ../Build/Reference Tests/WendyE2ETests
	open Build/Reference/index.html

.PHONY: proto proto-windows proto-unix
proto: proto-$(HOST_ENV) ## Generate Swift gRPC code from Proto/

proto-windows:
	@throw 'proto is not supported on Windows yet; use a unix host for Scripts/GenerateProto.sh.'

proto-unix:
	./Scripts/GenerateProto.sh

.PHONY: clean clean-windows clean-unix
clean: clean-$(HOST_ENV) ## Remove Build/

clean-windows:
	Remove-Item -LiteralPath Build -Recurse -Force -ErrorAction SilentlyContinue

clean-unix:
	rm -rf Build/

.PHONY: agent-release agent-release-windows agent-release-macos agent-release-linux
agent-release: agent-release-$(HOST_OS) ## Build, sign, notarize, and package the macOS app

agent-release-macos:
	VERSION='$(VERSION)' OUTPUT_DIR='$(OUTPUT_DIR)' bash ./Scripts/Build.sh

agent-release-linux:
	@echo 'ERROR: agent-release is macOS-only.' >&2; exit 1

agent-release-windows:
	@throw 'agent-release is macOS-only.'

.PHONY: build-cli build-cli-windows build-cli-unix
build-cli: build-cli-$(HOST_ENV) ## Build the Go CLI in ../go

build-cli-windows:
	$(MAKE) -C ../go build-cli HOST_ENV=windows

build-cli-unix:
	$(MAKE) -C ../go build-cli HOST_ENV=unix

.PHONY: agent-build agent-build-windows agent-build-macos agent-build-linux
agent-build: agent-build-$(HOST_OS) ## Build and package a dev-signed macOS app without notarization

agent-build-macos:
	OUTPUT_DIR='$(OUTPUT_DIR)' bash ./Scripts/Build.sh --dev

agent-build-linux:
	@echo 'ERROR: agent-build is macOS-only.' >&2; exit 1

agent-build-windows:
	@throw 'agent-build is macOS-only.'

.PHONY: agent-start agent-start-windows agent-start-macos agent-start-linux
agent-start: agent-start-$(HOST_OS) ## Stop, build, and open the WendyAgentMac app

agent-start-macos:
	@$(MAKE) agent-stop
	@$(MAKE) agent-build
	open Build/WendyAgentMac.app

agent-start-linux:
	@echo 'ERROR: agent-start is macOS-only.' >&2; exit 1

agent-start-windows:
	@throw 'agent-start is macOS-only.'

.PHONY: agent-reset-settings agent-reset-settings-windows agent-reset-settings-macos agent-reset-settings-linux
agent-reset-settings: agent-reset-settings-$(HOST_OS) ## Reset WendyAgentMac preferences/defaults

agent-reset-settings-macos:
	./Scripts/ResetMacAppSettings.sh

agent-reset-settings-linux:
	@echo 'ERROR: agent-reset-settings is macOS-only.' >&2; exit 1

agent-reset-settings-windows:
	@throw 'agent-reset-settings is macOS-only.'

.PHONY: agent-reset-permissions agent-reset-permissions-windows agent-reset-permissions-macos agent-reset-permissions-linux
agent-reset-permissions: agent-reset-permissions-$(HOST_OS) ## Reset WendyAgentMac permissions

agent-reset-permissions-macos:
	./Scripts/ResetMacAppTCC.sh

agent-reset-permissions-linux:
	@echo 'ERROR: agent-reset-permissions is macOS-only.' >&2; exit 1

agent-reset-permissions-windows:
	@throw 'agent-reset-permissions is macOS-only.'

.PHONY: agent-reset agent-reset-windows agent-reset-macos agent-reset-linux
agent-reset: agent-reset-$(HOST_OS) ## Reset WendyAgentMac settings and permissions

agent-reset-macos: agent-reset-settings-macos agent-reset-permissions-macos

agent-reset-linux:
	@echo 'ERROR: agent-reset is macOS-only.' >&2; exit 1

agent-reset-windows:
	@throw 'agent-reset is macOS-only.'

.PHONY: agent-stop agent-stop-windows agent-stop-macos agent-stop-linux
agent-stop: agent-stop-$(HOST_OS) ## Quit WendyAgentMac and fail if it is still running after 10 seconds

agent-stop-macos:
	./Scripts/Quit.sh

agent-stop-linux:
	@echo 'ERROR: agent-stop is macOS-only.' >&2; exit 1

agent-stop-windows:
	@throw 'agent-stop is macOS-only.'
