package main

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/curve25519"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
	_ "modernc.org/sqlite"
)

// ===== 编译期注入 =====
var (
	cfgUsers      = "1"
	cfgMode       = "ip"
	cfgDomain     = ""
	cfgStorage    = "sql"
	cfgServerName = "www.bing.com"
	cfgProxyName  = "Game-VLESS"
	cfgPort       = "8206"

	// 健康检查 API
	cfgAPIEnabled = "true"
	cfgAPIListen  = "127.0.0.1"
	cfgAPIPort    = "8207"
)

const (
	dataDir = "zhiliaoya/mydata"
	yamlDir = "zhiliaoya/yaml"
	dbFile  = "xray.db"
)

type UserRecord struct {
	ID         int
	UUID       string
	PrivateKey string
	PublicKey  string
	ShortID    string
	ServerName string
	ProxyName  string
	ServerAddr string
	Port       int
	CreatedAt  string
}

// ===== 应用状态（供 API 读取）=====

type AppState struct {
	mu         sync.RWMutex
	xrayUp     atomic.Bool
	startAt    time.Time
	listenPort int
	userCount  int
	lastErr    string
	lastErrAt  time.Time
}

var appState = &AppState{startAt: time.Now()}

func (s *AppState) MarkXrayUp(port, users int) {
	s.xrayUp.Store(true)
	s.mu.Lock()
	s.listenPort = port
	s.userCount = users
	s.lastErr = ""
	s.mu.Unlock()
}

func (s *AppState) MarkXrayDown(err error) {
	s.xrayUp.Store(false)
	s.mu.Lock()
	if err != nil {
		s.lastErr = err.Error()
		s.lastErrAt = time.Now()
	}
	s.mu.Unlock()
}

func (s *AppState) Snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"xray_up":     s.xrayUp.Load(),
		"uptime_sec":  int(time.Since(s.startAt).Seconds()),
		"listen_port": s.listenPort,
		"user_count":  s.userCount,
		"last_error":  s.lastErr,
		"last_err_at": s.lastErrAt.Format(time.RFC3339),
	}
}

// ===== 密钥 / ID =====

func generateRealityKeyPair() (string, string, error) {
	privateKey := make([]byte, curve25519.ScalarSize)
	if _, err := rand.Read(privateKey); err != nil {
		return "", "", err
	}
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	publicKey, err := curve25519.X25519(privateKey, curve25519.Basepoint)
	if err != nil {
		return "", "", err
	}
	return base64.RawURLEncoding.EncodeToString(privateKey),
		base64.RawURLEncoding.EncodeToString(publicKey), nil
}

func generateShortID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ===== 连接地址解析 =====

func getPublicIP() (string, error) {
	client := http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	ip, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ip)), nil
}

func resolveServerAddr() (string, error) {
	if strings.ToLower(cfgMode) == "domain" {
		if cfgDomain == "" {
			return "", fmt.Errorf("domain 模式但 cfgDomain 为空")
		}
		return cfgDomain, nil
	}
	return getPublicIP()
}

// ===== 目录 / 数据库 =====

func ensureDirs() error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return err
	}
	return os.MkdirAll(yamlDir, 0755)
}

func dbPath() string {
	return filepath.Join(dataDir, dbFile)
}

