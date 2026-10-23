# 飞牛 fnOS fpk 打包实战笔记

> 记录本项目（记事本应用）从「上传报错」到「HTTP / HTTPS / 网关三路全通」的完整过程。
> 所有结论均来自对 fnOS 实机的观察 + 官方文档核对，不是博客抄录。
> - 正式规范（照着做就能装成）：[fnos-fpk-spec.md](fnos-fpk-spec.md)
> - 官方文档：<https://developer.fnnas.com/docs/guide/>
>
> **进度**：14.1（本地安装无桌面图标 / 无「打开」）已于 **1.0.11** 解决；**1.0.12** 桌面入口改走网关，修复 HTTPS 桌面空白；**1.0.13** 加了账号登录（第 15 节）。两次修复的完整复盘见第 14 节。

## 0. 先记住这五条

1. `.fpk` 就是 **gzip 压缩的 tar**，包根必须有 `manifest`。
2. `manifest` 是 **INI 风格** `key = value`，**必填 `appname` / `version` / `platform`**。
3. **`platform` 只能写 `x86` / `arm` / `all`**，写成 `x86_64` 会直接被判「不是有效的 fpk 文件」。
4. 应用载荷**必须打成 `app.tgz` 放在包根**，飞牛会把它解压到 `${TRIM_APPDEST}`。
5. 生命周期脚本叫 **`cmd/main`**，接口是 `main start|stop|status`（`0`=成功/运行中，`3`=未运行）。

## 1. fpk 包结构

```
manifest            # 清单，INI，包根必须有
ICON.PNG            # 64x64
ICON_256.PNG        # 256x256
cmd/main            # 生命周期入口
cmd/install_init    # 以及 install_callback / upgrade_init /
cmd/uninstall_init  # upgrade_callback / uninstall_init / uninstall_callback
config/privilege    # 运行身份（JSON）
config/resource     # 资源声明
wizard/             # 安装向导目录，可为空
app.tgz             # 应用载荷，解压到 ${TRIM_APPDEST}
```

## 2. manifest 字段

格式：`key = value`，值可以不加引号，也可以用双引号包起来。**不能带注释行**。多行说明用 `<br>`，可以内嵌 `<a href="...">`。

| 字段 | 说明 |
| --- | --- |
| `appname` | **必填**。应用唯一标识，小写英文字母/数字/连字符，**不能是中文** |
| `version` | **必填**。三段式 `X.Y.Z` |
| `platform` | **必填**。`x86` / `arm` / `all` |
| `display_name` | 中文显示名放这里 |
| `desc` | 简介，支持 HTML（`<br>`、`<a>`） |
| `changelog` | 更新说明 |
| `source` | `official` / `thirdparty`（**不是 `third-party`**） |
| `arch` | 如 `x86_64`，可选 |
| `install_type` | `root`（装到系统盘）或 `volume`（装到存储空间） |
| `min_size` | 最小安装空间（字节） |
| `service_port` / `service_protocol` / `checkport` | 服务端口、协议、是否探活 |
| `desktop_uidir` | 桌面入口的 UI 目录，相对载荷根，一般写 `ui` |
| `desktop_applaunchname` | 启动入口名，形如 `<appname>.main` |
| `desktop_appname` | 另一种写法（与上者二选一，别同时写） |
| `ctl_stop` / `ctl_uninstall` | 是否允许在应用中心停止/卸载 |
| `disable_authorization_path` | 是否禁用授权路径 |
| `os_min_version` | 最低系统版本，如 `1.2.0` |
| `checksum` | 32 位十六进制（MD5），商店包才有 |

**本项目最终清单**（可直接当模板）：

```ini
appname                    = notepad
version                    = 1.0.12
display_name               = 记事本
desc                       = 简介，支持 <br> 和 <a href="...">
changelog                  = 更新说明
source                     = thirdparty
maintainer                 = vanneswong
maintainer_url             = https://github.com/vanneswong
distributor                = vanneswong
distributor_url            = https://github.com/vanneswong
support_url                = https://github.com/vanneswong
platform                   = all
install_type               = volume
min_size                   = 10485760
service_port               = 18180
service_protocol           = http
checkport                  = true
desktop_uidir              = ui
desktop_applaunchname      = notepad.main
ctl_stop                   = true
ctl_uninstall              = true
disable_authorization_path = false
os_min_version             = 1.2.0
```

