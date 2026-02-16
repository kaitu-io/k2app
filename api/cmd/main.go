package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"crypto/md5"
	"encoding/hex"

	center "github.com/kaitu-io/k2app/api"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wordgate/qtoolkit/aws/ssm"
	"github.com/wordgate/qtoolkit/log"
	"github.com/wordgate/qtoolkit/util"
)

var (
	configFile string
	// 版本信息变量
	Version   = "dev"
	BuildTime = "unknown"
	GitCommit = "unknown"
)

const (
	// SSM Parameter Store keys
	SSMSecretKeyParameter = "/kaitu/center/secret-key"
)

var (
	foreground bool // 前台运行标志
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func getPidFile() string {
	if runtime.GOOS == "darwin" {
		return "/tmp/kaitu-center.pid"
	}
	return "/apps/kaitu/kaitu-center.pid"
}

var rootCmd = &cobra.Command{
	Use:   "center",
	Short: "Kaitu center service",
	Long:  `Kaitu center service is the main service for Kaitu platform.`,
}

func daemonize() {
	if os.Getenv("_KAITU_DAEMON") == "1" {
		return // 已在后台
	}
	key := getOrInitSecretKeyAndCA(true)
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Env = append(os.Environ(), "_KAITU_DAEMON=1")

	// 后台进程的输出重定向到 /dev/null
	devNull, err := os.OpenFile("/dev/null", os.O_WRONLY, 0644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法打开 /dev/null: %v\n", err)
		os.Exit(1)
	}
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	stdin, err := cmd.StdinPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "创建管道失败: %v\n", err)
		os.Exit(1)
	}

	err = cmd.Start()
	if err != nil {
		fmt.Fprintf(os.Stderr, "后台启动失败: %v\n", err)
		os.Exit(1)
	}

	_, err = stdin.Write([]byte(key))
	if err != nil {
		fmt.Fprintf(os.Stderr, "传递密钥失败: %v\n", err)
		os.Exit(1)
	}
	stdin.Close()
	fmt.Printf("已在后台启动，PID: %d\n", cmd.Process.Pid)
	os.Exit(0)
}

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the HTTP server (daemon mode by default, use -f for foreground)",
	Run: func(cmd *cobra.Command, args []string) {
		util.SetConfigFile(configFile)
		pidFile := getPidFile()
		if pid, err := readPidFile(pidFile); err == nil {
			if isProcessRunning(pid) {
				fmt.Printf("服务已在运行，PID: %d\n", pid)
				return
			}
		}

		if foreground {
			// 前台模式运行
			runServerInForeground()
		} else {
			// 后台模式运行
			daemonize()

			// --- 从这里开始是后台守护进程的逻辑 ---

			// 写入 PID 文件
			if err := writePidFile(pidFile); err != nil {
				log.Fatalf(context.Background(), "写入 PID 文件失败: %v", err)
			}
			defer os.Remove(pidFile) // 确保进程退出时删除 PID 文件

			// 设置信号处理
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
			go func() {
				s := <-sigChan
				log.Infof(context.Background(), "接收到信号 %v, 服务正在关闭...", s)
				os.Remove(pidFile) // 在退出前再次尝试删除
				os.Exit(0)
			}()

			log.Infof(context.Background(), "kaitu-center 服务已启动")

			// 启动一个带崩溃恢复的服务 goroutine
			go runServerWithCrashProtection()

			// 保持主 goroutine 运行
			<-sigChan // 等待终止信号
		}
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations",
	Run: func(cmd *cobra.Command, args []string) {
		util.SetConfigFile(configFile)
		// 执行数据库迁移
		if err := center.Migrate(); err != nil {
			log.Fatalf(context.Background(), "Failed to run migrations: %v", err)
		}
		fmt.Println("Migrations completed successfully")
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "显示版本信息",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("版本: %s\n", Version)
		fmt.Printf("构建时间: %s\n", BuildTime)
		fmt.Printf("Git提交: %s\n", GitCommit)
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "显示 Watchdog 主进程状态",
	Run: func(cmd *cobra.Command, args []string) {
		pid, err := readPidFile(getPidFile())
		if err != nil {
			fmt.Println("未找到 PID 文件，服务未运行")
			return
		}
		if isProcessRunning(pid) {
			fmt.Printf("Watchdog 正在运行，PID: %d\n", pid)
		} else {
			fmt.Printf("PID 文件存在但进程 %d 未运行\n", pid)
		}
	},
}

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "停止 Watchdog 主进程",
	Run: func(cmd *cobra.Command, args []string) {
		pidFile := getPidFile()
		pid, err := readPidFile(pidFile)
		if err != nil {
			fmt.Println("未找到 PID 文件，服务未运行")
			return
		}

		if !isProcessRunning(pid) {
			fmt.Printf("进程 %d 未运行，删除 PID 文件\n", pid)
			os.Remove(pidFile)
			return
		}

		fmt.Printf("正在停止进程 %d...\n", pid)
		err = syscall.Kill(pid, syscall.SIGTERM)
		if err != nil {
			fmt.Printf("发送 SIGTERM 失败: %v\n", err)
			return
		}

		// 等待最多5秒
		for i := 0; i < 50; i++ {
			if !isProcessRunning(pid) {
				fmt.Println("进程已停止")
				os.Remove(pidFile)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}

		fmt.Println("进程未能在5秒内停止，请手动检查")
	},
}

var healthCheckCmd = &cobra.Command{
	Use:   "health-check",
	Short: "Check health status of all k2wss tunnels",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		util.SetConfigFile(configFile)
		_ = getOrInitSecretKeyAndCA(true)
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		fmt.Println("Starting health check for all k2wss tunnels...")

		results, err := center.CheckAllK2WSSTunnelsHealth(ctx)
		if err != nil {
			fmt.Printf("Health check failed: %v\n", err)
			return
		}

		// 更新健康状态
		if err := center.UpdateTunnelHealthStatus(ctx, results); err != nil {
			fmt.Printf("Failed to update health status: %v\n", err)
			return
		}

		// 打印汇总结果
		healthyCount := 0
		unhealthyCount := 0
		for _, result := range results {
			if result.IsHealthy {
				healthyCount++
			} else {
				unhealthyCount++
			}
		}

		fmt.Printf("\nHealth check completed:\n")
		fmt.Printf("  Total tunnels: %d\n", len(results))
		fmt.Printf("  Healthy: %d\n", healthyCount)
		fmt.Printf("  Unhealthy: %d\n", unhealthyCount)

		// 显示不健康的隧道
		if unhealthyCount > 0 {
			fmt.Printf("\nUnhealthy tunnels:\n")
			for _, result := range results {
				if !result.IsHealthy {
					fmt.Printf("  - %s (ID: %d): %s\n", result.Domain, result.TunnelID, result.ErrorMsg)
				}
			}
		}
	},
}

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Install kaitu-center as a systemd service (Ubuntu Linux only)",
	Run: func(cmd *cobra.Command, args []string) {
		if runtime.GOOS != "linux" {
			fmt.Println("错误: install 命令仅支持 Linux 系统")
			os.Exit(1)
		}

		// 确定配置文件路径
		cfgFile := configFile
		if cfgFile == "" {
			cfgFile = "/apps/kaitu/config.yml"
		}

		// 确定工作目录
		workDir := "/apps/kaitu"

		// 获取当前可执行文件路径
		execPath, err := os.Executable()
		if err != nil {
			fmt.Printf("获取可执行文件路径失败: %v\n", err)
			os.Exit(1)
		}

		// 创建 systemd service 文件（自动尝试 AWS SSM，失败则提示输入）
		serviceContent := fmt.Sprintf(`[Unit]
Description=Kaitu Center Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=%s
Environment="KAITU_CONFIG_FILE=%s"
ExecStart=%s start -f -c %s
Restart=always
RestartSec=10s
LimitNOFILE=999999
StandardOutput=journal
StandardError=journal
SyslogIdentifier=kaitu-center

[Install]
WantedBy=multi-user.target
`, workDir, cfgFile, execPath, cfgFile)

		servicePath := "/etc/systemd/system/kaitu-center.service"
		err = os.WriteFile(servicePath, []byte(serviceContent), 0644)
		if err != nil {
			fmt.Printf("创建 systemd service 文件失败: %v\n", err)
			fmt.Println("提示: 请使用 sudo 权限运行此命令")
			os.Exit(1)
		}

		// Reload systemd
		cmd1 := exec.Command("systemctl", "daemon-reload")
		if output, err := cmd1.CombinedOutput(); err != nil {
			fmt.Printf("重载 systemd 失败: %v\n%s\n", err, output)
			os.Exit(1)
		}

		// Enable service
		cmd2 := exec.Command("systemctl", "enable", "kaitu-center")
		if output, err := cmd2.CombinedOutput(); err != nil {
			fmt.Printf("启用服务失败: %v\n%s\n", err, output)
			os.Exit(1)
		}

		fmt.Println("\n✅ 服务安装成功！")
		fmt.Printf("配置文件: %s\n", cfgFile)
		fmt.Println("\n使用以下命令管理服务:")
		fmt.Println("  sudo systemctl start kaitu-center    # 启动服务")
		fmt.Println("  sudo systemctl stop kaitu-center     # 停止服务")
		fmt.Println("  sudo systemctl restart kaitu-center  # 重启服务")
		fmt.Println("  sudo systemctl status kaitu-center   # 查看状态")
		fmt.Println("  sudo journalctl -u kaitu-center -f   # 查看日志")
	},
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall kaitu-center service (Ubuntu Linux only)",
	Run: func(cmd *cobra.Command, args []string) {
		if runtime.GOOS != "linux" {
			fmt.Println("错误: uninstall 命令仅支持 Linux 系统")
			os.Exit(1)
		}

		// Stop service
		cmd1 := exec.Command("systemctl", "stop", "kaitu-center")
		cmd1.CombinedOutput() // Ignore error if not running

		// Disable service
		cmd2 := exec.Command("systemctl", "disable", "kaitu-center")
		cmd2.CombinedOutput() // Ignore error if not enabled

		// Remove service file
		servicePath := "/etc/systemd/system/kaitu-center.service"
		err := os.Remove(servicePath)
		if err != nil && !os.IsNotExist(err) {
			fmt.Printf("删除 service 文件失败: %v\n", err)
			os.Exit(1)
		}

		// Reload systemd
		cmd3 := exec.Command("systemctl", "daemon-reload")
		if output, err := cmd3.CombinedOutput(); err != nil {
			fmt.Printf("重载 systemd 失败: %v\n%s\n", err, output)
			os.Exit(1)
		}

		fmt.Println("✅ 服务卸载成功！")
	},
}