func openDB() (*sql.DB, error) {
	if err := ensureDirs(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dbPath())
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA busy_timeout = 5000")
	db.Exec("PRAGMA journal_mode = WAL")

	createSQL := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY,
		uuid TEXT NOT NULL UNIQUE,
		private_key TEXT NOT NULL,
		public_key TEXT NOT NULL,
		short_id TEXT NOT NULL,
		server_name TEXT NOT NULL,
		proxy_name TEXT NOT NULL,
		created_at TEXT NOT NULL
	);`
	if _, err := db.Exec(createSQL); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func loadUsers(db *sql.DB) ([]UserRecord, error) {
	rows, err := db.Query(`SELECT id, uuid, private_key, public_key, short_id, server_name, proxy_name, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []UserRecord
	for rows.Next() {
		var u UserRecord
		if err := rows.Scan(&u.ID, &u.UUID, &u.PrivateKey, &u.PublicKey, &u.ShortID, &u.ServerName, &u.ProxyName, &u.CreatedAt); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func insertUser(db *sql.DB, u UserRecord) error {
	_, err := db.Exec(
		`INSERT INTO users (id, uuid, private_key, public_key, short_id, server_name, proxy_name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		u.ID, u.UUID, u.PrivateKey, u.PublicKey, u.ShortID, u.ServerName, u.ProxyName, u.CreatedAt,
	)
	return err
}

// ===== 用户构造 =====

// newUserRecord 生成一个用户。
// sharedKey 非空时，复用其 privateKey/publicKey（方案 B：所有用户共享同一对 REALITY 密钥），
// 只生成新的 UUID 和 shortID；sharedKey 为空时生成全新密钥对（第一个用户）。
func newUserRecord(idx int, serverName, proxyName string, sharedKey *UserRecord) (UserRecord, error) {
	var privateKey, publicKey string
	if sharedKey != nil {
		// 方案 B：共享 REALITY 密钥对
		privateKey = sharedKey.PrivateKey
		publicKey = sharedKey.PublicKey
	} else {
		var err error
		privateKey, publicKey, err = generateRealityKeyPair()
		if err != nil {
			return UserRecord{}, fmt.Errorf("生成密钥对失败: %w", err)
		}
	}

	shortID, err := generateShortID()
	if err != nil {
		return UserRecord{}, fmt.Errorf("生成 ShortID 失败: %w", err)
	}

	return UserRecord{
		ID:         idx,
		UUID:       uuid.New().String(),
		PrivateKey: privateKey,
		PublicKey:  publicKey,
		ShortID:    shortID,
		ServerName: serverName,
		ProxyName:  fmt.Sprintf("%s-%d", proxyName, idx),
		CreatedAt:  time.Now().Format(time.RFC3339),
	}, nil
}

// ===== YAML =====

func buildYAML(u UserRecord, sharedPublicKey string) string {
	return fmt.Sprintf(`proxies:
  - name: "%s"
    type: vless
    server: %s
    port: %d
    uuid: %s
    flow: xtls-rprx-vision
    encryption: none
    tls: true
    servername: %s
    reality-opts:
      public-key: %s
      short-id: %s
    client-fingerprint: firefox
`, u.ProxyName, u.ServerAddr, u.Port, u.UUID, u.ServerName, sharedPublicKey, u.ShortID)
}

func writeYAML(u UserRecord, sharedPublicKey string) (string, error) {
	if err := ensureDirs(); err != nil {
		return "", err
	}
	fname := filepath.Join(yamlDir, fmt.Sprintf("client-%d.yaml", u.ID))
	content := buildYAML(u, sharedPublicKey)
	if err := os.WriteFile(fname, []byte(content), 0644); err != nil {
		return "", err
	}
	return fname, nil
}

// ===== 无存储模式 =====

func buildVolatileUsers(n int, serverName, proxyName string) ([]UserRecord, error) {
	users := make([]UserRecord, 0, n)
	var first *UserRecord
	for i := 1; i <= n; i++ {
		u, err := newUserRecord(i, serverName, proxyName, first)
		if err != nil {
			return nil, err
		}
		if first == nil {
			f := u
			first = &f
		}
		users = append(users, u)
	}
	return users, nil
}

// ===== SQL 存储模式 =====

func buildPersistentUsers(n int, serverName, proxyName string) ([]UserRecord, error) {
	db, err := openDB()
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	defer db.Close()

	users, err := loadUsers(db)
	if err != nil {
		return nil, fmt.Errorf("读取用户失败: %w", err)
	}

	// 空库：生成 n 个，第一个生成新密钥对，后续共享
	if len(users) == 0 {
		fmt.Println("[INFO] 未发现用户配置，开始生成...")
		var first *UserRecord
		for i := 1; i <= n; i++ {
			u, err := newUserRecord(i, serverName, proxyName, first)
			if err != nil {
				return nil, err
			}
			if first == nil {
				f := u
				first = &f
			}
			if err := insertUser(db, u); err != nil {
				return nil, fmt.Errorf("写入用户失败: %w", err)
			}
		}
		return loadUsers(db)
	}

	// 扩容：复用已有第一个用户的密钥对
	if len(users) < n {
		fmt.Printf("[INFO] 已有 %d 个用户，扩容到 %d 个...\n", len(users), n)
		base := users[0]
		for i := len(users) + 1; i <= n; i++ {
			u, err := newUserRecord(i, serverName, proxyName, &base)
			if err != nil {
				return nil, err
			}
			if err := insertUser(db, u); err != nil {
				return nil, fmt.Errorf("扩容失败: %w", err)
			}
		}
		return loadUsers(db)
	}

	return users, nil
}

// ===== 打印 / Xray 配置 =====

func printUserInfo(u UserRecord) {
	fmt.Println("========== 客户端连接信息 ==========")
	fmt.Println("用户ID    :", u.ID)
	fmt.Println("节点名称  :", u.ProxyName)
	fmt.Println("连接地址  :", u.ServerAddr)
	fmt.Println("端口      :", u.Port)
	fmt.Println("UUID      :", u.UUID)
	fmt.Println("PublicKey :", u.PublicKey)
	fmt.Println("ServerName:", u.ServerName)
	fmt.Println("ShortID   :", u.ShortID)
	fmt.Println("Flow      : xtls-rprx-vision")
	fmt.Println("=====================================")
}

func buildXrayConfig(users []UserRecord, listenPort int) string {
	var clients []string
	var shortIDs []string
	for _, u := range users {
		clients = append(clients, fmt.Sprintf(`{ "id": "%s", "flow": "xtls-rprx-vision" }`, u.UUID))
		shortIDs = append(shortIDs, fmt.Sprintf(`"%s"`, u.ShortID))
	}

	// REALITY 的 privateKey / dest / serverNames 是入站级别的，
	// 只能取第一个用户的（所有用户共享同一对密钥）；
	// shortIds 是列表，把所有用户的 shortID 都放进去。
	first := users[0]

	return fmt.Sprintf(`{
		"log": { "loglevel": "warning" },
		"inbounds": [
			{
				"listen": "0.0.0.0",
				"port": %d,
				"protocol": "vless",
				"settings": {
					"clients": [%s],
					"decryption": "none"
				},
				"streamSettings": {
					"network": "tcp",
					"security": "reality",
					"realitySettings": {
						"dest": "%s:443",
						"serverNames": ["%s"],
						"privateKey": "%s",
						"shortIds": [%s]
					}
				}
			}
		],
		"outbounds": [
			{ "protocol": "freedom", "tag": "direct" }
		]
	}`, listenPort, strings.Join(clients, ","),
		first.ServerName, first.ServerName, first.PrivateKey,
		strings.Join(shortIDs, ","))
}

// ===== 健康检查 API =====

func startAPIServer(listen, port string) {
	mux := http.NewServeMux()

	// 1) 进程存活：能进 handler 就 200，不碰 Xray
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	// 2) Xray 就绪：读状态，不主动探测
	mux.HandleFunc("/hxray", func(w http.ResponseWriter, r *http.Request) {
		snap := appState.Snapshot()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if !appState.xrayUp.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			snap["ok"] = false
			snap["error"] = "xray not ready"
			_ = json.NewEncoder(w).Encode(snap)
			return
		}
		snap["ok"] = true
		_ = json.NewEncoder(w).Encode(snap)
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%s", listen, port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("[API] 管理接口监听 %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[API] 服务异常退出: %v", err)
		}
	}()
}

// ===== main =====

func main() {
	// 解析用户数
	n, err := strconv.Atoi(cfgUsers)
	if err != nil || n < 1 {
		n = 1
	}
	if n > 3 {
		n = 3
	}

	// 解析端口
	listenPort, err := strconv.Atoi(cfgPort)
	if err != nil || listenPort < 1 || listenPort > 65535 {
		listenPort = 8206
	}

	fmt.Printf("运行模式: %d 人，连接=%s，存储=%s，端口=%d\n", n, cfgMode, cfgStorage, listenPort)

	serverAddr, err := resolveServerAddr()
	if err != nil {
		log.Fatalf("解析连接地址失败: %v", err)
	}
	fmt.Println("连接地址 :", serverAddr)

	// 先起 API：liveness 在启动阶段即可用，readiness 此时自然为 503
	if strings.EqualFold(cfgAPIEnabled, "true") {
		startAPIServer(cfgAPIListen, cfgAPIPort)
	}

	var users []UserRecord

	switch strings.ToLower(cfgStorage) {
	case "none":
		users, err = buildVolatileUsers(n, cfgServerName, cfgProxyName)
		if err != nil {
			log.Fatalf("生成用户失败: %v", err)
		}
		for i := range users {
			users[i].ServerAddr = serverAddr
			users[i].Port = listenPort
		}
		fmt.Println("[INFO] 无存储模式：配置仅本次运行有效，重启即失效")
	case "sql", "":
		users, err = buildPersistentUsers(n, cfgServerName, cfgProxyName)
		if err != nil {
			log.Fatalf("初始化用户失败: %v", err)
		}
		// 共享的 publicKey 取第一个用户的（方案 B：服务端只用第一对密钥）
		sharedPublicKey := users[0].PublicKey
		for i := range users {
			users[i].ServerAddr = serverAddr
			users[i].Port = listenPort
			if _, err := writeYAML(users[i], sharedPublicKey); err != nil {
				log.Fatalf("写入 YAML 失败: %v", err)
			}
		}
	default:
		log.Fatalf("未知的 storage=%q（应为 none 或 sql）", cfgStorage)
	}

	// 打印：明确区分共享 publicKey 与各用户独立 shortID
	fmt.Println("REALITY 共享 PublicKey:", users[0].PublicKey)
	fmt.Println("各用户 shortID 独立，已全部注册到服务端 shortIds")
	fmt.Println()

	for _, u := range users {
		printUserInfo(u)
		fmt.Println()
	}

	// 启动 Xray
	configJSON := buildXrayConfig(users, listenPort)
	pbConfig, err := serial.LoadJSONConfig(strings.NewReader(configJSON))
	if err != nil {
		appState.MarkXrayDown(err)
		log.Fatalf("解析配置失败: %v", err)
	}
	server, err := core.New(pbConfig)
	if err != nil {
		appState.MarkXrayDown(err)
		log.Fatalf("创建实例失败: %v", err)
	}
	if err := server.Start(); err != nil {
		appState.MarkXrayDown(err)
		log.Fatalf("启动失败: %v", err)
	}

	appState.MarkXrayUp(listenPort, len(users))
	fmt.Println("Xray (VLESS+REALITY) 已启动，监听端口", listenPort)

	// 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("收到退出信号，正在关闭...")
	appState.MarkXrayDown(nil)
	_ = server.Close()
}