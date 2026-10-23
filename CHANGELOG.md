# 更新日志

## 1.0.14 — 2026-10-23

优化交互与新增操作日志查看。

### 新增

- **操作日志**：左下角「设置」新增「操作日志」入口，点击弹出日志模态框，按时间倒序查看最近的登录、退出登录、新建、重命名、删除笔记和修改密码等操作；日志保存在 `${TRIM_PKGVAR}/oplog.jsonl`（与 `notes/`、`users.json` 同级），权限 `0600`，升级、卸载都会保留。

### 变更

- **新建按钮**：右上角不再显示「新建」字样，改为只显示「+」的圆形按钮（悬停仍有「新建笔记」提示）。
- **标题去后缀**：左侧笔记列表与右侧编辑区标题都不再显示 `.txt` 后缀，界面更干净；真实文件名不变，重命名/搜索行为不受影响。

## 1.0.13 — 2026-10-15

新增用户登录，笔记不再对局域网/外网完全开放。

### 新增

- **首次安装创建账号**：`users.json` 不存在（或没有任何账号）时，打开界面就是「创建账号」页；账号建好后创建入口自动关闭，接口层面也无法再被调用（`POST /api/setup` 返回 409），不会被人趁虚而入注册第二个账号。
- **必须登录**：除 `/api/session`、`/api/login`、`/api/setup`、`/api/logout` 外，所有接口（`/api/info`、`/api/notes*`、`/api/password`）都要带会话 Cookie，否则返回 401，前端随即回到登录页。
- **记住账号密码**：登录页可勾选，勾选后账号密码存在浏览器本地（`localStorage`），下次打开自动填好；登录会话勾选「记住」时保留 30 天，否则关掉浏览器即失效。主动「退出登录」会清掉本地保存的账号密码。
- **设置面板**：左下角状态栏新增齿轮图标，内含当前账号、版本号、数据目录，以及**修改密码**和**退出登录**。
- **修改密码**：需验证当前密码，新密码至少 6 位；改完之后**其他设备上的会话立即失效**，本机保持登录，本地保存的密码同步更新。
- **登录限速**：同一客户端 + 同一账号 5 分钟内错 5 次，返回 429 并带 `Retry-After`，避免被暴力猜密码（应用可能通过端口转发暴露到公网）。
- 密码用 **PBKDF2-HMAC-SHA256（12 万次迭代、随机盐）** 存储，`users.json` 权限 `0600`；仍是纯标准库实现，没有引入任何第三方依赖。

### 变更

- 左侧状态栏与右侧状态栏统一为 36px 高并共用同一条上边框，两条底栏严格齐平（之前左栏比右栏高约 8px）。
- 账号文件默认落在 `${TRIM_PKGVAR}/users.json`（与 `notes/` 同级），升级、卸载都不会删除；可用 `-users` 指定别的位置。
- 服务端启动日志会打印用户文件路径，没有任何账号时提示「首次运行：请打开界面创建账号」。

### 测试

- `internal/auth`：PBKDF2 标准测试向量、随机盐、密码强度与用户名校验、改动密码、会话过期与回收、登录限速。
- `internal/server`：未登录一律 401、首次创建账号后无法二次创建、改密码踢掉其他设备、退出登录清 Cookie、网关前缀下 Cookie 作用域收敛到 `/app/notepad/`。

## 1.0.12 — 2026-10-15

修复在 HTTPS 桌面打开应用一片空白的问题。

### 修复

- **混合内容被浏览器拦截**：1.0.11 的桌面入口用 `type=iframe` + `protocol=http` + `port=18180`，在 `http://<NAS>:5666/` 桌面下正常，但在 `https://<域名>:5667/` 桌面下，浏览器会阻止 iframe 加载明文 HTTP 资源，页面因此空白。
- 桌面入口改为走**飞牛统一网关**：`gatewayPrefix=/app/notepad`、`gatewaySocket=notepad.sock`、`url=/app/notepad/`，iframe 与桌面同源同协议（复用飞牛自己的 HTTPS 证书），HTTP / HTTPS 两种桌面都能打开。应用本身已实现 unix socket 监听与 `/app/notepad` 前缀剥离，无需额外改动。
- 服务端在收到不带结尾斜杠的网关前缀（`/app/notepad`）时返回 301 跳到 `/app/notepad/`，保证相对资源路径不会解析到应用外。

