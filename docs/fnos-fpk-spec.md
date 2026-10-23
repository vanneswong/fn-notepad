# 飞牛 fnOS fpk 开发规范

> 本文是本项目（记事本）打包 `.fpk` 的**正式规范**：把官方文档
> <https://developer.fnnas.com/docs/guide/> 与本项目实机验证的结论整理成“照着做就能装成”的清单。
> 逐条踩坑的实战记录见 [fnos-fpk-guide.md](fnos-fpk-guide.md)。
>
> 约定：**官方规定** = 一定照做；**实测/实践** = 本项目验证过的做法；**待确认** = 尚未在真机验证。

## 1. 总览

一个可安装、能出桌面图标、能走网关的 fpk，由五部分构成：

```
<项目目录>/
├── app/                # 应用载荷（安装后映射到 target/，即 ${TRIM_APPDEST}）
│   └── ui/             # 桌面入口：config + images
├── cmd/                # 生命周期脚本：main / install_* / upgrade_* / uninstall_*
├── config/             # privilege（运行身份）+ resource（资源声明）
├── wizard/             # 安装/卸载表单，可为空目录
├── manifest            # 清单（INI 风格 key = value）
├── ICON.PNG            # 包图标 64×64
└── ICON_256.PNG        # 包图标 256×256
```

打包流程：

```
app/ ─► app.tgz ─┐
cmd/ config/ wizard/ manifest ICON*.PNG ─┴─► notepad-<version>.fpk
```

- `.fpk` = **gzip 压缩的 tar**。
- 包根**必须**有 `manifest`；载荷**必须**是包根的 `app.tgz`。
- 本项目用 `scripts/build-fpk.sh` 完成上述流程，并在打包时自动校验（见第 11 节）。

## 2. 目录与路径映射（硬性）

| 项目里 | 打包后 | 安装后 | 环境变量 |
| --- | --- | --- | --- |
| `app/**` | `app.tgz` 的内容 | `/var/apps/<appname>/target/**` | `${TRIM_APPDEST}` |
| `cmd/**` | 包根 `cmd/**` | `/var/apps/<appname>/cmd/**` | —— |
| `config/**` | 包根 `config/**` | `/var/apps/<appname>/config/**` | —— |
| `wizard/**` | 包根 `wizard/**` | `/var/apps/<appname>/wizard/**` | —— |
| `manifest` | 包根 `manifest` | `/var/apps/<appname>/manifest` | —— |
| `ICON.PNG` / `ICON_256.PNG` | 包根 | `/var/apps/<appname>/` | —— |

安装后 `target` / `etc` / `var` / `tmp` / `home` 都是指向存储卷的软链：

```
/var/apps/<appname>/
├── target -> /vol<n>/@appcenter/<appname>   # 载荷，${TRIM_APPDEST}
├── etc    -> /vol<n>/@appconf/<appname>      # 配置，${TRIM_PKGETC}
├── var    -> /vol<n>/@appdata/<appname>      # 持久数据，${TRIM_PKGVAR}
├── tmp    -> /vol<n>/@apptemp/<appname>      # ${TRIM_PKGTMP}
└── home   -> /vol<n>/@apphome/<appname>      # ${TRIM_PKGHOME}
```

**规范**：

- 生命周期脚本与程序里**只用环境变量**定位目录，**不要硬编码** `/var/apps/...` 或 `/vol1/...`。
- 需要在重启/升级后保留的数据放 `${TRIM_PKGVAR}`；临时文件放 `${TRIM_PKGTMP}`。
- 应用用户私有数据放 `${TRIM_PKGHOME}`；配置放 `${TRIM_PKGETC}`。

> 本项目里我们把 `app/` 理解为“载荷根”，等价于安装后的 `target/`。官方 `fnpack` 生成的项目同样如此：
> `app/ui/config` 最终落在 `${TRIM_APPDEST}/ui/config`。

## 3. manifest 规范

### 3.1 语法

- INI 风格 `key = value`，一行一个键；值可加引号可不加。
- **不能有注释行**（`#` 开头会被当作键解析失败）。多行说明用 `<br>`。
- `desc` 支持 HTML，可以写 `<a href="...">`。

### 3.2 字段表

