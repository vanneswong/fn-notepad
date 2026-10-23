# 记事本 · fnOS 应用

一个为飞牛私有云（fnOS）打包的轻量记事本应用。用来自在网页里新建、打开、阅读、编辑和保存纯文本文件，**输入即自动保存**，不需要手动点保存。界面是灰白配色，现代、安静、简洁。

后端是一个零第三方依赖的 Go 程序（只用标准库），前端是一个无构建步骤的原生 HTML/CSS/JS 单页应用，两者编译进同一个可执行文件，安装后只有一个二进制。

**首次打开要创建账号**，之后必须登录才能看到笔记；支持「记住账号密码」，左下角齿轮里可以修改密码、查看操作日志和退出登录。

## 访问地址与数据位置

| 用途 | 地址 |
| --- | --- |
| 局域网（HTTP） | `http://<IP>:18180/` |
| 外网（HTTPS） | `https://<域名>:18181/` |

> 把 `<IP>` 换成 NAS 的局域网地址，把 `<域名>` 换成你的域名。**用域名访问时会自动强制 HTTPS**（HTTP 端口只对 IP 访问开放）；用 IP 访问则可以直接走 HTTP。外网 HTTPS 用的是自签证书，浏览器首次会提示"不安全"，点继续访问即可（详见下文「三种访问方式」）。18181 需要在路由器上做端口转发。

**数据存储位置：`/vol1/@appdata/notepad/notes`** —— 每篇笔记一个纯文本文件，用任何工具都能直接读写；**卸载应用不会删除这个目录**，升级也会保留。

**账号文件：`/vol1/@appdata/notepad/users.json`** —— 与 `notes/` 同级，密码以 PBKDF2-HMAC-SHA256（12 万次迭代 + 随机盐）存储，文件权限 `0600`；同样不会被升级或卸载删除。**忘记密码时删掉这个文件，重启应用即可重新创建账号**（笔记不受影响）。

**操作日志：`/vol1/@appdata/notepad/oplog.jsonl`** —— 与上面两个文件同级，记录登录、退出登录、新建、重命名、删除笔记和修改密码等操作，权限 `0600`；可在左下角「设置 → 操作日志」里查看，升级、卸载都会保留。

应用中心里的简介同样写了这两条地址和数据位置。

## 功能

- **新建 / 打开 / 阅读 / 编辑 / 保存**：完整的文本文件增删改查；右上角是只显示「+」的圆形新建按钮，列表和编辑区标题都不显示 `.txt` 后缀。
- **登录**：首次安装创建账号 → 必须登录才能查看与编辑；未登录时所有接口返回 401，前端自动回到登录页。
- **记住账号密码**：登录页勾选后，账号密码保存在浏览器本地，下次打开自动填好（勾选「记住」时登录状态保留 30 天）。
- **设置**：左下角状态栏的齿轮图标，里面有当前账号、版本号、数据目录，以及**修改密码**、**操作日志**、**退出登录**；改密码后其他设备上的登录会立即失效。
- **自动保存**：停止输入约 700ms 后自动落盘；切换笔记、切走标签页或关闭窗口前也会兜底保存。也可以按 `Ctrl/Cmd + S` 立即保存。
- **重命名 / 删除**：顶部工具栏操作，删除有二次确认。
- **搜索**：按文件名即时过滤。
- **状态提示**：底栏实时显示「未保存 / 保存中 / 已保存 + 时间」，以及字数和行数；左右两条底栏等高对齐（36px）。
- **安全写入**：每次保存都写临时文件再原子替换，中途断电不会产生半截文件。
- **域名强制 HTTPS**：用域名访问 HTTP 端口时会自动 301 到 HTTPS 端口，避免明文传输；用 IP 访问局域网时仍可用 HTTP。
- **自动发布**：推送 `v*` 标签时，GitHub Actions 自动跑测试、打包 fpk 并发布带附件的 Release。
- **响应式**：桌面端左右分栏，窄屏（手机/平板）自动切换成「列表 → 编辑器」两层。

## 界面与配色

整体是灰白单色系，用色克制：浅灰背景 `#f4f5f6`、白色面板、极浅描边 `#e7e9ec`，主色是近黑灰 `#23272e`，正文 `#1d2126`，辅助文字 `#8b929c`。圆角、细阴影、过渡动画都很轻，没有亮色块和强对比，长时间阅读不累。

编辑器使用等宽字体，字号可在 14–20px 之间循环切换（右下角 `A` 按钮），选择会记在浏览器本地。

## 目录结构