func init() {
	// 添加前台运行参数和配置文件参数到 start 命令
	startCmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "在前台运行服务")
	startCmd.Flags().StringVarP(&configFile, "config", "c", "./config.yml", "配置文件路径 (必填)")
	startCmd.MarkFlagRequired("config")

	// 添加配置文件参数到 migrate 命令
	migrateCmd.Flags().StringVarP(&configFile, "config", "c", "./config.yml", "配置文件路径")

	// Health check command
	healthCheckCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "./config.yml", "配置文件路径 (必填)")
	healthCheckCmd.MarkPersistentFlagRequired("config")

	// Service commands
	installCmd.Flags().StringVarP(&configFile, "config", "c", "", "配置文件路径 (默认: /apps/kaitu/config.yml)")
	uninstallCmd.Flags().StringVarP(&configFile, "config", "c", "", "配置文件路径")

	// 添加所有命令到根命令
	rootCmd.AddCommand(userCmd)
	rootCmd.AddCommand(startCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(stopCmd)
	rootCmd.AddCommand(healthCheckCmd)
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
}

func writePidFile(pidFile string) error {
	pid := os.Getpid()
	return os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", pid)), 0644)
}

func readPidFile(pidFile string) (int, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, err
	}
	return pid, nil
}

func isProcessRunning(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

// 前台模式运行服务
func runServerInForeground() {
	log.Infof(context.Background(), "kaitu-center 服务以前台模式启动")

	// 前台模式下直接获取密钥
	key := getOrInitSecretKeyAndCA(true)
	if key == "" {
		log.Fatalf(context.Background(), "未获取到 SECRET_KEY")
	}

	// 设置信号处理
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		s := <-sigChan
		log.Infof(context.Background(), "接收到信号 %v, 服务正在关闭...", s)
		os.Exit(0)
	}()

	// 启动服务
	startServer()
}