## 3. 载荷 app.tgz

包根放 `app.tgz`（tar.gz），内容解压后落到 **`${TRIM_APPDEST}`**。单架构包就放一个二进制；想一个包通吃多架构，就放多份、由 `cmd/main` 按 `uname -m` 选。

本项目载荷：

```
notepad-amd64 / notepad-arm64 / notepad-armv7
ui/config                          # 桌面入口配置（见第 6 节）
ui/images/icon_64.png              # 入口图标，严格 64x64（下划线命名）
ui/images/icon_256.png             # 入口图标，严格 256x256
ui/images/icon-64.png, icon-256.png # 连字符版，兼容旧写法
ui/index.html                      # 网关兜底页（可选）
```

## 4. cmd/main 生命周期

飞牛用这三个动作管理应用，**返回码有严格要求**：

```sh
case "${1:-status}" in
  start)  # 启动，成功 0
  stop)   # 停止，成功 0
  status) # 运行中 0，未运行 3
esac
```

飞牛会注入这些环境变量：

| 变量 | 含义 |
| --- | --- |
| `TRIM_APPDEST` | 载荷目录（`app.tgz` 解压到这里） |
| `TRIM_PKGVAR` | 数据目录，升级/卸载都不丢 |
| `TRIM_PKGTMP` | 临时目录 |
| `TRIM_SERVICE_PORT` | 清单里声明的服务端口 |
| `TRIM_TEMP_LOGFILE` | 需要在应用中心展示的日志 |

**坑**：不要用 `setsid cmd &` 直接取 `$!`——`setsid` 可能 fork，pid 会记错，导致 `start` 假成功。正确做法是让一层 `sh` 写 pid 再 `exec`：

```sh
sh -c 'echo $$ > "$1"; shift; exec "$@"' _ "$PID_FILE" "$BIN" -addr "0.0.0.0:$PORT"
```

## 5. config/privilege

```json
{
  "defaults": { "run-as": "package" },
  "username": "notepad",
  "groupname": "notepad",
  "join-groups": []
}
```

## 6. 桌面入口与「打开」按钮

> **2026-10 修正**：本节早先抄了社区博客的写法，**是错的**。正确格式以官方文档
> <https://developer.fnnas.com/docs/core-concepts/app-entry/> 与
> <https://developer.fnnas.com/docs/core-concepts/icon/> 为准。

这三者由**同一套东西**决定，缺一不可：

1. 清单里的 `desktop_uidir = ui` + `desktop_applaunchname = <appname>.main`；
2. 载荷里存在 `app/ui/config`（安装后位于 `${TRIM_APPDEST}/ui/config`）；
3. 载荷里存在 `app/ui/images/icon_64.png` 与 `icon_256.png`，**尺寸必须严格 64×64 / 256×256**。

`ui/config` 的官方结构是 **`.url` 映射**，每个入口一个 ID，入口 ID 必须与
`desktop_applaunchname` 完全一致：

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

关键点（踩过的坑）：

- **必须包在 `".url"` 对象里**，入口 ID 作为 key；顶层直接写 `type`/`url` 飞牛会完全忽略；
- 图标写 `images/icon_{0}.png`（下划线 + `{0}` 占位），飞牛会替换成 `icon_64.png` / `icon_256.png`；
  写成连字符 `icon-64.png` 或在配置里写死尺寸都不对；
- `type` 只有 `iframe`（桌面窗口内打开）与 `url`（浏览器标签页）两种；
- `allUsers: true` 才会对所有用户可见并生成桌面图标；
- **HTTPS 桌面（5667）必须用网关入口**：端口入口（`protocol=http` + `port=18180`）在 HTTP 桌面（5666）能用，
  但在 HTTPS 桌面里 iframe 加载明文 HTTP 属于混合内容，会被浏览器直接拦掉，页面一片空白。
  网关入口用 `gatewayPrefix` + `gatewaySocket` + `url`，`protocol`/`port` 会被忽略，iframe 与桌面同源同协议，
  两种桌面都通；