```
.
├── cmd/notepad/          # 程序入口
├── internal/store/       # 笔记文件读写 + 文件名安全校验 + 原子写
├── internal/auth/        # 账号（PBKDF2 口令散列）+ 会话 + 登录限速
├── internal/oplog/       # 操作日志（JSONL 追加写 + 倒序读取）
├── internal/server/      # JSON API + 静态资源 + 中间件
├── web/                  # 前端（通过 go:embed 编进二进制）
│   ├── index.html
│   └── assets/{app.css,app.js,favicon.svg}
├── tools/icongen/        # 生成应用图标（纯标准库，可离线运行）
├── packaging/            # fnOS 安装包模板
│   ├── manifest          # 应用清单
│   └── cmd/              # 生命周期脚本 install/start/stop/status/uninstall/upgrade
├── scripts/build-fpk.sh  # 打包脚本
├── .github/workflows/    # 推送 v* 标签时自动发布 Release
├── docs/
│   ├── fnos-fpk-spec.md  # fpk 开发规范（字段表 / 入口 / 自检清单）
│   └── fnos-fpk-guide.md # 实战笔记与排错复盘
└── Makefile
```

## 本地开发

```bash
make run                 # 默认 http://127.0.0.1:18180，数据写到 ./data
make test                # 单元测试
make vet                 # 静态检查
```

也可以手动指定监听地址和笔记目录：

```bash
go run ./cmd/notepad -addr 127.0.0.1:18180 -data ~/notes -users ~/users.json
```

（`-users` 不写时默认放在 `-data` 的上一级目录，即 `<数据目录>/../users.json`。）

## HTTP 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/session` | 登录状态：是否需要创建账号、是否已登录、当前账号 |
| POST | `/api/setup` | 首次创建账号，body `{"username":"...","password":"...","remember":true}`；已存在账号时返回 409 |
| POST | `/api/login` | 登录，body `{"username":"...","password":"...","remember":true}`；密码错误 401，错太多次 429 |
| POST | `/api/logout` | 退出登录，清除会话 Cookie |
| POST | `/api/password` | 修改密码，body `{"current":"...","next":"..."}`（需登录） |
| GET | `/api/info` | 应用名、版本、数据目录、笔记数量 |
| GET | `/api/notes` | 笔记列表（名称、大小、修改时间） |
| POST | `/api/notes` | 新建，body `{"name":"标题"}` |
| GET | `/api/notes/{name}` | 读取一篇笔记的正文 |
| PUT | `/api/notes/{name}` | 保存，body `{"content":"..."}` |
| DELETE | `/api/notes/{name}` | 删除 |
| POST | `/api/notes/{name}/rename` | 重命名，body `{"name":"新标题"}` |

除 `/api/session`、`/api/setup`、`/api/login`、`/api/logout` 外，所有接口都需要会话 Cookie（`notepad_session`，HttpOnly + SameSite=Lax），否则返回 401。

文件名会被严格校验：禁止路径分隔符、`..`、控制字符和以点开头的隐藏名，没有扩展名时自动补 `.txt`。所有读写都被限制在一个笔记目录内，不会越过它去访问系统文件。

通过飞牛网关访问（`/app/notepad/`）时，会话 Cookie 的作用域会收敛到 `/app/notepad/`；直连服务端口时才是 `/`，因此在同一台 NAS 上的其他应用看不到这个 Cookie。

## 打包 fnOS 安装包

> - **开发规范**（包结构、manifest 字段表、`ui/config` 规范、图标、生命周期脚本、自检清单）：[docs/fnos-fpk-spec.md](docs/fnos-fpk-spec.md)
> - **实战笔记 / 排错复盘**（两次桌面入口问题的完整过程与发布方法）：[docs/fnos-fpk-guide.md](docs/fnos-fpk-guide.md)

```bash
./scripts/build-fpk.sh
```

产物是 `dist/notepad-1.0.13.fpk`（gzip 压缩的 tar）。脚本会交叉编译 **amd64 / arm64 / armv7 三份二进制**，由 `cmd/main` 在运行时按 `uname -m` 挑，所以清单里可以写 `platform = all`，**一个包通吃所有机型**。

包内结构与飞牛第三方应用完全一致：

```
manifest            # 清单：INI 风格 key = value，必填 appname / version / platform
ICON.PNG            # 64x64 图标
ICON_256.PNG        # 256x256 图标
cmd/main            # 生命周期脚本：main start|stop|status（0=成功/运行中，3=未运行）
cmd/install_init    # install_callback / upgrade_init / upgrade_callback /
cmd/uninstall_init  # uninstall_callback —— 占位脚本
config/privilege    # 运行身份 {"defaults":{"run-as":"package"}, ...}
config/resource     # 资源声明
wizard/             # 安装向导目录（可为空）
app.tgz             # 应用载荷（tar.gz），解压后落到 ${TRIM_APPDEST}
```