| 字段 | 必填 | 取值 / 说明 | 本项目 |
| --- | --- | --- | --- |
| `appname` | ✅ | 应用唯一标识，**小写字母/数字/连字符**，不能中文 | `notepad` |
| `version` | ✅ | `X.Y.Z`（可带 `-beta` 之类后缀） | `1.0.12` |
| `display_name` |  | 应用中心显示名（中文放这里） | `记事本` |
| `desc` |  | 简介，支持 HTML | 见 `packaging/manifest` |
| `changelog` |  | 更新说明 | 每次发版必改 |
| `source` |  | `official` / `thirdparty`（**不是** `third-party`） | `thirdparty` |
| `platform` | ✅ | **只能是 `x86` / `arm` / `all`**，写成 `x86_64` 直接判无效 | `all` |
| `arch` |  | 已废弃，改用 `platform` | 不写 |
| `install_type` |  | 空=用户选存储位置；`root`=系统盘。本项目用 `volume` 也可装 | `volume` |
| `min_size` |  | 最小安装空间（字节） | `10485760` |
| `service_port` |  | 应用监听端口 | `18180` |
| `service_protocol` |  | `http` / `https` | `http` |
| `checkport` |  | 启动前是否探活端口，默认 `true`；不监听固定端口时可用 `false` | `true` |
| `desktop_uidir` |  | 桌面入口目录，相对应用目录，默认 `ui` | `ui` |
| `desktop_applaunchname` |  | 应用卡片「打开」用的入口 ID，必须与 `ui/config` 中某入口 ID 完全一致 | `notepad.main` |
| `desktop_appname` |  | 另一种写法，**与上者二选一** | 不写 |
| `ctl_stop` |  | 是否显示启动/停止控制 | `true` |
| `ctl_uninstall` |  | 是否允许卸载 | `true` |
| `disable_authorization_path` |  | 是否隐藏“授权目录”设置 | `false` |
| `os_min_version` |  | 最低系统版本 | `1.2.0` |
| `os_max_version` |  | 最高兼容版本（有明确上限才写） | 不写 |
| `maintainer` / `maintainer_url` |  | 开发者信息 | 见 manifest |
| `distributor` / `distributor_url` |  | 发布者信息 | 见 manifest |
| `support_url` |  | 支持链接 | 见 manifest |
| `install_dep_apps` |  | 依赖应用，多个用 `:` 分隔，`>` 声明最低版本 | 不写 |
| `checksum` |  | 商店包才有，32 位十六进制（MD5 待确认） | 不写 |

### 3.3 本项目清单模板

见 [`packaging/manifest`](../packaging/manifest)，可直接复制改 `appname` / `display_name` / 端口。

## 4. 载荷 `app.tgz`

- **必须**叫 `app.tgz` 放在包根，内容是 tar.gz；飞牛解压到 `${TRIM_APPDEST}`。
- 直接塞 `app/` 目录会报「解压 app.tgz 失败」。
- 多架构支持：包内放多份二进制，`cmd/main` 按 `uname -m` 选；这样 `platform=all` 才能名副其实。
  - 本项目：`notepad-amd64` / `notepad-arm64` / `notepad-armv7`。

### 4.1 前端接入网关的硬要求

- **所有资源与接口用相对路径**（`assets/…`、`api/…`）。写死 `/assets/…` 只能挂在根路径，网关 `/app/<name>/` 下会 404。
- 服务端要能把网关前缀**剥离**（本项目 `-prefix /app/notepad`），同一个 handler 同时服务直连端口和网关。
- **网关前缀缺结尾斜杠时自动补 `/`**（服务端 301 + 前端兜底），否则相对路径会解析到上一层，页面空白。

## 5. 图标规范

### 5.1 包图标（应用中心 / 安装识别）

| 文件 | 尺寸 | 要求 |
| --- | --- | --- |
| `ICON.PNG` | **严格 64×64** | PNG/JPG，sRGB，≤ 1024 KB |
| `ICON_256.PNG` | **严格 256×256** | 同上 |

- 尺寸错（例如放 128×128）会**静默失效**，不报错但桌面没图标。
- 视觉：圆角矩形主体，不要直角铺满；64px 下仍清晰；两个尺寸风格一致。

### 5.2 入口图标（桌面图标 / 文件打开方式）