// 运行服务并包含崩溃恢复机制 (daemon 模式)
func runServerWithCrashProtection() {
	defer func() {
		if r := recover(); r != nil {
			log.Infof(context.Background(), "服务崩溃, 错误: %v", r)
			// 在这里可以添加重启逻辑或通知
		}
	}()

	// daemon 模式下不显示终端输出
	key := getOrInitSecretKeyAndCA(false)
	if key == "" {
		log.Fatalf(context.Background(), "未获取到 SECRET_KEY")
	} else {
		log.Infof(context.Background(), "CA 已加载。")
	}

	// 启动服务
	startServer()
}

// 共用的服务启动函数
func startServer() {
	ctx := context.Background()

	// Task system is ready - execute tasks via center.RunTasks() or admin APIs
	log.Infof(ctx, "Task system ready")

	// 启动 HTTP 服务
	r := center.SetupRouter()
	serverConfig := center.ConfigServer(ctx)
	port := strconv.Itoa(serverConfig.Port)

	log.Infof(ctx, "服务在端口 %s 启动 HTTP 服务", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf(ctx, "HTTP 服务启动失败: %v", err)
	}
}

// 只在主进程交互获取密钥
func getOrInitSecretKeyAndCA(terminalEnabled bool) string {
	var key string
	var err error
	var secretKey string

	// 1. 优先从配置文件读取
	configSecretKey := viper.GetString("secret_key")
	if configSecretKey != "" {
		if terminalEnabled {
			fmt.Printf("✅ 已从配置文件获取密钥\n")
		}
		os.Setenv("SECRET_KEY", configSecretKey)
		key = configSecretKey
		// 跳转到数据库初始化逻辑
		goto initDatabase
	}

	// 2. 尝试从 AWS SSM Parameter Store 获取
	secretKey, err = ssm.GetParameter(SSMSecretKeyParameter)
	if err == nil && secretKey != "" {
		// SSM 成功，使用该密钥
		if terminalEnabled {
			fmt.Printf("✅ 已从 AWS SSM 获取密钥 (%s)\n", SSMSecretKeyParameter)
		}
		os.Setenv("SECRET_KEY", secretKey)
		key = secretKey
		// 跳转到数据库初始化逻辑
		goto initDatabase
	}

	// SSM 失败，静默忽略错误，继续尝试其他方式
	if terminalEnabled && err != nil {
		fmt.Printf("ℹ️  AWS SSM 不可用 (%v)，将使用其他方式获取密钥\n", err)
	}

	// 3. 尝试从环境变量获取
	key = os.Getenv("SECRET_KEY")
	if key != "" {
		if terminalEnabled {
			fmt.Printf("✅ 已从环境变量获取密钥\n")
		}
		goto initDatabase
	}

	// 所有方式都失败，报错退出
	fmt.Println("❌ 错误: 未找到 SECRET_KEY")
	fmt.Println("请通过以下任一方式配置密钥:")
	fmt.Println("  1. 配置文件: 在 config.yml 中设置 secret_key")
	fmt.Printf("  2. AWS SSM: 在 SSM Parameter Store 中设置 %s (需配置 aws.* 凭证)\n", SSMSecretKeyParameter)
	fmt.Println("  3. 环境变量: 设置 SECRET_KEY 环境变量")
	os.Exit(1)

initDatabase:
	// 设置密钥哈希
	hash := md5.Sum([]byte(key))
	hashStr := hex.EncodeToString(hash[:])
	os.Setenv("SECRET_KEY_HASH", hashStr)

	// 初始化数据库连接（触发 qtoolkit lazy loading）
	// 必须在访问 CA 之前完成，因为 GetCa() 需要查询数据库
	ctx := context.Background()
	if err := center.Migrate(); err != nil {
		fmt.Printf("数据库初始化失败: %v\n", err)
		os.Exit(1)
	}

	// 检查/加载CA
	_, _, err = center.GetCa(ctx)
	if err != nil {
		if err == center.ErrCaNotFound {
			fmt.Println("未检测到CA密钥对，自动生成...")
			certPEM, keyPEM, err := center.GenerateCA(context.Background())
			if err != nil {
				fmt.Println("生成CA失败：", err)
				os.Exit(1)
			}
			if err := center.SetCa(context.Background(), certPEM, keyPEM); err != nil {
				fmt.Println("保存CA失败：", err)
				os.Exit(1)
			}
			fmt.Println("CA 已生成并保存。")
		} else if err == center.ErrCaPassword {
			fmt.Println("CA 密码错误，请检查 SECRET_KEY/输入的密码是否正确！")
			os.Exit(1)
		} else {
			fmt.Println("加载CA失败：", err)
			os.Exit(1)
		}
	} else {
		fmt.Println("CA 已加载。")
	}
	return key
}
