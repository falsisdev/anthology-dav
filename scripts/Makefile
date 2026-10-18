APP_NAME := anthology-dav
PROJECT_DIR := /Users/falsis/Desktop/Projects/anthology-dav
PLIST_NAME := com.falsisdev.anthology-dav.plist
PLIST_SRC := $(PROJECT_DIR)/scripts/$(PLIST_NAME)
PLIST_DST := $(HOME)/Library/LaunchAgents/$(PLIST_NAME)
LOG_FILE := /tmp/anthology-dav.log
ERR_LOG_FILE := /tmp/anthology-dav.err.log

.PHONY: build run install-service uninstall-service logs clean

build:
	cd $(PROJECT_DIR) && go mod tidy && go build -o $(PROJECT_DIR)/$(APP_NAME) .

run: build
	$(PROJECT_DIR)/$(APP_NAME) -host 127.0.0.1 -port 8085

install-service: build
	mkdir -p $(HOME)/Library/LaunchAgents
	cp $(PLIST_SRC) $(PLIST_DST)
	-launchctl unload $(PLIST_DST) 2>/dev/null || true
	launchctl load -w $(PLIST_DST)
	@echo "anthology-dav LaunchAgent installed and started at http://127.0.0.1:8085/dav/"

uninstall-service:
	-launchctl unload -w $(PLIST_DST) 2>/dev/null || true
	rm -f $(PLIST_DST)
	@echo "anthology-dav LaunchAgent uninstalled."

logs:
	tail -f $(LOG_FILE) $(ERR_LOG_FILE)

clean:
	rm -f $(PROJECT_DIR)/$(APP_NAME)