- 放在 `${TRIM_APPDEST}/ui/images/`，官方命名 **`icon_64.png`**、**`icon_256.png`**（**下划线**）。
- `ui/config` 里通过 `"icon": "images/icon_{0}.png"` 引用，飞牛把 `{0}` 替换成 `64` / `256`。
- 本项目构建脚本同时输出下划线版（官方）和连字符版 `icon-64.png`（兼容旧写法），但配置只引用下划线版。

## 6. 桌面入口 `ui/config` 规范（最容易写错）

### 6.1 结构

入口配置是 JSON，**必须包在 `".url"` 对象里**，key 是入口 ID：

```json
{
  ".url": {
    "<appname>.main": { ...入口字段... }
  }
}
```

- 入口 ID 用 `appname` 作前缀，保持稳定，例如 `notepad.main`。
- `manifest.desktop_applaunchname` 必须等于其中一个入口 ID（决定「打开」按钮落到哪个入口）。
- **顶层直接写 `type`/`url` 是社区博客的错误写法，飞牛会完全忽略**，于是没有「打开」、也没有桌面图标。

### 6.2 字段表

| 字段 | 说明 |
| --- | --- |
| `title` | 用户看到的入口名称 |
| `icon` | 相对 UI 目录的图标路径，用 `images/icon_{0}.png` |
| `type` | `iframe`（桌面窗口内打开）或 `url`（浏览器标签页 / 外部视图） |
| `protocol` | `http` / `https` / `""`（空 = 系统按当前访问方式自适应） |
| `port` | 服务端口；端口入口用 |
| `url` | 打开路径；端口入口一般 `/`，网关入口用 `/app/<appname>/` |
| `allUsers` | 是否对所有用户可见；`true` 才会生成桌面图标 |
| `gatewayPrefix` | 网关公开路径，如 `/app/notepad`（网关入口用） |
| `gatewaySocket` | **只写 Socket 文件名**，如 `notepad.sock`（网关入口用） |
| `fileTypes` | 文件打开方式支持的扩展名数组 |
| `noDisplay` | 桌面隐藏入口，但保留文件打开方式 |
| `control.accessPerm` | `editable` / `readonly` / `hidden`，入口设置项的可见性 |

### 6.3 端口入口示例

```json
{
  ".url": {
    "notepad.main": {
      "title": "记事本",
      "icon": "images/icon_{0}.png",
      "type": "iframe",
      "protocol": "http",
      "port": "18180",
      "url": "/",
      "allUsers": true
    }
  }
}
```

### 6.4 网关入口示例（HTTPS 桌面必需）

```json
{
  ".url": {
    "notepad.main": {
      "title": "记事本",
      "icon": "images/icon_{0}.png",
      "type": "iframe",
      "protocol": "",
      "gatewayPrefix": "/app/notepad",
      "gatewaySocket": "notepad.sock",
      "url": "/app/notepad/",
      "allUsers": true
    }
  }
}
```

### 6.5 端口入口 vs 网关入口（重要）

| 场景 | 端口入口 | 网关入口 |
| --- | --- | --- |
| HTTP 桌面（`:5666`） | ✅ | ✅ |
| HTTPS 桌面（`:5667`） | ❌ **空白**（iframe 加载 http 属混合内容，被浏览器拦截） | ✅（与桌面同源同协议，复用飞牛证书） |
| 依赖飞牛登录态 | 否 | 是（网关转发用户 Header） |
| 依赖 socket / WebSocket | 否 | 是 |

**结论**：只要飞牛桌面可能以 HTTPS 打开，**桌面主入口就用网关入口**。
网关入口的 `protocol` / `port` 会被忽略，路由只看 `gatewayPrefix` + `gatewaySocket`。

### 6.6 校验清单

- [ ] 是 `.url` 映射，不是扁平对象；
- [ ] 入口 ID 与 `desktop_applaunchname` 一致；
- [ ] `icon` 为 `images/icon_{0}.png`，且 `icon_64.png` / `icon_256.png` 都存在；
- [ ] `type` ∈ {`iframe`, `url`}；
- [ ] 需要 HTTPS 桌面时用网关入口，且 `url` 带结尾斜杠、在 `gatewayPrefix` 下；
- [ ] `gatewaySocket` 只是文件名，socket 确实创建在 `${TRIM_APPDEST}` 下；
- [ ] `allUsers: true`。

