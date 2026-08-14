package main

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/usb-simulator/internal/api"
	"github.com/usb-simulator/internal/config"
	"github.com/usb-simulator/internal/hub"
	"github.com/usb-simulator/internal/model"
	"github.com/usb-simulator/internal/simulator"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

//go:embed all:web/dist
var webFS embed.FS

// AppVersion 应用版本号
const AppVersion = "V2.8.2"

// isConsoleAvailable 检测是否有可用的控制台（GUI 模式下没有）
func isConsoleAvailable() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// isPortAvailable 检测端口是否可用
func isPortAvailable(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("端口 %d 已被占用（可能已有实例在运行）", port)
	}
	ln.Close()
	return nil
}

func main() {
	// 将工作目录设置为可执行文件所在目录（确保配置文件路径正确）
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		os.Chdir(exeDir)
	}

	// 解析命令行参数
	noBrowser := false
	for _, arg := range os.Args[1:] {
		if arg == "--no-browser" || arg == "-n" {
			noBrowser = true
		}
	}

	hasConsole := isConsoleAvailable()

	// ---- 单实例检测（Windows 互斥体） ----
	if runtime.GOOS == "windows" {
		if !CreateMutex("Global\\UsbSimulatorSingleInstance") {
			msg := "移动介质安检站模拟测试工具已在运行中！\n\n请检查系统托盘图标，或在任务管理器中结束 usb-simulator-gui.exe 进程。"
			if hasConsole {
				fmt.Println(msg)
			} else {
				MessageBoxError("重复启动", msg)
			}
			os.Exit(1)
		}
	}

	// 1. 加载配置
	cfg, err := config.Load()
	if err != nil {
		msg := fmt.Sprintf("加载配置文件失败: %v", err)
		if hasConsole {
			fmt.Println(msg)
		} else {
			MessageBoxError("配置错误", msg)
		}
		os.Exit(1)
	}

	// ---- 端口冲突检测 ----
	if err := isPortAvailable(cfg.Server.Port); err != nil {
		msg := fmt.Sprintf("%v\n\n如需关闭已有实例，请在系统托盘右键选择「退出」，或在任务管理器中结束进程。", err)
		if hasConsole {
			fmt.Println(msg)
		} else {
			MessageBoxError("端口冲突", msg)
		}
		os.Exit(1)
	}

	// 2. 初始化日志（GUI 模式下日志写入文件）
	var logger *zap.Logger
	if !hasConsole {
		logDir := filepath.Join(".", "logs")
		os.MkdirAll(logDir, 0755)
		logPath := filepath.Join(logDir, fmt.Sprintf("usb-simulator-%s.log", time.Now().Format("2006-01-02")))

		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			MessageBoxError("日志错误", fmt.Sprintf("无法创建日志文件: %v", err))
			os.Exit(1)
		}

		encoderCfg := zap.NewProductionEncoderConfig()
		encoderCfg.TimeKey = "ts"
		encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

		core := zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(f),
			zapcore.InfoLevel,
		)
		logger = zap.New(core, zap.AddCaller())
	} else if cfg.Debug {
		logger, _ = zap.NewDevelopment()
	} else {
		logger, _ = zap.NewProduction()
	}
	defer logger.Sync()
	zap.ReplaceGlobals(logger)

	// 3. 初始化数据库
	db, err := model.InitDB(cfg.Database.Path)
	if err != nil {
		logger.Fatal("failed to init database", zap.Error(err))
	}
	defer db.Close()

	// 4. 启动内置 Echo Server（模拟 SASOC）
	echoSrv := simulator.NewEchoServer(cfg.Sasoc.Port)
	if err := echoSrv.Start(); err != nil {
		logger.Warn("echo server start failed (port may be in use)", zap.Error(err))
	} else {
		logger.Info("echo server started", zap.Int("port", cfg.Sasoc.Port))
	}

	// 5. 初始化 EventBus + Hub
	bus := hub.NewEventBus(256)
	h := hub.NewHub(db, bus, cfg)

	// 从数据库恢复已有站点
	if err := h.RestoreStations(); err != nil {
		logger.Warn("failed to restore stations from DB", zap.Error(err))
	} else {
		stationCount := h.StationCount()
		if stationCount > 0 {
			logger.Info("stations restored from DB", zap.Int("count", stationCount))
		}
	}

	// 从数据库恢复已有U盘设备
	if err := h.RestoreUsbs(); err != nil {
		logger.Warn("failed to restore USB devices from DB", zap.Error(err))
	} else {
		usbCount := h.UsbDeviceCount()
		if usbCount > 0 {
			logger.Info("USB devices restored from DB", zap.Int("count", usbCount))
		}
	}

	// 从数据库恢复管控柜插槽故障状态
	if err := h.RestoreCabinetSlotStatuses(); err != nil {
		logger.Warn("failed to restore cabinet slot statuses from DB", zap.Error(err))
	}

	// 控制台模式显示启动信息
	url := fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	if hasConsole {
		banner := fmt.Sprintf(
			"╔══════════════════════════════════════════════╗\n"+
				"║  移动介质安检站模拟测试工具 %-8s           ║\n"+
				"║  Web 控制台: http://localhost:%-4d           ║\n"+
				"║  Echo Server: localhost:%-4d (模拟SASOC)     ║\n"+
				"║  按 Ctrl+C 退出                              ║\n"+
				"╚══════════════════════════════════════════════╝\n",
			AppVersion, cfg.Server.Port, cfg.Sasoc.Port,
		)
		os.Stdout.Write([]byte(banner))
	} else {
		logger.Info("GUI mode started", zap.String("url", url))
	}

	// 6. 启动 HTTP 服务
	gin.SetMode(gin.ReleaseMode)
	router := api.SetupRouter(h, cfg)

	// 嵌入前端静态资源
	webDist, subErr := fs.Sub(webFS, "web/dist")
	var httpHandler http.Handler = router
	if subErr != nil {
		logger.Warn("failed to create sub filesystem for web/dist", zap.Error(subErr))
	} else {
		indexHTML, readErr := fs.ReadFile(webDist, "index.html")
		if readErr != nil {
			logger.Warn("failed to read index.html from embed", zap.Error(readErr))
		} else {
			logger.Info("web UI embedded", zap.Int("size", len(indexHTML)))
		}

		httpHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if (len(path) >= 4 && path[:4] == "/api") || (len(path) >= 3 && path[:3] == "/ws") {
				router.ServeHTTP(w, r)
				return
			}
			embedPath := path[1:]
			if embedPath != "" && embedPath != "index.html" {
				if data, err := fs.ReadFile(webDist, embedPath); err == nil {
					contentType := "application/octet-stream"
					switch {
					case len(embedPath) > 3 && embedPath[len(embedPath)-3:] == "css":
						contentType = "text/css; charset=utf-8"
					case len(embedPath) > 2 && embedPath[len(embedPath)-2:] == "js":
						contentType = "application/javascript; charset=utf-8"
					case len(embedPath) > 4 && embedPath[len(embedPath)-5:] == "json":
						contentType = "application/json; charset=utf-8"
					case len(embedPath) > 3 && embedPath[len(embedPath)-4:] == "png":
						contentType = "image/png"
					case len(embedPath) > 3 && embedPath[len(embedPath)-4:] == "svg":
						contentType = "image/svg+xml"
					case len(embedPath) > 3 && embedPath[len(embedPath)-4:] == "ico":
						contentType = "image/x-icon"
					}
					w.Header().Set("Content-Type", contentType)
					w.Write(data)
					return
				}
			}
			if len(indexHTML) > 0 {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Write(indexHTML)
				return
			}
			router.ServeHTTP(w, r)
		})
	}

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Server.Port),
		Handler: httpHandler,
	}

	go func() {
		logger.Info("server starting", zap.Int("port", cfg.Server.Port))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	// 7. 自动打开浏览器
	openBrowserFn := func() {
		// 等待 HTTP 服务就绪
		for i := 0; i < 30; i++ {
			resp, err := http.Get(url)
			if err == nil {
				resp.Body.Close()
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		openBrowser(url)
	}

	if !noBrowser {
		go openBrowserFn()
	}

	// 8. 退出信号
	quitCh := make(chan struct{})
	doShutdown := func() {
		select {
		case quitCh <- struct{}{}:
		default:
		}
	}

	// 控制台模式：Ctrl+C
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		doShutdown()
	}()

	// GUI 模式：系统托盘
	if !hasConsole && runtime.GOOS == "windows" {
		tray := NewTrayApp(
			"移动介质安检站模拟测试工具",
			url,
			openBrowserFn, // 双击托盘图标 → 打开浏览器
			doShutdown,    // 右键退出 → 触发关闭
		)
		go tray.Run()
	}

	// 9. 等待退出
	<-quitCh
	logger.Info("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	h.ShutdownAll()
	echoSrv.Stop()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("server forced shutdown", zap.Error(err))
	}
	logger.Info("server exited")
}

// openBrowser 跨平台打开默认浏览器
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