- `url` 建议保留结尾斜杠（`/app/notepad/`），否则文档 base 变成目录上一层，`assets/…` 会 404。
  同时服务端对不带斜杠的前缀做 301 兜底。

安装后这些信息会落到 PostgreSQL 的 `appcenter` 库：`app_service`（服务卡片，`service_name` 就是入口 ID）与 `app_shortcut`（桌面图标，`type='desktop'`）。

> **结论**：14.1 里「本地安装不生成服务记录」的怀疑是错的——`manual_install = t` 和有没有入口无关，真正的原因是 `ui/config` 结构写错了。改成官方 `.url` 结构后本地安装同样会生成 `app_service` / `app_shortcut`。

## 7. 接入飞牛网关（走飞牛自己的 HTTPS）

约定：应用在 **`${TRIM_APPDEST}/<appname>.sock`** 上监听 unix socket，nginx 把 **`/app/<appname>/`** 反代过去（见 `app_service.gateway_socket` / `gateway_prefix`）。

前端有两个硬要求：

- **所有资源与接口用相对路径**（`assets/…`、`api/…`），否则挂在 `/app/notepad/` 下会 404；
- 服务端要能**剥离 `/app/notepad` 前缀**，这样同一个 handler 既支持网关，也支持直连端口；
- 页面 URL 少了结尾斜杠时自动补 `/`，否则相对路径会解析错层级（服务端 301 + 前端兜底各一层）。

## 8. HTTPS

飞牛的 nginx 监听 `80 / 443 / 5666 / 5667`，配置里用的是 **`ssl_certificate $ssl_server_name;`**（按 SNI 动态取证书），所以别指望从 nginx 配置里读出证书路径。

想让应用自带 HTTPS（如 18181），证书按优先级找：`notepad.env` 指定 → `${TRIM_PKGVAR}/tls/{cert,key}.pem` → 飞牛按域名存放的证书 → 都没有就**自签一张**（能用，但浏览器会提示不受信任）。

## 9. 安装后长什么样

```
/var/apps/<appname>/
├── manifest  ICON.PNG  ICON_256.PNG
├── cmd/      config/   wizard/
├── etc  -> /vol<n>/@appconf/<appname>
├── home -> /vol<n>/@apphome/<appname>
├── meta -> /vol<n>/@appmeta/<appname>
├── target -> /vol<n>/@appcenter/<appname>   # TRIM_APPDEST，载荷在这里
├── tmp  -> /vol<n>/@apptemp/<appname>
└── var  -> /vol<n>/@appdata/<appname>       # TRIM_PKGVAR，数据在这里
```

## 10. 报错对照表

| 现象 | 原因 | 怎么查 |
| --- | --- | --- |
| 「不是有效的 fpk 文件」 | `platform` 取值非法，或缺 `appname`/`version` | `grep -aoE 'platform \(%s\) is invalid\|appname can not be empty\|version can not be empty' /usr/trim/bin/trim_app_center` |
| 「解压 app.tgz 失败」 | 载荷没打成 `app.tgz`（比如直接塞了 `app/` 目录） | 解压 fpk 看根目录 |
| 没有桌面图标 / 没有「打开」 | `app/ui/config` 不是官方 `.url` 结构（最典型），或图标尺寸/命名不对 | 对照第 6 节；查 `appcenter` 库的 `app_service` / `app_shortcut` |
| HTTP 桌面能开、HTTPS 桌面一片空白 | 用的是端口入口，iframe 加载明文 http 被浏览器按混合内容拦截 | 改用网关入口（第 6.5 节） |
| 网关页空白 / 资源 404 | 入口 `url` 缺结尾斜杠，相对路径解析错层 | 服务端 301 + 入口 `url` 带 `/` 结尾 |
| 网关 `/app/<名>/` 404 | 应用没在 `<appname>.sock` 上应答 | 看 `${TRIM_APPDEST}/<appname>.sock` 是否存在 |
| `start` 假成功 | pid 记成了 `setsid` 的中间进程 | 用 `sh -c 'echo $$ …; exec …'` 写法 |