## 7. 统一网关规范

1. `ui/config` 里声明网关入口：`gatewayPrefix` + `gatewaySocket` + `url`。
2. 应用在 **`${TRIM_APPDEST}/<gatewaySocket>`** 上监听 **Unix Socket**（本项目：`target/notepad.sock`）。
3. 飞牛 nginx 把 `/app/<appname>/…` 反代到该 socket。
4. 应用需能处理带前缀的请求（剥离前缀），或按 `gatewayPrefix` 自行路由。
5. 飞牛会在转发时带上用户 Header（业务鉴权仍由应用负责）：

   | Header | 含义 |
   | --- | --- |
   | `X-Trim-Userid` | 当前用户 UID |
   | `X-Trim-Isadmin` | 是否管理员 |
   | `X-Trim-Username` | 当前用户名 |

6. socket 权限：nginx worker 可能不是应用用户，需保证可连（本项目 `chmod 0666`）。
7. WebSocket 放在稳定子路径下（如 `/app/<appname>/ws`），复用同一 socket。

> 本项目网关路径固定为 `/app/notepad`，服务端 `-prefix` 默认值与之对应；直连端口路径不带前缀，`stripPrefix` 是 no-op，所以同一份代码三路通用。

## 8. 生命周期脚本规范

### 8.1 文件与职责

| 脚本 | 时机 | 本项目 |
| --- | --- | --- |
| `cmd/main` | `start` / `stop` / `status` | 完整实现 |
| `cmd/install_init` | 落盘前 | 占位 `exit 0` |
| `cmd/install_callback` | 落盘后 | 占位 |
| `cmd/upgrade_init` | 升级前 | 占位 |
| `cmd/upgrade_callback` | 升级后 | 占位 |
| `cmd/uninstall_init` | 卸载前 | 占位 |
| `cmd/uninstall_callback` | 卸载清理后 | 占位 |
| `cmd/config_init` / `cmd/config_callback` | 配置变更前后（可选） | 未用 |

脚本要**可重复执行**（安装/升级/恢复都可能重跑）。

### 8.2 `cmd/main` 退出码

| 动作 | 成功 | 未运行 |
| --- | --- | --- |
| `start` | `0` | 非 0 |
| `stop` | `0` | —— |
| `status` | `0`（运行中） | `3`（未运行） |

### 8.3 环境变量（常用）

| 变量 | 含义 |
| --- | --- |
| `TRIM_APPNAME` / `TRIM_APPVER` | 应用名 / 版本 |
| `TRIM_APP_STATUS` | `INSTALL` / `START` / `UPGRADE` / `STOP` / `UNINSTALL` / `CONFIG` |
| `TRIM_APPDEST` | 载荷目录（`target`） |
| `TRIM_PKGETC` | 配置目录 |
| `TRIM_PKGVAR` | 持久数据目录 |
| `TRIM_PKGTMP` | 临时目录 |
| `TRIM_PKGHOME` | 应用用户数据目录 |
| `TRIM_SERVICE_PORT` | 清单声明的端口 |
| `TRIM_TEMP_LOGFILE` | 写“用户可见错误”的临时日志 |
| `TRIM_USERNAME` / `TRIM_UID` | 应用专用用户 |

### 8.4 两个必踩的坑

1. **别用 `setsid cmd &` 取 `$!` 记 pid**：`setsid` 可能 fork，pid 会记错，`start` 会“假成功”。
   正确写法是让一层 `sh` 先写 pid 再 `exec`：
   ```sh
   sh -c 'echo $$ > "$1"; shift; exec "$@"' _ "$PID_FILE" "$BIN" -addr "0.0.0.0:$PORT"
   ```
2. **失败时把清晰错误写进 `$TRIM_TEMP_LOGFILE`**，否则用户只看到「启动失败」不知道原因。

## 9. 权限与资源

`config/privilege`（运行身份）：

```json
{
  "defaults": { "run-as": "package" },
  "username": "notepad",
  "groupname": "notepad",
  "join-groups": []
}
```

- `run-as: package` 表示用 `username`/`groupname` 定义的专用用户运行。
- 需要的共享目录、Docker、外部运行时等资源在 `config/resource` 声明；本项目为空对象 `{}`。

## 10. 版本、升级与数据