### 文档

- 新增 [docs/fnos-fpk-spec.md](docs/fnos-fpk-spec.md)：飞牛 fpk 开发规范（包结构、manifest 字段表、`ui/config` 规范、图标、生命周期脚本、自检清单）。
- [docs/fnos-fpk-guide.md](docs/fnos-fpk-guide.md) 新增第 14 节：1.0.11 / 1.0.12 两次修复的完整复盘与发布 / 升级操作方法。

## 1.0.11 — 2026-10-15

修复本地安装后应用中心没有「打开」按钮、桌面没有图标的问题。

### 修复

- **桌面入口配置写法错误**：`app/ui/config` 原来用的是社区博客的扁平结构（顶层 `type`/`url`/`icon256`），飞牛根本解析不出入口，因此既没有「打开」也没有桌面图标。现改为飞牛官方的 `".url"` 结构，入口 ID `notepad.main` 与清单里的 `desktop_applaunchname` 一致，并补齐 `title` / `icon` / `type` / `protocol` / `port` / `url` / `allUsers` 字段；图标引用改为官方约定的 `images/icon_{0}.png`。
- 打包脚本新增校验：检查 `ui/config` 的 `.url` 结构与必需字段、载荷里存在 `icon_64.png` / `icon_256.png`，并确认 `ICON.PNG` / `ICON_256.PNG` 尺寸严格为 64×64 / 256×256；不满足直接让打包失败。

## 1.0.7 — 2026-10-14

保证 `https://<域名>:18181/` 一定能起来。

### 新增

- 证书按四级优先级查找：`notepad.env` 指定 → `${TRIM_PKGVAR}/tls/{cert,key}.pem` → 飞牛按域名存放的 `/usr/trim/etc/ssl.{crt,key}/<域名>` → nginx 配置里的具体路径。
- 上述都没有时**自动签发一张自签证书**放进 `${TRIM_PKGVAR}/tls/`，HTTPS 立即可用（浏览器会提示证书不受信任）。把 `NOTEPAD_TLS_HOST=你的域名` 写进 `notepad.env`，自签证书的域名就能对上，只剩"不受信任"一个提示。
- 识别并跳过飞牛 nginx 里的 `ssl_certificate $ssl_server_name;`（变量形式，不是具体文件路径）。

## 1.0.6 — 2026-10-14

让 `https://<域名>:18181/` 开箱可用。

### 新增

- `cmd/main` 会自动从飞牛的 nginx 配置（`nginx -T`）里找出正在使用的证书，直接给应用的 HTTPS 监听复用；有多张证书时优先选 CA 签发的那张，跳过 nginx 自带的自签默认证书。
- 如果想指定证书，仍然优先读 `${TRIM_PKGVAR}/notepad.env` 里的 `NOTEPAD_TLS_CERT` / `NOTEPAD_TLS_KEY`，或者把证书放到 `${TRIM_PKGVAR}/tls/cert.pem` + `key.pem`。

## 1.0.5 — 2026-10-14

接入飞牛网关，走飞牛自带的 HTTPS，无需自己配证书。

### 新增

- 支持 **unix socket 监听**（`-unix`）。`cmd/main` 会按飞牛约定在 `${TRIM_APPDEST}/notepad.sock` 上开一个 socket，nginx 把 `/app/notepad/` 反代到这里，于是 `https://<域名>:<飞牛的 HTTPS 端口>/app/notepad/` 直接可用，用的是飞牛自己的证书。
- 请求路径前缀自动剥离（`-prefix`，默认 `/app/notepad`），所以同一套 handler 既能被网关反代，也能继续用 `http://<NAS>:18180/` 直连。
- 支持 HTTPS 双监听（`-tls-addr` / `-tls-cert` / `-tls-key`），没证书时自动跳过，不影响 HTTP。

