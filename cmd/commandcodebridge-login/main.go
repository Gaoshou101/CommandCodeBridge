package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"

	"commandcodebridge/internal/loginhelper"
)

func main() {
	noOpen := flag.Bool("no-open", false, "do not open the local receiver in a browser")
	flag.Parse()
	listener, err := net.Listen("tcp4", loginhelper.Address)
	if err != nil {
		fmt.Println("本地认证接收器无法启动，127.0.0.1:45879 已被占用。关闭已有接收器后重试。")
		os.Exit(1)
	}
	fmt.Println("CommandCode 本地认证接收器：" + loginhelper.LocalURL)
	fmt.Println("请在打开的页面粘贴 CPAMP 官方授权链接。认证成功后关闭此程序；程序最多运行 15 分钟。")
	server := &http.Server{Handler: loginhelper.New(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	time.AfterFunc(15*time.Minute, func() { _ = server.Close() })
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", loginhelper.LocalURL)
	case "darwin":
		command = exec.Command("open", loginhelper.LocalURL)
	default:
		command = exec.Command("xdg-open", loginhelper.LocalURL)
	}
	if !*noOpen && command.Start() == nil {
		go func() { _ = command.Wait() }()
	}
	_ = server.Serve(listener)
}
