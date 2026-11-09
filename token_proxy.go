//go:build windows

package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/elazarl/goproxy"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	tokenProxyAddr  = "127.0.0.1:18899"
	inetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	caCommonName    = "ParkingMonitor Local CA"
)

type TokenProxy struct {
	mu      sync.Mutex
	server  *http.Server
	timer   *time.Timer
	running bool

	prevEnable uint32
	prevServer string

	onStop   func()
	stopOnce sync.Once
}

func (p *TokenProxy) Start(onToken func(string), onLog func(string), onStop func()) error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return errors.New("代理已在运行")
	}
	p.mu.Unlock()

	ca, certPath, err := loadOrCreateCA()
	if err != nil {
		return err
	}
	if err := installCA(certPath); err != nil {
		return err
	}

	proxy := goproxy.NewProxyHttpServer()
	proxy.Verbose = false
	hostCond := goproxy.ReqHostMatches(regexp.MustCompile(`^gaoxin\.zimoiot\.com:443$`))
	connectAction := &goproxy.ConnectAction{
		Action:    goproxy.ConnectMitm,
		TLSConfig: goproxy.TLSConfigFromCA(&ca),
	}
	proxy.OnRequest(hostCond).HandleConnect(goproxy.FuncHttpsHandler(
		func(host string, ctx *goproxy.ProxyCtx) (*goproxy.ConnectAction, string) {
			return connectAction, host
		}))

	var once sync.Once
	proxy.OnRequest().DoFunc(func(req *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		if req.URL != nil && req.URL.Hostname() == "gaoxin.zimoiot.com" {
			if token := req.Header.Get("Token"); token != "" {
				once.Do(func() {
					if onLog != nil {
						onLog("已从微信小程序请求中捕获 Token")
					}
					if onToken != nil {
						go onToken(token)
					}
				})
			}
		}
		return req, nil
	})

	ln, err := net.Listen("tcp", tokenProxyAddr)
	if err != nil {
		return fmt.Errorf("本地代理端口被占用（%s）: %w", tokenProxyAddr, err)
	}
	srv := &http.Server{Handler: proxy}
	go srv.Serve(ln)

	prevEnable, prevServer, err := setSystemProxy(tokenProxyAddr)
	if err != nil {
		srv.Close()
		return fmt.Errorf("设置系统代理失败: %w", err)
	}

	p.mu.Lock()
	p.running = true
	p.server = srv
	p.prevEnable = prevEnable
	p.prevServer = prevServer
	p.onStop = onStop
	p.stopOnce = sync.Once{}
	p.timer = time.AfterFunc(3*time.Minute, func() {
		if onLog != nil {
			onLog("自动获取 Token 超时（3 分钟），已停止代理并恢复网络设置")
		}
		p.Stop()
	})
	p.mu.Unlock()

	if onLog != nil {
		onLog("本地代理已启动，系统代理已临时指向 " + tokenProxyAddr)
	}
	return nil
}

func (p *TokenProxy) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

func (p *TokenProxy) Stop() {
	p.mu.Lock()
	srv := p.server
	wasRunning := p.running
	prevEnable := p.prevEnable
	prevServer := p.prevServer
	timer := p.timer
	onStop := p.onStop
	p.running = false
	p.server = nil
	p.timer = nil
	p.mu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	if srv != nil {
		srv.Close()
	}
	if wasRunning {
		restoreSystemProxy(prevEnable, prevServer)
	}
	p.stopOnce.Do(func() {
		if onStop != nil {
			onStop()
		}
	})
}

func caPaths() (certPath, keyPath string, err error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(base, "ParkingMonitor", "ca")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key"), nil
}

func loadOrCreateCA() (tls.Certificate, string, error) {
	certPath, keyPath, err := caPaths()
	if err != nil {
		return tls.Certificate{}, "", err
	}
	if _, err := os.Stat(certPath); err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			cert, err := tls.LoadX509KeyPair(certPath, keyPath)
			if err == nil {
				return cert, certPath, nil
			}
		}
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: caCommonName, Organization: []string{"ParkingMonitor"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, "", err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, "", err
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	return cert, certPath, err
}

func installCA(certPath string) error {
	cmd := exec.Command("certutil", "-addstore", "-user", "Root", certPath)
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := string(out)
	lower := strings.ToLower(msg)
	if strings.Contains(msg, "已存在") || strings.Contains(lower, "already") || strings.Contains(lower, "exists") {
		return nil
	}
	return fmt.Errorf("安装根证书失败（请在 Windows 安全提示中点“是”后重试）: %s", strings.TrimSpace(msg))
}

func setSystemProxy(addr string) (uint32, string, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return 0, "", err
	}
	defer key.Close()

	prevEnable, _, _ := key.GetIntegerValue("ProxyEnable")
	prevServer, _, _ := key.GetStringValue("ProxyServer")

	if err := key.SetDWordValue("ProxyEnable", 1); err != nil {
		return 0, "", err
	}
	if err := key.SetStringValue("ProxyServer", addr); err != nil {
		return 0, "", err
	}
	refreshWinINET()
	return uint32(prevEnable), prevServer, nil
}

func restoreSystemProxy(enable uint32, server string) {
	key, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return
	}
	defer key.Close()
	_ = key.SetDWordValue("ProxyEnable", enable)
	if server != "" {
		_ = key.SetStringValue("ProxyServer", server)
	}
	refreshWinINET()
}

var (
	wininetDLL            = windows.NewLazySystemDLL("wininet.dll")
	procInternetSetOption = wininetDLL.NewProc("InternetSetOptionW")
)

func refreshWinINET() {
	procInternetSetOption.Call(0, 39, 0, 0)
	procInternetSetOption.Call(0, 37, 0, 0)
}
