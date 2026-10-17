# 工作原理

这份文档说明 cfssh 启动后，在本机和 Cloudflare 上实际做了什么。使用方法见 [README](../README.md)。

## 整体链路

```text
连接方                         Cloudflare                       被连的电脑
ssh ──> cloudflared access ──> Access（核对服务令牌）──> Tunnel ──> cloudflared ──> sshd（127.0.0.1:2222）
        （ProxyCommand）                                          （出站连接）
```

- 被连的电脑**只往外连**。cloudflared 主动连到 Cloudflare 的 7844 端口，不用做端口映射，也不开入站防火墙。
- sshd 只监听 `127.0.0.1`，局域网里的其他机器连不到。唯一的入口是 Cloudflare 隧道。
- Cloudflare Access 挡在最前面。不带正确服务令牌的请求，连 sshd 都到不了。进来以后还要过 SSH 本身的密码或密钥验证。

## 预配阶段（运营方，`cfssh provision`）

用 API 令牌在 Cloudflare 上做四件事，结果写进 `cfssh-provision.json`：

1. 新建或复用一个「远程管理」的隧道（名字是 `-tunnel-name`），拿到它的隧道令牌。
2. 把隧道的路由设成 `主机名 → ssh://127.0.0.1:端口`。
3. 在域名下建一条 CNAME，把主机名指向这个隧道。
4. 为这个主机名建一个 Access 应用，再发一把服务令牌，允许持有它的人通过。

预配文件里只有隧道令牌、服务令牌和主机名，**没有 API 令牌**。客户机器上的程序不会再调用 Cloudflare API，只用隧道令牌把 cloudflared 跑起来。

## 启动阶段（客户点「启动」）

### 1. 核对密码

先确认输入的就是当前用户的登录密码。程序只向系统核对，不保存密码。

- macOS：`dscl . -authonly <用户> <密码>`
- Windows：调用 `LogonUserW`

### 2. 准备 cloudflared

按这个顺序找：程序自己缓存的那份 → 系统 `PATH` 里已有的 → 从 Cloudflare 官方 GitHub 发布页下载。

### 3. 以管理员身份启动 helper

改 SSH 配置、起 sshd 需要管理员权限，所以 GUI 会再拉起一个提权的 helper 进程（`cfssh helper -dir <运行目录>`）。之后 sshd 和 cloudflared 都由这个 helper 负责启动和停止。

- macOS：通过 `osascript ... with administrator privileges` 启动，系统会弹管理员密码框。
- Windows：用 `ShellExecute` 的 `runas` 方式启动，会弹 UAC。

GUI 和 helper 之间不走网络。GUI 把启动参数写进运行目录（macOS 是 `~/.config/cfssh/run/`，Windows 是 `%AppData%\cfssh\run\`）里的 `request.json`；helper 把运行状态写进 `status.json`，日志写进 `helper.log`。helper 读完 `request.json` 后会把里面的隧道令牌删掉，令牌通过环境变量 `TUNNEL_TOKEN` 交给 cloudflared，不落盘。

### 4. 启动 SSH 服务端

两个系统的做法不一样。

#### macOS：系统自带的 sshd，单独起一份

macOS 本来就有 `/usr/sbin/sshd`，所以**什么都不安装**，也**不动「系统设置 → 通用 → 共享 → 远程登录」**。

1. 生成 cfssh 专用的主机密钥：`/etc/ssh/cfssh_host_ed25519_key` 和 `/etc/ssh/cfssh_host_rsa_key`。只在第一次生成，以后复用。
2. 写一份专用配置 `/etc/ssh/cfssh_sshd_config`：
   - `ListenAddress 127.0.0.1`，`Port 2222`
   - `AllowUsers <当前用户>`，`PermitRootLogin no`
   - 打开密码和公钥登录，`UsePAM yes`
   - `ClientAliveInterval 30`，用来清理断掉的会话
3. 前台运行 `/usr/sbin/sshd -D -e -f /etc/ssh/cfssh_sshd_config`，日志进 `helper.log`。
4. 停止时结束这个 sshd，删掉那份配置文件。主机密钥保留，这样下次连接时主机指纹不变。

如果你已经开了「远程登录」，两份 sshd 会同时运行、互不影响：

| | 系统「远程登录」 | cfssh 起的那份 |
| --- | --- | --- |
| 地址 | 22 端口，局域网可连 | `127.0.0.1:2222`，只有隧道能进 |
| 运行时间 | 一直开着，由 launchd 管理 | 只在 cfssh 窗口开着时运行 |
| 配置文件 | `/etc/ssh/sshd_config` | `/etc/ssh/cfssh_sshd_config` |
| 主机密钥 | 系统的 `ssh_host_*` | `cfssh_host_*` |

主机密钥不同，所以经隧道连进来看到的主机指纹，和直连 22 端口看到的不一样。这是正常的。

不直接复用「远程登录」，是因为它监听在局域网的 22 端口，而且关掉 cfssh 窗口后也停不掉；另外，大部分被协助的人根本没开它。

#### Windows：系统的 OpenSSH Server 服务

Windows 上用系统自带的 OpenSSH Server 功能，必要时先装上：

1. 检查 `OpenSSH.Server` 这个 Windows 功能，没装就用 `Add-WindowsCapability` 安装。
2. 生成主机密钥（`ssh-keygen -A`），然后把私钥文件的权限收紧到只有 SYSTEM 和 Administrators 能读。否则 sshd 会认为权限太宽，拒绝使用这些密钥。
3. 备份 `%ProgramData%\ssh\sshd_config`，再把端口、监听地址、`AllowUsers` 等几项写进一个 `# BEGIN cfssh managed` 区块，其余配置保留不动。
4. 删掉 OpenSSH 安装程序自带的「入站 22 端口」防火墙规则。
5. 启动 `sshd` 服务，等它开始监听 `127.0.0.1:2222`。
6. 停止时停掉服务，用备份还原 `sshd_config`。

