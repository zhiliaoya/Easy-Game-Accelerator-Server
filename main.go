package main

import (
	"bufio"
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
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
	"golang.org/x/crypto/curve25519"
	_ "modernc.org/sqlite"
)

// ===== 编译期注入（仅作为 build.ini 缺失时的兜底默认值）=====
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
	dataDir   = "zhiliaoya/mydata"
	yamlDir   = "zhiliaoya/yaml"
	dbFile    = "xray.db"
	iniFile   = "build.ini"
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

// ===== 配置结构（对应 build.ini / config 表）=====

type BuildConfig struct {
	Users      int
	Port       int
	Mode       string // ip | domain
	Domain     string
	Storage    string // none | sql
	ServerName string
	ProxyName  string

	APIEnabled bool
	APIListen  string
	APIPort    string
}

func defaultBuildConfig() BuildConfig {
	users, err := strconv.Atoi(cfgUsers)
	if err != nil || users < 1 {
		users = 1
	}
	if users > 3 {
		users = 3
	}
	port, err := strconv.Atoi(cfgPort)
	if err != nil || port < 1 || port > 65535 {
		port = 8206
	}
	apiListen := cfgAPIListen
	if apiListen == "" {
		apiListen = "127.0.0.1"
	}
	apiPort := cfgAPIPort
	if apiPort == "" {
		apiPort = "8207"
	}
	return BuildConfig{
		Users:      users,
		Port:       port,
		Mode:       strings.ToLower(cfgMode),
		Domain:     cfgDomain,
		Storage:    strings.ToLower(cfgStorage),
		ServerName: cfgServerName,
		ProxyName:  cfgProxyName,
		APIEnabled: strings.EqualFold(cfgAPIEnabled, "true"),
		APIListen:  apiListen,
		APIPort:    apiPort,
	}
}

// loadBuildIni 解析 build.ini；文件不存在返回 (nil, nil)
func loadBuildIni(path string) (*BuildConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	kv := make(map[string]string)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			continue // 忽略 section 行
		}
		idx := strings.IndexAny(line, "=:")
		if idx < 0 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(line[:idx]))
		v := strings.TrimSpace(line[idx+1:])
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		kv[k] = v
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	cfg := defaultBuildConfig()
	if v, ok := kv["users"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			if n > 3 {
				n = 3
			}
			cfg.Users = n
		}
	}
	if v, ok := kv["port"]; ok {
		if p, err := strconv.Atoi(v); err == nil && p >= 1 && p <= 65535 {
			cfg.Port = p
		}
	}
	if v, ok := kv["mode"]; ok && v != "" {
		cfg.Mode = strings.ToLower(v)
	}
	if v, ok := kv["domain"]; ok {
		cfg.Domain = v
	}
	if v, ok := kv["storage"]; ok && v != "" {
		cfg.Storage = strings.ToLower(v)
	}
	if v, ok := kv["server_name"]; ok && v != "" {
		cfg.ServerName = v
	}
	if v, ok := kv["proxy_name"]; ok && v != "" {
		cfg.ProxyName = v
	}
	if v, ok := kv["api_enabled"]; ok {
		cfg.APIEnabled = strings.EqualFold(v, "true")
	}
	if v, ok := kv["api_listen"]; ok && v != "" {
		cfg.APIListen = v
	}
	if v, ok := kv["api_port"]; ok && v != "" {
		cfg.APIPort = v
	}
	return &cfg, nil
}

// ===== DB config 表 =====

func ensureConfigTable(db *sql.DB) error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS config (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		created_at TEXT NOT NULL
	);`)
	return err
}

func saveConfig(db *sql.DB, c BuildConfig) error {
	now := time.Now().Format(time.RFC3339)
	items := map[string]string{
		"users":       strconv.Itoa(c.Users),
		"port":        strconv.Itoa(c.Port),
		"mode":        c.Mode,
		"domain":      c.Domain,
		"storage":     c.Storage,
		"server_name": c.ServerName,
		"proxy_name":  c.ProxyName,
		"api_enabled": strconv.FormatBool(c.APIEnabled),
		"api_listen":  c.APIListen,
		"api_port":    c.APIPort,
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO config (key, value, created_at) VALUES (?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for k, v := range items {
		if _, err := stmt.Exec(k, v, now); err != nil {
			return fmt.Errorf("写入 config[%s] 失败: %w", k, err)
		}
	}
	return tx.Commit()
}

func loadConfig(db *sql.DB) (*BuildConfig, error) {
	if err := ensureConfigTable(db); err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT key, value FROM config`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	kv := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		kv[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(kv) == 0 {
		return nil, nil
	}

	cfg := defaultBuildConfig()
	if v, ok := kv["users"]; ok {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			if n > 3 {
				n = 3
			}
			cfg.Users = n
		}
	}
	if v, ok := kv["port"]; ok {
		if p, err := strconv.Atoi(v); err == nil && p >= 1 && p <= 65535 {
			cfg.Port = p
		}
	}
	if v, ok := kv["mode"]; ok {
		cfg.Mode = v
	}
	if v, ok := kv["domain"]; ok {
		cfg.Domain = v
	}
	if v, ok := kv["storage"]; ok {
		cfg.Storage = v
	}
	if v, ok := kv["server_name"]; ok {
		cfg.ServerName = v
	}
	if v, ok := kv["proxy_name"]; ok {
		cfg.ProxyName = v
	}
	if v, ok := kv["api_enabled"]; ok {
		cfg.APIEnabled = strings.EqualFold(v, "true")
	}
	if v, ok := kv["api_listen"]; ok {
		cfg.APIListen = v
	}
	if v, ok := kv["api_port"]; ok {
		cfg.APIPort = v
	}
	return &cfg, nil
}