### 变更

- 前端资源与接口全部改用**相对路径**（`assets/…`、`api/…`），因此可以挂在 `/app/notepad/` 这类子路径下；页面 URL 缺少结尾斜杠时会自动补上。
- 停止服务时清理 socket 文件。

## 1.0.2 — 2026-10-14

修正载荷打包形式：`app.tgz`。

### 修复

- 上一步清单校验已通过，但报错变为「解压 app.tgz 失败」。原因是应用载荷不能直接以 `app/` 目录放进包里，**必须以 `app.tgz`（tar.gz）形式放在包根目录**，由飞牛解压后落到 `${TRIM_APPDEST}`。
- 构建脚本改为：先把三份二进制与 `ui/` 打进 `app.tgz`，再把 `manifest`、`cmd/`、`config/`、`wizard/`、`app.tgz`、两个图标一起打成 fpk。
- `cmd/main` 现在能从 `${TRIM_APPDEST}/notepad-<arch>` 直接取到载荷（同时保留对旧 `app/` 布局的兼容）。

## 1.0.1 — 2026-10-14

按飞牛真实规范重做打包，修复「不是有效的 fpk 文件」。

### 修复

- **清单的 `platform` 取值错误**：原先写 `x86_64`，而飞牛只接受 `x86` / `arm` / `all`，触发 `platform (%s) is invalid`，导致上传被判为无效 fpk。现改为 `platform = all`。
- 清单字段名对齐飞牛规范：`source=thirdparty`、新增 `service_port` / `service_protocol` / `checkport` / `desktop_uidir` / `desktop_applaunchname` / `ctl_stop` / `ctl_uninstall` / `install_type` / `min_size` / `support_url` 等，去掉飞牛不识别的 `author`。
- 生命周期脚本改名并改成飞牛的接口：原来的 `cmd/install|start|stop` 换成 **`cmd/main start|stop|status`**（返回码 `0=运行中/成功`、`3=未运行`），并补齐 `install_init` / `install_callback` / `upgrade_init` / `upgrade_callback` / `uninstall_init` / `uninstall_callback`。
- 新增 `config/privilege`（`run-as: package`）与 `config/resource`。
- `cmd/main` 改用飞牛传入的 `TRIM_APPDEST` / `TRIM_PKGVAR` / `TRIM_SERVICE_PORT`，数据落在 `TRIM_PKGVAR/notes`。

### 变更

- 一个包通吃三种架构：包内带 amd64 / arm64 / armv7 三份二进制，`cmd/main` 按 `uname -m` 选择，产物为 `dist/notepad-1.0.1.fpk`。

## 1.0.0 — 2026-10-14

首个版本。

### 新增

- 记事本核心功能：新建、打开、阅读、编辑、保存、删除、重命名。
- 自动保存：停止输入约 700ms 自动落盘，切换笔记 / 切走标签页 / 关窗前兜底保存；`Ctrl/Cmd + S` 手动立即保存。
- 灰白配色、现代简洁的响应式界面：桌面左右分栏，窄屏切换为列表 + 编辑器。
- 顶部文件名可直接编辑即重命名；底部状态栏显示保存状态、字数与行数。
- 编辑器字号切换（14–20px），选择记于浏览器本地。
- 文件名安全校验（禁止路径穿越、控制字符、隐藏名），自动补 `.txt`。
- 原子写入，避免半截文件。
- fnOS 打包：`manifest`、生命周期脚本（`install`/`start`/`stop`/`status`/`uninstall`/`upgrade`）、图标，`amd64` / `arm64` / `arm` 三平台交叉编译。
- 单元测试覆盖笔记存储与 HTTP 接口。