因为改的是系统服务本身，如果你原本就在用 Windows 的 OpenSSH，关掉 cfssh 后要自己把服务重新启动。

### 5. 启动 Cloudflare 隧道

helper 运行 `cloudflared tunnel --no-autoupdate --loglevel info run`，隧道令牌通过环境变量 `TUNNEL_TOKEN` 传入。隧道的路由配置在 Cloudflare 云端（预配时已经设好），本机不需要配置文件。

cloudflared 默认会和 Cloudflare 建立 4 条连接。**至少有一条连上之后**，窗口才显示「运行中」；90 秒内一条都没连上，就报错退出。

## 运行中：断线检测与自动重连

被协助的人通常不在电脑前，没法帮你点「重新启动」，所以 helper 会自己盯着隧道：

- **判断隧道在不在线**：主要看 cloudflared 自带的 `/ready` 接口（helper 从日志里读出它的地址，每 5 秒查一次 `readyConnections`）。如果 `/ready` 超过 15 秒没响应，就改看日志里的 `Registered tunnel connection` / `Unregistered` 等记录，统计当前有几条连接。
- **断线时**：界面显示「重连中」，`status.json` 里的 `tunnel` 字段是 `reconnecting`。连接要稳定保持 30 秒，才算真正恢复。
- **自动重启**：遇到下面两种情况，helper 会重启 cloudflared：
  - 所有连接都断开超过 60 秒；
  - cloudflared 进程退出了。macOS 上已退出、但还没被回收的「僵尸进程」也算作已退出。
- **轮换协议**：每次重启都在 HTTP/2 和 QUIC 之间切换。有的网络 QUIC（UDP）不稳定，有的网络 7844 端口的 TCP 被拦截，只有 QUIC 能用。所以不固定用某一种，而是轮流试。
- **重启间隔逐次翻倍**：重启后如果一直连不上，下一次等待的时间会翻倍：10 秒、20 秒、40 秒……最长 5 分钟。一旦连上，间隔恢复为 10 秒。

## 停止

下面任何一种情况都会让 helper 停掉 cloudflared 和 sshd，然后退出：

- 在窗口里点「停止」（GUI 会在运行目录里写一个 `shutdown` 文件）；
- 关掉窗口；
- GUI 进程消失，比如被任务管理器强制结束。helper 每秒检查一次父进程还在不在。

云端的隧道、DNS 记录和 Access 应用都会保留，下次启动直接复用。程序不会开机自启，也不会在关机后自动再打开 SSH。

## 连接方

连接方不需要安装 cfssh，只需要装 `cloudflared`，然后在 ssh 命令里加上 ProxyCommand：

```sshconfig
Host remote-control-helper.jiwenkang.com
    User 对方的用户名
    ProxyCommand cloudflared access ssh --hostname %h --service-token-id '<Client ID>' --service-token-secret '<Client Secret>'
```

`cloudflared access ssh` 会带着服务令牌去连 Cloudflare Access。验证通过后，SSH 流量经隧道到达对方机器上的 `127.0.0.1:2222`。登录时输入的是对方电脑当前用户的登录密码。
