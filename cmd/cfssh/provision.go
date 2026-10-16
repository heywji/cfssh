package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"cfssh/internal/clientcfg"
	"cfssh/internal/config"
	"cfssh/internal/orchestrator"
	"cfssh/internal/provision"
)

// runProvision is the operator-side command. With the Cloudflare API token it
// creates the tunnel, DNS record and Access service token for one machine and
// writes a sidecar file. The operator ships that file next to cfssh; the customer
// then only types their own login password. The API token never leaves here.
func runProvision(args []string) {
	fs := flag.NewFlagSet("provision", flag.ExitOnError)
	token := fs.String("token", "", "Cloudflare API 令牌（自定义 Token，不是 Global API Key）")
	list := fs.Bool("list", false, "只列出该令牌能看到的账户和域名，然后退出")
	account := fs.String("account", "", "账户 ID（-list 里能看到）")
	zoneID := fs.String("zone-id", "", "域名 Zone ID（留空则用 -zone 按名称查找）")
	zoneName := fs.String("zone", "", "域名，例如 example.com")
	subdomain := fs.String("subdomain", "ssh", "主机名前缀，公网地址为 <前缀>.<域名>")
	tunnelName := fs.String("tunnel-name", "cfssh", "Cloudflare 隧道名（每台机器建议用不同的名字）")
	port := fs.Int("port", 2222, "本机 SSH 监听端口")
	localhost := fs.Bool("localhost", true, "只监听 127.0.0.1（推荐）")
	out := fs.String("out", provision.Name, "输出的 sidecar 文件路径")
	_ = fs.Parse(args)

	if strings.TrimSpace(*token) == "" {
		fmt.Fprintln(os.Stderr, "需要 -token。用法: cfssh provision -token <API令牌> [-list] | -account <id> -zone <域名> [-subdomain ssh] [-tunnel-name cfssh] [-out cfssh-provision.json]")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	accounts, zones, err := orchestrator.TestToken(ctx, *token)
	if err != nil {
		fmt.Fprintln(os.Stderr, "令牌验证失败:", err)
		os.Exit(1)
	}

	if *list {
		fmt.Println("账户:")
		if len(accounts) == 0 {
			fmt.Println("  (这把令牌列不出账户名，用域名所属账户即可)")
		}
		for _, a := range accounts {
			fmt.Printf("  %s  %s\n", a.ID, a.Name)
		}
		fmt.Println("域名:")
		for _, z := range zones {
			fmt.Printf("  zone-id=%s  %s  (account=%s)\n", z.ID, z.Name, z.AccountID)
		}
		return
	}

	// Resolve zone by name when only -zone was given.
	zid, zname, zacct := *zoneID, *zoneName, ""
	for _, z := range zones {
		if (*zoneID != "" && z.ID == *zoneID) || (*zoneID == "" && *zoneName != "" && strings.EqualFold(z.Name, *zoneName)) {
			zid, zname, zacct = z.ID, z.Name, z.AccountID
			break
		}
	}
	if zid == "" || zname == "" {
		fmt.Fprintln(os.Stderr, "找不到域名，先用 -list 看看有哪些 zone")
		os.Exit(1)
	}
	acct := *account
	if acct == "" {
		acct = zacct
	}
	if acct == "" {
		fmt.Fprintln(os.Stderr, "需要 -account（用 -list 查看）")
		os.Exit(1)
	}

	cfg := config.File{
		APIToken:      *token,
		AccountID:     acct,
		ZoneID:        zid,
		ZoneName:      zname,
		Subdomain:     *subdomain,
		TunnelName:    *tunnelName,
		Port:          *port,
		LocalhostOnly: *localhost,
	}

	bundle, err := orchestrator.Provision(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "预配失败:", err)
		os.Exit(1)
	}
	if err := provision.Save(*out, bundle); err != nil {
		fmt.Fprintln(os.Stderr, "写入 sidecar 失败:", err)
		os.Exit(1)
	}

	fmt.Printf("已预配完成，公网主机名: %s\n", bundle.Hostname)
	fmt.Printf("sidecar 已写入: %s\n", *out)
	fmt.Println("把这个文件和 cfssh（或 cfssh.exe）放在同一目录发给客户。客户运行后只需输入自己的登录密码。")
	fmt.Println()
	fmt.Println("客户端连接命令（对方也要装 cloudflared）:")
	fmt.Println("  " + clientcfg.Command("<用户名>", bundle.Hostname, bundle.ServiceTokenClientID, bundle.ServiceTokenSecret))
}