## 11. 拿不到文档时怎么逆向

这套方法在本项目里极其有效，几乎每一步都是靠它破的：

```bash
B=/usr/trim/bin/trim_app_center        # 应用中心后台程序

# 1) 系统里没装 strings 时，用 grep 当 strings 用
sudo grep -aoE '[[:print:]]{4,}' "$B" | sort -u | head -50

# 2) 直接挖 Go 结构体标签 —— 等于拿到字段名和格式（json/yaml/ini）
sudo grep -aoE '(json|yaml|toml|ini|gorm):"[^"]+"' "$B" | sort -u

# 3) 挖报错文案，顺着文案就能找到校验逻辑
sudo grep -aoE '[[:print:]]{0,40}(can not be empty|invalid|not exist)[[:print:]]{0,60}' "$B" | sort -u

# 4) 挖 API 路由
sudo grep -aoE '/app-center/[a-zA-Z0-9/_:.-]*' "$B" | sort -u

# 5) 已安装的应用就是"装完后的标准答案"
sudo find /var/apps/<某个商店应用> -maxdepth 3
sudo cat /var/apps/<应用>/manifest

# 6) 日志
sudo ls /usr/trim/logs/
sudo tail -100 /var/log/trim_app_center/info.log
```

状态都在 **PostgreSQL 15** 里（库 `appcenter`、`trim_sac` 等）。查宽表要截断，否则一行几万字符会把终端冲爆：

```bash
sudo -u postgres psql -d appcenter -c '\d app'
sudo -u postgres psql -d appcenter -x -c "SELECT * FROM app_service;" | cut -c1-160
sudo -u postgres psql -d appcenter -c "SELECT * FROM app_shortcut;"
```

## 12. 本项目的最终命令速查

```bash
make run                  # 本地启动
make test                 # 单元测试
./scripts/build-fpk.sh    # 打包成 dist/notepad-<version>.fpk

# 校验产物（打包后必做）
tar -tzf dist/notepad-1.0.12.fpk                    # 包根应有 manifest / app.tgz / 图标
tar -tzf dist/stage/app.tgz                         # 载荷里应有 ui/config + ui/images/*
tar -xzOf dist/notepad-1.0.12.fpk manifest | head   # 确认 platform / desktop_* 字段
```

## 13. 教训

- **看懂报错比猜快**：从「不是有效的 fpk」到「解压 app.tgz 失败」，每次报错推进一个阶段，顺着走就不会迷路。
- **先对官方文档，再谈逆向**：14.1 就是只看数据库、没对文档，得出了错误假设，绕了一圈。官方 <https://developer.fnnas.com/docs/guide/> 已经把 `ui/config` 结构写得很清楚。
- **已安装的应用是最好的文档**：`/var/apps/*/manifest` 比任何博客都准。
- **二进制里什么都有**：字段名、校验规则、API 路由，全在 `strings` 里。
- **别用 `.` 打包**：`tar -czf out .` 会产生 `./manifest` 这种带前缀的条目，有的校验器按名字精确匹配 `manifest`，会直接判无效；应当显式列出条目。
- **相对路径是接入网关的前提**，写死 `/assets/...` 就只能在根路径用。
- **HTTPS 桌面必须用网关入口**：端口入口的 http iframe 在 HTTPS 页面里会被混合内容拦截，表现为空白。
- **能本地模拟的别等真机**：unix socket + `curl --unix-socket` 就能把网关路径、前缀剥离、结尾斜杠全验一遍。

## 14. 两次修复复盘（1.0.11 / 1.0.12）

> 这一节记录两处桌面入口问题的完整排查过程与修法。方法论、字段表和清单见规范：
> [fnos-fpk-spec.md](fnos-fpk-spec.md)。

### 14.1 1.0.11：本地安装没有桌面图标 / 没有「打开」按钮

**版本**：1.0.10 → 1.0.11

**现象**

- 应用中心里「记事本」只有「停用 / 卸载」，没有「打开」；飞牛桌面上也没有图标。
- 同为第三方应用的「俄罗斯方块」「Talebook」既有「打开」也有桌面图标，只有我们是手动上传安装的。