// resolveConfig 决定本次运行使用哪套配置
//   1. 若 storage=sql 且 config 表非空 → 用 DB 中的配置
//   2. 否则读 build.ini（缺失则用编译期默认值）；storage=sql 时写入 DB
// 返回 (cfg, db, fromIni, err)；db 非 nil 由调用方关闭。
func resolveConfig() (BuildConfig, *sql.DB, bool, error) {
	ini, err := loadBuildIni(iniFile)
	if err != nil {
		return BuildConfig{}, nil, false, fmt.Errorf("解析 %s 失败: %w", iniFile, err)
	}

	storage := strings.ToLower(cfgStorage)
	if ini != nil && ini.Storage != "" {
		storage = ini.Storage
	}

	if storage == "none" {
		if ini != nil {
			return *ini, nil, true, nil
		}
		return defaultBuildConfig(), nil, true, nil
	}
	if storage != "sql" {
		return BuildConfig{}, nil, false, fmt.Errorf("未知的 storage=%q（应为 none 或 sql）", storage)
	}

	db, err := openDB()
	if err != nil {
		return BuildConfig{}, nil, false, fmt.Errorf("打开数据库失败: %w", err)
	}

	stored, err := loadConfig(db)
	if err != nil {
		db.Close()
		return BuildConfig{}, nil, false, fmt.Errorf("读取 DB 配置失败: %w", err)
	}

	if stored != nil {
		fmt.Println("[INFO] 检测到已有 SQL 配置，复用数据库中的配置")
		return *stored, db, false, nil
	}

	cfg := defaultBuildConfig()
	if ini != nil {
		cfg = *ini
	}
	cfg.Storage = "sql"

	if err := saveConfig(db, cfg); err != nil {
		db.Close()
		return BuildConfig{}, nil, false, fmt.Errorf("写入 DB 配置失败: %w", err)
	}
	fmt.Println("[INFO] 首次运行：已从 build.ini 读取配置并写入 SQL 配置表")
	return cfg, db, true, nil
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

func resolveServerAddrWith(mode, domain string) (string, error) {
	if strings.ToLower(mode) == "domain" {
		if domain == "" {
			return "", fmt.Errorf("domain 模式但 domain 为空")
		}
		return domain, nil
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
	if err := ensureConfigTable(db); err != nil {
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

func newUserRecord(idx int, serverName, proxyName string, sharedKey *UserRecord) (UserRecord, error) {
	var privateKey, publicKey string
	if sharedKey != nil {
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

// buildPersistentUsersWithDB 使用已打开的 db，不负责关闭
func buildPersistentUsersWithDB(db *sql.DB, n int, serverName, proxyName string) ([]UserRecord, error) {
	if err := ensureConfigTable(db); err != nil {
		return nil, fmt.Errorf("初始化 config 表失败: %w", err)
	}
	users, err := loadUsers(db)
	if err != nil {
		return nil, fmt.Errorf("读取用户失败: %w", err)
	}

	// 空库：生成 n 个
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

// 保留旧签名（内部开/关 db），供可能的外部调用；main 已改用 WithDB 版本
func buildPersistentUsers(n int, serverName, proxyName string) ([]UserRecord, error) {
	db, err := openDB()
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	defer db.Close()
	return buildPersistentUsersWithDB(db, n, serverName, proxyName)
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

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

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
	// 1) 决定配置来源：DB 优先，否则 build.ini，否则编译期默认
	cfg, db, fromIni, err := resolveConfig()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	if db != nil {
		defer db.Close()
	}

    

	n := cfg.Users
	listenPort := cfg.Port

	fmt.Printf("运行模式: %d 人，连接=%s，存储=%s，端口=%d (配置来源: ini=%v)\n",
		n, cfg.Mode, cfg.Storage, listenPort, fromIni)

	// 2) 解析连接地址
	serverAddr, err := resolveServerAddrWith(cfg.Mode, cfg.Domain)
	if err != nil {
		log.Fatalf("解析连接地址失败: %v", err)
	}
	fmt.Println("连接地址 :", serverAddr)

	// 3) 先起 API：liveness 启动阶段即可用，readiness 自然为 503
	if cfg.APIEnabled {
		startAPIServer(cfg.APIListen, cfg.APIPort)
	}

	// 4) 构造用户
	var users []UserRecord

	switch cfg.Storage {
	case "none":
		users, err = buildVolatileUsers(n, cfg.ServerName, cfg.ProxyName)
		if err != nil {
			log.Fatalf("生成用户失败: %v", err)
		}
		for i := range users {
			users[i].ServerAddr = serverAddr
			users[i].Port = listenPort
		}
		fmt.Println("[INFO] 无存储模式：配置仅本次运行有效，重启即失效")
	case "sql":
		users, err = buildPersistentUsersWithDB(db, n, cfg.ServerName, cfg.ProxyName)
		if err != nil {
			log.Fatalf("初始化用户失败: %v", err)
		}
		sharedPublicKey := users[0].PublicKey
		for i := range users {
			users[i].ServerAddr = serverAddr
			users[i].Port = listenPort
			if _, err := writeYAML(users[i], sharedPublicKey); err != nil {
				log.Fatalf("写入 YAML 失败: %v", err)
			}
		}
	default:
		log.Fatalf("未知的 storage=%q（应为 none 或 sql）", cfg.Storage)
	}

	// 打印
	fmt.Println("REALITY 共享 PublicKey:", users[0].PublicKey)
	fmt.Println("各用户 shortID 独立，已全部注册到服务端 shortIds")
	fmt.Println()

	for _, u := range users {
		printUserInfo(u)
		fmt.Println()
	}

	// 5) 启动 Xray
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

	// 6) 优雅退出
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("收到退出信号，正在关闭...")
	appState.MarkXrayDown(nil)
	_ = server.Close()
}