`app.tgz` 里就是载荷本体：三份 `notepad-<arch>` 二进制加一个 `ui/`（桌面入口页），解压后位于 `${TRIM_APPDEST}` 根目录。

飞牛通过环境变量把路径交给 `cmd/main`：`TRIM_APPDEST`（载荷目录）、`TRIM_PKGVAR`（数据目录）、`TRIM_PKGTMP`（临时目录）、`TRIM_SERVICE_PORT`（服务端口）。

## 三种访问方式

应用启动后会同时提供以下入口，取决于你部署的环境：

| 入口 | 说明 |
| --- | --- |
| `http://<NAS>:18180/` | 直连服务端口，明文 HTTP，局域网最省事 |
| `https://<域名>:18181/` | 应用自己监听 HTTPS。证书按优先级查找：`notepad.env` 指定 → `${TRIM_PKGVAR}/tls/{cert,key}.pem` → 飞牛按域名存放的证书 → nginx 配置；**都没有时自动签发自签证书**，因此开箱即可用（浏览器会提示不受信任） |
| `https://<域名>:<飞牛HTTPS端口>/app/notepad/` | 走飞牛网关（应用在 `${TRIM_APPDEST}/notepad.sock` 上监听），用飞牛自己的证书 |

网关方式依赖飞牛 nginx 把 `/app/notepad/` 反代到应用的 socket；应用的请求路径会自动剥离 `/app/notepad` 前缀，前端资源也全部用相对路径，所以同一套代码三种入口都能用。

桌面图标与应用中心的「打开」按钮走的就是**网关入口**（`app/ui/config` 里的 `gatewayPrefix` + `gatewaySocket`），因此 iframe 与飞牛桌面同源同协议：HTTP 桌面（`:5666`）和 HTTPS 桌面（`:5667`）都能正常打开，不会出现混合内容导致的空白页。

自签证书想让域名对上，在 `${TRIM_PKGVAR}/notepad.env` 里写 `NOTEPAD_TLS_HOST=你的域名` 即可；要完全消除浏览器警告，就把受信任的证书放到 `${TRIM_PKGVAR}/tls/`（命名成 `cert.pem` / `key.pem`），这个优先级最高。

## 在 fnOS 上安装

1. `./scripts/build-fpk.sh` 生成 `dist/notepad-1.0.13.fpk`；
2. 打开飞牛「应用中心 → 手动安装」，上传该 `.fpk`；
3. 安装完成后启动应用，点桌面图标，或直接访问 `http://<NAS地址>:18180`。

### 备用安装方式（不走应用中心）

如果应用中心仍然拒绝，可以直接用 SSH 手动跑起来，效果一样，只是不出现在应用中心列表里：

```bash
# 1. 把 fpk 传到 NAS 并解包
sudo mkdir -p /opt/notepad
sudo tar -xzf notepad-1.0.13.fpk -C /opt/notepad
sudo tar -xzf /opt/notepad/app.tgz -C /opt/notepad

# 2. 直接启动（前台运行，Ctrl-C 结束）
sudo /opt/notepad/notepad-amd64 -addr 0.0.0.0:18180 -data /opt/notepad/data
```

ARM 机型把 `notepad-amd64` 换成 `notepad-arm64`（64 位）或 `notepad-armv7`。之后访问 `http://<NAS地址>:18180`。

## 数据与配置

笔记默认放在 `/var/lib/notepad/notes`。数据目录的确定顺序为：

1. 环境变量 `NOTEPAD_HOME`；
2. 安装时由 fnOS 传入的 `TRIM_PKGVAR` / `PKGVAR`；
3. 兜底 `/var/lib/notepad`。

账号文件默认放在数据目录的上一级（fnOS 上是 `${TRIM_PKGVAR}/users.json`，与 `notes/` 同级），可用 `-users` 或环境变量 `NOTEPAD_USERS` 指定。

端口默认 `18180`，可用 `-addr` 或环境变量 `NOTEPAD_ADDR` 覆盖。

**卸载不会删除笔记，也不会删除账号文件**（两者都不在应用目录里），需要时手动清理。

## 关于图标

图标由 `tools/icongen` 用 Go 标准库程序化绘制（深灰圆角方块 + 白色文档 + 折角），可离线、可复现。构建时自动生成 64/128/256 三种尺寸。

## License

MIT
