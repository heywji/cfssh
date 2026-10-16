# CF SSH

图形化的本机 SSH 发布工具。它在这台电脑上启动 SSH，只用 Cloudflare Tunnel 把连接转到公网。关掉窗口后，Cloudflare Tunnel 和这次启动的 SSH 都会停掉。

## 下载

已编译的程序在 [Releases](https://github.com/heywji/cfssh/releases/latest)：

| 文件 | 系统 |
| --- | --- |
| [cfssh-windows-amd64.exe](https://github.com/heywji/cfssh/releases/latest/download/cfssh-windows-amd64.exe) | Windows 64 位 |
| [cfssh-macos.zip](https://github.com/heywji/cfssh/releases/latest/download/cfssh-macos.zip) | macOS Apple 芯片，解压后打开里面的 `CF SSH.app` |

macOS 请用这个 app，不要双击单独的可执行文件。从浏览器下载后如果系统拦住，在终端执行：

```bash
xattr -dr com.apple.quarantine ~/Downloads/cfssh-macos.zip ~/Downloads/CF\ SSH.app
open ~/Downloads/CF\ SSH.app
```

Windows 上如果 SmartScreen 拦截，点「仍要运行」。API 令牌不要贴进窗口，只写在配置文件的 `api_token` 里。

Linux 在 Fedora 或 Ubuntu 上执行 `make build`。客户端只要安装 [cloudflared](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/)，然后 `ssh 你的主机名`。

主要在 Windows 上用。同一套代码也能在 Fedora、Ubuntu 和 macOS 上编译。程序是一个 Go 二进制，运行时不用再装 Python、Node 或 Docker。第一次启动时，如果系统里没有 `cloudflared`，它会从 Cloudflare 的 GitHub 发布页下载官方程序。

临时的 `trycloudflare.com` 地址只能转发网站，不能转发 SSH。所以需要：

- 一个 Cloudflare 账号
- 一个已经把 DNS 托管在 Cloudflare 的域名

## 它会做什么

1. 安装或使用本机的 OpenSSH。Linux 上如果没有 `sshd`，会用 `apt` 或 `dnf` 安装。Windows 上会启用系统自带的 OpenSSH Server 功能。macOS 使用系统自带的 `sshd`。
2. SSH 默认只听 `127.0.0.1` 上的一个端口（默认 2222）。不在路由器上做端口映射，也不打开入站防火墙。
3. 用 API 令牌创建或复用一条远程管理的 Cloudflare Tunnel，把 `主机名` 指到本机 SSH。
4. 创建 Cloudflare Access 应用，并给客户端发一把服务令牌。连接时不用收邮箱验证码。
5. 生成本机密钥 `~/.ssh/cfssh_ed25519`，并把公钥写入 `authorized_keys`。Windows 管理员还会写入 `administrators_authorized_keys`。
6. 关闭窗口、进程退出，或者点「停止」时，停掉 `cloudflared` 和这次启动的 SSH。

云端的隧道和 DNS 记录会留着，下次启动直接复用。程序不会在关机后自动再开 SSH。

## 令牌权限

打开 [API 令牌页面](https://dash.cloudflare.com/profile/api-tokens)，创建自定义令牌，勾选这些权限：

| 范围 | 权限 |
| --- | --- |
| 账户 | Cloudflare Tunnel / Cloudflare One Connectors，编辑 |
| 账户 | Access: Apps and Policies，编辑 |
| 账户 | Access: Service Tokens，编辑 |
| 区域 | DNS，编辑 |

区域资源选你要用来公开 SSH 的那个域名。令牌保存在本机配置里，文件权限是 `0600`：

- macOS 和 Linux：`~/.config/cfssh/config.json`
- Windows：`%AppData%\cfssh\config.json`

## 使用

```bash
# 在对应系统上编译后运行
make run
```

1. 把 API 令牌写进配置文件的 `api_token`，打开程序后点「刷新域名」。窗口不显示这把令牌。
2. 选择账户和域名，主机名前缀默认是 `ssh`，公网名字就是 `ssh.你的域名`。
3. 填写当前用户的 SSH 登录密码，并再输入一次。Windows 上这就是当前 Windows 用户的登录密码，客户端 SSH 进来时要填同一个密码。程序只向系统核对，不写入配置文件，也不修改这个密码。
4. 点「启动」。Windows 会弹出 UAC。macOS 会弹出管理员密码框，Linux 桌面用 polkit（`pkexec`）。需要管理员权限是因为 SSH 服务要能登录到本机用户。
5. 把窗口里的命令或 SSH 配置复制到**客户端**。客户端也要安装 [cloudflared](https://developers.cloudflare.com/cloudflare-one/networks/connectors/cloudflare-tunnel/downloads/)。登录用户是这台电脑的当前用户，密码是上一步填写的那个。

客户端 `~/.ssh/config` 类似：

```sshconfig
Host ssh.example.com
    User 你的用户名
    ProxyCommand cloudflared access ssh --hostname %h --service-token-id '客户端 ID' --service-token-secret '客户端密钥'
    IdentityFile ~/.ssh/cfssh_ed25519
```

连接时只输入系统密码。服务令牌已经写在这条命令里，不用再收邮箱验证码。

把「只监听 127.0.0.1」留着。这样就算本机防火墙被改过，局域网也连不上这个 SSH。

## 关掉之后

- `cloudflared` 会被结束。
- Linux 和 macOS 上，程序自己拉起的 `sshd` 会被结束，系统原来的 SSH 服务不动。
- Windows 上会停掉 OpenSSH 服务，并把 `sshd_config` 还原。安装程序自带的 OpenSSH 入站防火墙规则会被删掉，避免 22 端口裸露在网上。
- 如果从任务管理器强制结束窗口进程，旁边的 helper 发现父进程没了，也会停掉隧道和 SSH。正在安装 OpenSSH 时关掉窗口，安装进程同样会被停掉。

## 编译

在目标系统上编译。Fyne 界面需要 CGO 和系统图形库，所以不能从 macOS 直接交叉编译出一个能用的 Windows 程序。

macOS：安装 Xcode 命令行工具，然后 `make build`。

Ubuntu：

```bash
sudo apt install golang gcc libgl1-mesa-dev libx11-dev libxrandr-dev libxinerama-dev libxcursor-dev libxi-dev
make build
```

Fedora：

```bash
sudo dnf install golang gcc libGL-devel libX11-devel libXrandr-devel libXinerama-devel libXcursor-devel libXi-devel
make build
```

Windows：安装 Go 和 [MSYS2/mingw-w64](https://www.mingw-w64.org/) 的 gcc，然后在该环境里执行：

```bat
set CGO_ENABLED=1
go build -ldflags "-H windowsgui" -o dist\cfssh.exe .\cmd\cfssh
```

`-H windowsgui` 用来隐藏旁边的黑色控制台。不加也能用。

## 安全

公网入口是 Cloudflare Access 的服务令牌，不是把 22 端口挂到网上。服务令牌和 API 令牌都在 `config.json` 里，不要把这个文件交给别人。

Windows 版会接管系统 OpenSSH 服务。关闭程序时这个服务会停。如果你本来就在用系统 OpenSSH，关闭本程序后需要自己再把它启动起来。