**排查过程（走过的弯路也要记下来）**

1. 一开始怀疑是**安装路径差异**：查 PostgreSQL（库 `appcenter`）发现其余应用 `app.manual_install = f`，
   只有 notepad 是 `t`，且 `app_service` 里没有 notepad 的记录、`app_shortcut` 全表只有 Talebook 一行。
   于是得出「本地安装不生成服务记录」的假设——**这个方向是错的**。
2. 回头去读**官方文档**，重点看「应用入口」和「图标」两页，拿到 `ui/config` 的正式结构。
3. 与项目现有配置逐字段对比，发现结构**根本不一致**：我们写的是社区博客的扁平对象，
   官方要的是 `".url"` 映射。问题一下就锁定了。

**根因**

`app/ui/config` 写成了：

```json
{
  "type": "url",
  "url": "http://${host}:18180/",
  "icon": "images/icon-64.png",
  "icon256": "images/icon-256.png"
}
```

- 没有 `".url"` 外层，飞牛解析不到任何入口；
- 没有入口 ID，无法与 `desktop_applaunchname` 对应；
- 图标写成连字符 `icon-64.png`，官方约定是 `icon_{0}.png` → `icon_64.png` / `icon_256.png`。

**修改（前后对照）**

```diff
-{
-  "type": "url",
-  "url": "http://${host}:18180/",
-  "icon": "images/icon-64.png",
-  "icon256": "images/icon-256.png"
-}
+{
+  ".url": {
+    "notepad.main": {
+      "title": "记事本",
+      "icon": "images/icon_{0}.png",
+      "type": "iframe",
+      "protocol": "http",
+      "port": "18180",
+      "url": "/",
+      "allUsers": true
+    }
+  }
+}
```

涉及文件：

- `packaging/app/ui/config`：改成官方 `.url` 结构；
- `scripts/build-fpk.sh`：图标改名为下划线版（保留连字符兜底），并加入打包期校验；
- `packaging/manifest` / `CHANGELOG.md` / `README.md`：版本与说明。

**验证**

- 打包脚本新增的校验会检查 `.url`、入口 ID、必备字段、图标文件与尺寸；
- 装到真机后，应用中心出现「打开」、桌面出现图标，`app_service` / `app_shortcut` 生成。

**教训**

- 「本地安装不生成服务记录」的错误假设，来自只看数据库、没对文档。**先对官方文档，再谈逆向。**
- `manual_install` 与有没有入口无关；数据库只是结果，不是原因。

<details>
<summary>附：临时给旧包补记录（不推荐，正路是装新包）</summary>

```sql
-- 备份：sudo -u postgres pg_dump -d appcenter -t app_service -t app_shortcut > /tmp/shortcut_bak.sql
-- 19 换成 SELECT id FROM app WHERE app_name='notepad'; 的结果
BEGIN;
INSERT INTO app_service (
  app_id, service_name, title, desc, icon, type, url, default_url,
  is_admin, control, full_url, no_display, gateway_socket, gateway_prefix,
  feature, file_types, i18n_matadata, created_at, updated_at)
SELECT 19, 'notepad.main', '记事本', '在线记事本', 'ui/images/icon_{0}.png', 'iframe',
       '/', '/',
       false, control, '', false, '', '',
       feature, file_types, '{"zh-CN":{"Title":"记事本","Desc":"在线记事本"}}',
       now(), now()
FROM app_service WHERE id = 9
  AND NOT EXISTS (SELECT 1 FROM app_service WHERE app_id = 19);

INSERT INTO app_shortcut (app_id, service_id, type, created_at, updated_at)
SELECT 19, id, 'desktop', now(), now() FROM app_service
WHERE app_id = 19 AND NOT EXISTS (SELECT 1 FROM app_shortcut WHERE app_id = 19);
COMMIT;

-- 回滚：DELETE FROM app_shortcut WHERE app_id=19; DELETE FROM app_service WHERE app_id=19;
```

</details>

### 14.2 1.0.12：HTTPS 桌面打开一片空白

