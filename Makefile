.PHONY: fmt vet lint test test-race test-integration build run-dry tidy check schedule-enable schedule-disable schedule-status

BIN := bin/job-hunt-agent

# 定期実行（方式A: launchd）。plist テンプレートを絶対パスへ置換して LaunchAgents へ登録する。
LAUNCHD_LABEL := com.job-hunt-agent.run
LAUNCHD_PLIST := $(HOME)/Library/LaunchAgents/$(LAUNCHD_LABEL).plist
LAUNCHD_TEMPLATE := deploy/launchd/$(LAUNCHD_LABEL).plist.template
LAUNCHD_WRAPPER := deploy/launchd/run-wrapper.sh
LAUNCHD_DOMAIN := gui/$(shell id -u)
SCHEDULE_LOGDIR := $(HOME)/Library/Logs/job-hunt-agent

fmt:
	gofmt -w .

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test ./...

test-race:
	go test -race ./...

test-integration:
	go test -tags=integration ./...

build:
	go build -o $(BIN) ./cmd/job-hunt-agent

run-dry: build
	$(BIN) run --dry-run

tidy:
	go mod tidy

check: fmt vet lint test

# 定期実行を有効化する（平日 08:00）。bin を作り、plist を生成して launchd へ登録する。
# 先に bootout するのは、既に登録済みだと bootstrap が失敗するため（再実行を冪等にする）。
schedule-enable: build
	@mkdir -p "$(SCHEDULE_LOGDIR)"
	@mkdir -p "$(HOME)/Library/LaunchAgents"
	@chmod +x "$(CURDIR)/$(LAUNCHD_WRAPPER)"
	@sed -e 's#__WRAPPER__#$(CURDIR)/$(LAUNCHD_WRAPPER)#g' \
	     -e 's#__WORKDIR__#$(CURDIR)#g' \
	     -e 's#__LOGDIR__#$(SCHEDULE_LOGDIR)#g' \
	     "$(LAUNCHD_TEMPLATE)" > "$(LAUNCHD_PLIST)"
	@launchctl bootout "$(LAUNCHD_DOMAIN)/$(LAUNCHD_LABEL)" 2>/dev/null || true
	@launchctl bootstrap "$(LAUNCHD_DOMAIN)" "$(LAUNCHD_PLIST)"
	@echo "定期実行を有効化しました（平日 08:00）。ログ: $(SCHEDULE_LOGDIR)"

# 定期実行を無効化する。未登録でも失敗しないようガードする。
schedule-disable:
	@launchctl bootout "$(LAUNCHD_DOMAIN)/$(LAUNCHD_LABEL)" 2>/dev/null || true
	@rm -f "$(LAUNCHD_PLIST)"
	@echo "定期実行を無効化しました。"

# 定期実行の状態を表示する。未登録なら無効である旨を出す。
schedule-status:
	@launchctl print "$(LAUNCHD_DOMAIN)/$(LAUNCHD_LABEL)" 2>/dev/null || echo "定期実行は無効です（未登録）。"
