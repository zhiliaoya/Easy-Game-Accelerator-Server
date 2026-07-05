package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/curve25519"
	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"
)

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

func main() {
	// 参数说明：
	// 1. serverName —— 伪装域名（REALITY SNI），如 www.bing.com
	// 2. proxyName  —— 节点显示名称，如 Game-VLESS
	serverName := "www.bing.com"
	if len(os.Args) > 1 {
		serverName = os.Args[1]
	}
	proxyName := "Game-VLESS"
	if len(os.Args) > 2 {
		proxyName = os.Args[2]
	}

	// 自动获取本机公网 IP，作为客户端连接地址
	proxyServerAddr, err := getPublicIP()
	if err != nil {
		log.Fatalf("获取公网IP失败: %v", err)
	}

	privateKey, publicKey, err := generateRealityKeyPair()
	if err != nil {
		log.Fatalf("生成密钥对失败: %v", err)
	}
	shortID, err := generateShortID()
	if err != nil {
		log.Fatalf("生成 ShortID 失败: %v", err)
	}
	userUUID := uuid.New().String()
	const listenPort = 443

	fmt.Println("========== 客户端连接信息 ==========")
	fmt.Println("公网IP    :", proxyServerAddr)
	fmt.Println("UUID      :", userUUID)
	fmt.Println("PublicKey :", publicKey)
	fmt.Println("ServerName:", serverName)
	fmt.Println("ShortID   :", shortID)
	fmt.Println("Port      :", listenPort)
	fmt.Println("Flow      : xtls-rprx-vision")
	fmt.Println("=====================================")

	// ---- 输出 Clash/Mihomo 风格的 YAML 配置（仅打印，不落盘） ----
	yamlConfig := fmt.Sprintf(`proxies:
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
`, proxyName, proxyServerAddr, listenPort, userUUID, serverName, publicKey, shortID)

	fmt.Println("========== Clash/Mihomo YAML 配置 ==========")
	fmt.Println(yamlConfig)
	fmt.Println("=============================================")

	// ---- 启动 xray-core 服务端 ----
	configJSON := fmt.Sprintf(`{
		"log": { "loglevel": "warning" },
		"inbounds": [
			{
				"listen": "0.0.0.0",
				"port": %d,
				"protocol": "vless",
				"settings": {
					"clients": [
						{ "id": "%s", "flow": "xtls-rprx-vision" }
					],
					"decryption": "none"
				},
				"streamSettings": {
					"network": "tcp",
					"security": "reality",
					"realitySettings": {
						"dest": "%s:443",
						"serverNames": ["%s"],
						"privateKey": "%s",
						"shortIds": ["%s"]
					}
				}
			}
		],
		"outbounds": [
			{ "protocol": "freedom", "tag": "direct" }
		]
	}`, listenPort, userUUID, serverName, serverName, privateKey, shortID)

	pbConfig, err := serial.LoadJSONConfig(strings.NewReader(configJSON))
	if err != nil {
		log.Fatalf("解析配置失败: %v", err)
	}
	server, err := core.New(pbConfig)
	if err != nil {
		log.Fatalf("创建实例失败: %v", err)
	}
	if err := server.Start(); err != nil {
		log.Fatalf("启动失败: %v", err)
	}

	fmt.Println("Xray (VLESS+REALITY) 已启动，监听端口", listenPort)
	select {}
}