**版本**：1.0.11 → 1.0.12

**现象**

- 在 `http://192.168.10.232:5666/`（飞牛 HTTP 桌面）点图标能正常打开、运行。
- 在 `https://fn.myminibase.com:5667/`（飞牛 HTTPS 桌面）点图标是**一片空白**。

**排查过程**

1. 同一条入口，只有协议不同 → 先怀疑**混合内容**。1.0.11 的入口是
   `type=iframe` + `protocol=http` + `port=18180`，飞牛会把 iframe 指向 `http://<host>:18180/`。
2. HTTPS 页面里嵌 HTTP iframe，浏览器按 Mixed Content 直接拦截，iframe 空白——与现象吻合。
3. 结论：端口入口**天生不兼容 HTTPS 桌面**。要么给应用配受信任 HTTPS（自签会被 iframe 拦），
   要么走飞牛**统一网关**：让 iframe 与桌面同源同协议，复用飞牛自己的证书。

**根因**

桌面主入口使用端口入口。端口入口在 HTTP 桌面正常，在 HTTPS 桌面因混合内容被拦截。

**修改**

1. `packaging/app/ui/config` 主入口改为网关入口：

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

2. `internal/server/server.go`：网关前缀不带结尾斜杠时 301 跳到带斜杠版本。
   否则页面 base 变成上一层目录，相对路径 `assets/…` 会解析到应用外，同样是一片空白。

```go
case p == prefix:
    if r.Method == http.MethodGet || r.Method == http.MethodHead {
        u := *r.URL
        u.Path = prefix + "/"
        http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
        return
    }
    r.URL.Path = "/"
```

3. `internal/server/server_test.go`：新增 `/app/notepad` → 301 `/app/notepad/` 的用例。
4. `scripts/build-fpk.sh`：校验逻辑识别网关入口（`gatewaySocket` 以 `.sock` 结尾、`url` 在 `gatewayPrefix` 下）。

应用侧本来就已经实现了 unix socket 监听（`cmd/main` 里 `-unix ${TRIM_APPDEST}/notepad.sock`）和网关前缀剥离，
所以这次**不需要动运行逻辑，只动入口配置 + 补一个斜杠重定向**。

**验证（本地模拟网关，无需真机）**

用构建出的二进制直接监听 unix socket，再用 `curl --unix-socket` 走一遍：

```bash
./dist/stage/payload/notepad-amd64 -addr "" -tls-addr "" \
  -unix /tmp/np.sock -prefix /app/notepad -data /tmp/npdata &

curl -s -o /dev/null -D - --unix-socket /tmp/np.sock http://localhost/app/notepad   # 301 → /app/notepad/
curl -s -o /dev/null -w '%{http_code}\n' --unix-socket /tmp/np.sock http://localhost/app/notepad/          # 200
curl -s -o /dev/null -w '%{http_code}\n' --unix-socket /tmp/np.sock http://localhost/app/notepad/assets/app.css  # 200
curl -s --unix-socket /tmp/np.sock http://localhost/app/notepad/api/info           # 200 JSON
kill %1
```

结果：

| 请求 | 结果 |
| --- | --- |
| `GET /app/notepad` | 301 → `/app/notepad/` |
| `GET /app/notepad/` | 200（index） |
| `GET /app/notepad/assets/app.css` | 200 |
| `GET /app/notepad/api/info` | 200 JSON |
| `GET /`（直连端口） | 200（未受影响） |

**教训**

- **只要飞牛桌面可能用 HTTPS 打开，桌面主入口就用网关入口**，不要用端口入口。
- 网关页面的 `url` 要带结尾斜杠；服务端再加一层 301 兜底。
- 能本地模拟的（unix socket + curl）就本地验证，别什么都等真机。

### 14.3 发布 / 升级操作方法

两次修复都按同一套流程发布，下面是标准动作。

**1) 本地准备**

```bash
# 改 packaging/manifest： bump version + 写 changelog
# 改 CHANGELOG.md
make test && make vet          # 回归
./scripts/build-fpk.sh         # 生成 dist/notepad-<version>.fpk，内含自动校验
```

**2) 产物自检（打包后必做）**

