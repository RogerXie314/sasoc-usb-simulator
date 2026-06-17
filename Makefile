.PHONY: build run test lint clean build-win build-win-gui build-linux

# 项目变量
APP_NAME := usb-simulator
BUILD_DIR := build

# Go 编译参数
LDFLAGS := -s -w

build:
	go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) .

run:
	go run . --debug

run-no-browser:
	go run . --no-browser

test:
	go test -v -race ./...

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BUILD_DIR)

# 交叉编译 Windows（控制台模式，保留黑窗口看日志）
build-win:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME).exe .

# 交叉编译 Windows GUI 模式（无黑窗口，日志写文件，启动自动打开浏览器）
build-win-gui:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS) -H windowsgui" -o $(BUILD_DIR)/$(APP_NAME)-gui.exe .

# 交叉编译 Linux
build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) .

# 前端构建
web-build:
	cd web && npm install && npm run build

# 全量构建（前端 + 后端）
build-all: web-build build

# 开发模式（前端热更新 + 后端）
dev:
	go run $(CMD) --debug

# 生成默认配置
gen-config:
	@go run $(CMD) --gen-config > config.yaml
