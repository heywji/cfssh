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
3. 用预配好的隧道令牌（或高级界面里的 API 令牌）启动远程管理的 Cloudflare Tunnel，把 `主机名` 指到本机 SSH。只有 cloudflared 真正连上 Cloudflare 之后，窗口才显示「运行中」。
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

分成两个角色。**运营方**（你，持有 Cloudflare API 令牌）先为每台机器预配一次，生成一个 `cfssh-provision.json`；**客户**拿到 `cfssh` 和这个文件后，只需输入自己的登录密码。客户机器上永远不会出现你的 API 令牌。

### 运营方：预配一台机器（命令行）

```bash
# 先看看令牌能用哪些账户和域名
cfssh provision -token <API令牌> -list

# 为一台机器预配，生成 sidecar 文件
cfssh provision -token <API令牌> -zone example.com -subdomain ssh -tunnel-name cfssh-客户A
```

这一步会创建/复用隧道、发布 DNS、建好 Access 服务令牌，并把**隧道令牌和公网主机名**（不含 API 令牌）写进 `cfssh-provision.json`。每台机器建议用不同的 `-subdomain` 和 `-tunnel-name`，这样各有各的公网地址和隧道。

把生成的 `cfssh-provision.json` 和 `cfssh`（或 `cfssh.exe`、macOS 的 `CF SSH.app`）**放在同一目录**发给客户即可。

### 客户：只填密码

1. 把 `cfssh.exe` 和旁边的 `cfssh-provision.json` 一起运行。程序检测到 sidecar 后，界面只剩一个「当前用户密码」输入框。
2. 填写当前用户的登录密码并再输入一次。Windows 上就是当前 Windows 用户的登录密码，别人 SSH 进来时要用同一个。程序只向系统核对，不写入任何文件，也不修改密码。
   - **不知道自己的密码？** 如果当前用户是这台电脑的管理员（本地账户），窗口里会出现「不知道当前密码？给 <用户> 设置一个新密码」。勾选后输入新密码，启动前会再确认一次，然后把这个 Windows 账户的登录密码改成新密码（以后登录这台电脑也用它）。新密码用 Windows DPAPI 加密后交给提权进程，不以明文落盘。域账户和微软账户不支持。
3. 点「启动」。Windows 会弹出 UAC，macOS 弹管理员密码框，Linux 桌面走 polkit（`pkexec`）。
4. 启动后窗口里给出连接命令，发给要登录的人。关掉窗口就停掉隧道和 SSH。

> 没有 `cfssh-provision.json` 时，程序会打开完整的高级界面（运营方也可以在那里图形化地验证令牌、选域名、一键启动，令牌仍只读取自本机配置文件 `api_token`，不显示在窗口里）。

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

## 没有显卡驱动的 Windows

多数云服务器和虚拟机只有 Microsoft Basic Display Adapter，没有可用的 OpenGL。

- **客户窗口**（旁边有 `cfssh-provision.json` 时）在 Windows 上用系统原生控件绘制，不需要 OpenGL，任何 Windows 都能打开。
- **高级窗口**（运营方用，没有 sidecar 时）仍然基于 Fyne，需要 OpenGL。没有时会弹窗说明原因并退出。运营方可以改用命令行 `cfssh provision`，或在有显卡驱动的电脑上打开高级窗口。确实需要在这类机器上打开高级窗口的话，可以把 [mesa-dist-win](https://github.com/pal1000/mesa-dist-win/releases) 的 `x64/opengl32.dll`、`x64/libgallium_wgl.dll` 放到 `cfssh.exe` 旁边，并设置 `GALLIUM_DRIVER=llvmpipe` 后启动。

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

`cmd/cfssh/rsrc_windows_amd64.syso` 是由 `cmd/cfssh/cfssh.manifest` 编出来的 Windows 资源（现代控件样式、高 DPI），Go 编译 Windows 版时会自动链接。改了 manifest 后执行 `make winres` 重新生成。

## 安全

公网入口是 Cloudflare Access 的服务令牌，不是把 22 端口挂到网上。服务令牌和 API 令牌都在 `config.json` 里，不要把这个文件交给别人。

Windows 版会接管系统 OpenSSH 服务。关闭程序时这个服务会停。如果你本来就在用系统 OpenSSH，关闭本程序后需要自己再把它启动起来。