```bash
tar -tzf dist/notepad-<version>.fpk                  # 根应有 manifest / app.tgz / ICON.PNG / ICON_256.PNG
tar -tzf dist/stage/app.tgz                          # 应有 ui/config 与 ui/images/icon_*.png
tar -xzOf dist/notepad-<version>.fpk manifest | grep -E 'version|platform|desktop'
tar -xzOf dist/stage/app.tgz ui/config               # 确认 .url 结构
```

**3) 真机安装 / 升级**

- 应用中心 → 手动安装 → 上传 `dist/notepad-<version>.fpk`。
- 升级（覆盖安装）同样是上传新包；数据在 `${TRIM_PKGVAR}`，不会丢。
- 安装后重新启动应用，让 `cmd/main start` 重建 socket。

**4) 真机验证清单**

- [ ] 应用中心出现「打开」，可正常打开；
- [ ] 飞牛桌面出现图标；
- [ ] HTTP 桌面（`:5666`）点图标正常；
- [ ] HTTPS 桌面（`:5667`）点图标正常（本次重点）；
- [ ] `http://<NAS>:18180/` 直连正常；
- [ ] 网关 `https://<域名>:<HTTPS端口>/app/notepad/` 正常；
- [ ] 若装了 18181：`https://<域名>:18181/` 正常。

**5) 需要时可核对数据库**

```bash
sudo -u postgres psql -d appcenter -x -c \
  "SELECT id, app_id, service_name, type, url, gateway_socket, gateway_prefix FROM app_service WHERE app_id=(SELECT id FROM app WHERE app_name='notepad');"
sudo -u postgres psql -d appcenter -c \
  "SELECT * FROM app_shortcut WHERE app_id=(SELECT id FROM app WHERE app_name='notepad');"
```

**6) 出问题怎么快速回滚**

- 应用中心卸载后，重新上传上一个版本的 fpk 即可（数据在 `${TRIM_PKGVAR}`，保留）。
- 若手改过 `app_service` / `app_shortcut`，用 `pg_dump` 备份恢复。

### 14.4 其他待办

| # | 问题 | 现状 / 下一步 |
| --- | --- | --- |
| 1 | `ui/config` 官方字段表 | **已确认**（2026-10）：必须包在 `.url` 里，字段见规范第 6 节 |
| 2 | `platform` 合法取值全集 | 只实测过 `x86` / `all`；ARM 机型（`arm` / `arm64` / `armv7`）未验证。当前用 `platform=all` + 包内三架构二进制绕过，**但没在 ARM 机器上跑过** |
| 3 | `checksum` 字段 | 商店包都有（32 位十六进制，疑似 MD5），本地安装没有也能装；校验对象与算法未确认 |
| 4 | 18181 的浏览器证书警告 | 现用自签证书，必然提示"不受信任"。想消除需给 `fn.myminibase.com` 一张受信任证书（飞牛证书管理申请或导入），放进 `${TRIM_PKGVAR}/tls/`。"复用飞牛证书"这条路走不通——其 nginx 用 `ssl_certificate $ssl_server_name` 动态取证书，磁盘上也没有该域名的证书文件 |
| 5 | 卸载是否真的保留数据 | 设计上笔记在 `${TRIM_PKGVAR}/notes`（`/vol1/@appdata/notepad/notes`），**尚未实测**飞牛卸载流程会不会清掉；有机会应实测一次 |
| 6 | 升级流程 | `upgrade_init` / `upgrade_callback` 目前是 `exit 0` 占位，从未走过真正的"覆盖升级"（一直是重新上传安装） |
| 7 | `app_service.control` 完整 JSON | 查询输出被截断，只看到前 160 字符；字段含义（`show`/`port`/`path`/`fullUrl` 等权限位）未弄懂 |
| 8 | 网关前缀是否被剥离 | 官方文档说应用应按 `gatewayPrefix` 路由；本项目两边都兼容（剥不剥都能用）。真机上 nginx 到底传哪种路径，值得确认后写死以简化代码 |
| 9 | 网关登录态 | 还没有读取 `X-Trim-Userid` / `X-Trim-Username`。1.0.13 用的是应用自己的账号（`users.json` + 会话 Cookie）；若未来要按飞牛用户区分笔记，需要接这个 |