- `version` 三段式 `X.Y.Z`；每次**用户可见变更**都要 bump。
- 同步更新 `changelog`（应用中心可见）和仓库 `CHANGELOG.md`。
- **数据必须放在 `${TRIM_PKGVAR}`**，升级 / 卸载都不丢；不要写在 `target/`（升级会覆盖）。
- 本项目笔记在 `/vol1/@appdata/notepad/notes`，卸载保留。
- 首次安装与“重新上传升级”都会重新解压载荷并重跑 `install_callback`；`upgrade_*` 目前是占位。

## 11. 打包与自检清单

一条命令：

```bash
./scripts/build-fpk.sh          # 产出 dist/notepad-<version>.fpk
```

脚本会：交叉编译三份二进制 → 生成图标 → 组装 `app.tgz` → 打 `fpk` → **自动校验**（缺一项直接失败）：

- `ui/config` 含 `.url` 与 `notepad.main`；
- 入口字段齐全、`icon` 为 `images/icon_{0}.png`、`type` 合法；
- 网关入口的 `gatewaySocket` 以 `.sock` 结尾、`url` 在 `gatewayPrefix` 下；
- `ui/images/icon_64.png` / `icon_256.png` 存在；
- `ICON.PNG` / `ICON_256.PNG` 尺寸严格 64×64 / 256×256。

发布前人工核对：

- [ ] `appname` 小写、`version` 已 bump、`changelog` 已更新；
- [ ] `platform` 与实际二进制匹配（本项目 `all` + 三架构二进制）；
- [ ] `desktop_uidir` / `desktop_applaunchname` 与 `ui/config` 一致；
- [ ] `ui/config` 是 `.url` 映射且主入口是网关入口；
- [ ] 数据目录使用 `${TRIM_PKGVAR}`；
- [ ] `tar -tzf dist/notepad-<version>.fpk` 根目录有 `manifest`（**无 `./` 前缀**）、`app.tgz`、两个图标；
- [ ] `tar -tzf dist/stage/app.tgz` 有 `ui/config` 与 `ui/images/icon_*.png`；
- [ ] 在真机安装，验证：应用中心「打开」、桌面图标、HTTP 桌面、HTTPS 桌面、`/app/notepad/` 网关、直连端口。

## 12. 常见错误对照表

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| 「不是有效的 fpk 文件」 | `platform` 非法，或缺 `appname`/`version` | 改 `platform` 为 `x86`/`arm`/`all` |
| 「解压 app.tgz 失败」 | 载荷不是包根的 `app.tgz` | 重新打包载荷 |
| 有「打开」但没桌面图标 | 图标尺寸/命名不对，或 `allUsers` 非 true | 校验 64/256 尺寸与 `icon_{0}.png` |
| 完全没有「打开」/图标 | `ui/config` 不是 `.url` 映射 | 改为官方结构 |
| HTTPS 桌面一片空白 | 端口入口的 http iframe 被混合内容拦截 | 改用网关入口 |
| 网关页空白/资源 404 | `url` 缺结尾斜杠，相对路径解析错层 | 服务端 301 + 配置带斜杠 |
| 网关 `/app/<名>/` 404 | socket 不存在或名字对不上 | 查 `${TRIM_APPDEST}/<name>.sock` |
| `start` 假成功 | pid 记成 `setsid` 中间进程 | 用 `sh -c 'echo $$ …; exec …'` |
| 应用中心无版本/记录 | 旧结论“本地安装不生成记录”已作废 | 检查 `ui/config` 结构 |

## 13. 参考

- 官方文档首页：<https://developer.fnnas.com/docs/guide/>
- 应用框架：<https://developer.fnnas.com/docs/core-concepts/framework/>
- Manifest：<https://developer.fnnas.com/docs/core-concepts/manifest/>
- 应用入口 `ui/config`：<https://developer.fnnas.com/docs/core-concepts/app-entry/>
- 图标：<https://developer.fnnas.com/docs/core-concepts/icon/>
- 统一网关：<https://developer.fnnas.com/docs/core-concepts/gateway-registration/>
- 环境变量：<https://developer.fnnas.com/docs/core-concepts/environment-variables/>
- fnpack：<https://developer.fnnas.com/docs/cli/fnpack/>
- 本项目实战笔记：[fnos-fpk-guide.md](fnos-fpk-guide.md)