### 14.5 建议的下一步

1. 在真机上重新安装 `dist/notepad-1.0.13.fpk`，逐条勾完 14.3 的验证清单；
2. 找一台 ARM 飞牛实测 `platform=all` 通用包（待办 2）；
3. 实测一次卸载，确认 `${TRIM_PKGVAR}/notes` 是否保留（待办 5）；
4. 若要做多用户，接入网关用户 Header（待办 9）。

## 15. 1.0.13：加登录功能时踩到的点

### 15.1 账号放在哪

- 账号文件是 `${TRIM_PKGVAR}/users.json`，**故意放在 `notes/` 的上一级**：既和笔记同在一个"卸载/升级都不碰"的目录里，又不会被 `internal/store` 的文件列举看到（它的 `List()` 会把笔记目录里的每个普通文件都当笔记，`users.json` 若放在里面就会出现在笔记列表里）。
- 二进制里 `-users` 默认值由 `-data` 推导（`<data>/../users.json`），所以以前那种 `notepad -data /opt/notepad/data` 的裸跑方式不用改参数也能用。
- 文件权限 `0600`、写入走"临时文件 + rename"的原子替换，和笔记落盘同一套路子。

### 15.2 首次安装建账号，怎么防止被人抢先注册

`POST /api/setup` 只在"一个账号都没有"时可用，第一次成功后立刻返回 409。也就是说：应用暴露到公网、管理员还没登录过的那段时间，谁先打开谁就能建账号——如果介意这一点，**装完先在局域网里把账号建好再开端口转发**。

### 15.3 会话 Cookie 的作用域

应用有两个入口（`http://<NAS>:18180/` 和网关 `https://<域名>:<端口>/app/notepad/`），同一套 handler 两边都要能用：

- 直连时 Cookie `Path=/`；
- 走网关时 `Path=/app/notepad/`，同一台 NAS 上的其他应用就看不到这个 Cookie。

实现方式：`stripPrefix` 中间件在剥前缀之前把前缀塞进 `context`（`cookieScopeKey`），签发/清除 Cookie 时再取出来算 `Path`。如果哪天要加新的 Cookie，记得走 `cookiePath()`，别写死 `/`。

`Secure` 只在 `r.TLS != nil` 时加：走网关时请求是从 nginx 经过 unix socket 打进来的，服务端看不到 TLS，硬加 `Secure` 反而会让 HTTP 桌面下登不上。

### 15.4 忘记密码怎么办

没有"找回密码"通道（也没接邮件），直接删掉账号文件再重启应用，就会重新回到建账号页：

```bash
sudo systemctl stop notepad 2>/dev/null || true
sudo /srv/.../cmd/main stop            # 或应用中心里停止应用
sudo rm /vol1/@appdata/notepad/users.json
# 再启动应用，打开界面重新创建账号；notes/ 里的笔记不受影响
```

### 15.5 真机验证清单（登录相关）

- [ ] 全新安装后打开界面 → 是「创建账号」页（不是登录页）；
- [ ] 建完账号直接进入应用，能看到原有笔记；
- [ ] 退出应用（关掉浏览器/换个浏览器）后再打开 → 是登录页，勾了「记住账号密码」时账号密码已填好；
- [ ] 左下角齿轮能打开设置，能看到账号 / 版本 / 数据目录；
- [ ] 设置 → 修改密码：当前密码错 → 有中文提示；改成功后用新密码能登录、旧密码不行；
- [ ] 改完密码后，另一台已登录的设备刷新页面 → 回到登录页（会话被踢）；
- [ ] 设置 → 退出登录 → 回到登录页，且本地保存的密码已被清掉；
- [ ] 未登录状态下直接访问 `https://<域名>:<端口>/app/notepad/api/notes` → 401；
- [ ] 连续输错 5 次密码 → 第 6 次提示"密码错误次数过多，请 N 秒后再试"（429）；
- [ ] 升级安装（不删 users.json）后，原账号仍可登录，笔记不丢。
