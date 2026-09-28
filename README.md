# edgit

GitHub 仓库镜像加速克隆工具。一条命令，自动探测可用镜像站并加速 `git clone`，克隆完成后自动还原 remote，日常 `git pull` / `git push` 完全不受影响。

```bash
edgit clone https://github.com/algerkong/AlgerMusicPlayer.git --depth 1
```

## 特性

- **镜像加速**：内置多个镜像站，按优先级并行探测「站点连通性 + 仓库可用性」，命中即从镜像克隆；全部未命中自动回落原始 GitHub 地址
- **对 git 零侵入**：不修改任何 git 配置（不使用 insteadOf），只有主动敲 `edgit` 才走镜像，日常 `git clone` / `git pull` 原样走 GitHub
- **remote 自动还原**：克隆完成后 `remote origin` 自动还原为原始 GitHub 地址，后续 pull / push 直连 GitHub
- **极速探测**：死站点 ~0.2s 淘汰（TCP 连通性预检，连不上不跑 git）；镜像并行探测 + 3 秒宽限择优；命中结果缓存 10 分钟，期间克隆零探测
- **git 参数透传**：`--depth 1`、`-b main` 等参数原样传给 `git clone`
- **零依赖单文件**：纯 Go 标准库实现，无任何第三方依赖，下载一个可执行文件即可使用

## 安装

下载对应平台产物（`dist/` 或 Release），放进 PATH 目录即可。系统需已安装 git ≥ 2.x。

| 平台 | 产物 |
|---|---|
| Windows | `edgit-windows-amd64.exe` / `edgit-windows-arm64.exe` |
| Linux | `edgit-linux-amd64` / `edgit-linux-arm64` |
| macOS | `edgit-darwin-amd64` / `edgit-darwin-arm64` |

**Windows**：改名为 `edgit.exe` 放进任意 PATH 目录（如 `D:\IDE\edgit`），并把目录加入环境变量 Path。

**Linux / macOS**：

```bash
chmod +x edgit-linux-amd64
sudo mv edgit-linux-amd64 /usr/local/bin/edgit
```

验证安装：

```bash
edgit version
```

## 快速开始

可选的初始化向导（展示镜像站、自动补 PATH、赞助信息；不运行也可直接使用）：

```bash
edgit init
```

镜像加速克隆，git 参数原样透传：

```bash
edgit clone https://github.com/algerkong/AlgerMusicPlayer.git --depth 1
edgit clone https://github.com/xxx/yyy.git -b main
```

SSH 地址同样支持：

```bash
edgit clone git@github.com:xxx/yyy.git
```

Release 附件下载加速（高校镜像优先）：

```bash
edgit get https://github.com/VSCodium/vscodium/releases/download/1.135.06055/VSCodium-1.135.06055-x64.exe
edgit get https://github.com/xxx/yyy/releases/download/v1.0/tool.zip out.zip   # 指定输出文件名
```

非 github.com 地址自动透传给 `git clone`，不走镜像：

```bash
edgit clone https://gitlab.com/xxx/yyy.git   # 原样透传
```

克隆完成后验证 remote 已还原：

```bash
git remote -v
# origin  https://github.com/algerkong/AlgerMusicPlayer.git (fetch)
# origin  https://github.com/algerkong/AlgerMusicPlayer.git (push)
```

## 镜像管理

```bash
edgit mirrors list                       # 列出全部镜像
edgit mirrors add https://gh.example.com/           # 添加镜像（代理前缀型）
edgit mirrors add 'https://gh.example.com/{owner}/{repo}.git'  # 或 URL 模板
edgit mirrors disable gh-proxy.com       # 禁用
edgit mirrors enable gh-proxy.com        # 启用
edgit mirrors remove gh-proxy.com        # 删除
```

内置镜像按**权威性**排序（知名高校镜像在前，其次经久不衰、社区口碑好的镜像，全部可增删启停）：

| 优先级 | 镜像 | clone | release | 说明 |
|---|---|---|---|---|
| 1 | TUNA（清华） | — | ✓ | 高校镜像，仅镜像 Release 文件 |
| 2 | USTC（中科大） | — | ✓ | 高校镜像，仅镜像 Release 文件 |
| 3 | gitclone.com | ✓ | — | 2015 年起专做 git clone 加速，口碑最老牌 |
| 4 | gh-proxy.com | ✓ | ✓ | hunshcn/gh-proxy 开源项目官方站 |
| 5 | ghproxy.net | ✓ | ✓ | ghproxy 系老牌域名 |
| 6 | ghfast.top | ✓ | ✓ | 各验证清单长期收录 |
| 7 | moeyy.xyz | ✓ | ✓ | moeyy 开源项目公共服务 |
| 8 | gh.llkk.cc | ✓ | ✓ | 社区镜像 |

> 高校镜像（TUNA/USTC）只镜像 Release 文件和特定仓库，不支持任意仓库的 git clone，因此仅参与 Release 下载（`edgit get`）；clone 按 1→3→8 中支持 clone 的镜像探测。镜像站列表会随时间失效，可通过 `mirrors add` / `remove` 自行维护。

## 工作原理

```
edgit clone <url> [git参数...]
  1. 解析 URL：github.com 的 HTTPS/SSH 走镜像；其他地址直接 git clone 透传
  2. 并行探测所有启用的镜像：
     Stage 1  连通性预检   TCP 拨号镜像站:443（2~3s）→ 连不上立即淘汰，不跑 git
     Stage 2  仓库探测     git ls-remote --symref <镜像URL> HEAD（8s）
  3. 优先级择优：选优先级最高的命中者；高优先级镜像慢/半死时，
     给 3 秒宽限期，超时即用已命中的镜像
  4. git clone <镜像URL> [参数] → git remote set-url origin <原GitHub地址>
  5. 全部未命中 → git clone <原GitHub地址>
```

命中缓存：最近命中的镜像缓存 10 分钟（`~/.edgit/cache.json`），期间克隆直接使用、跳过探测；镜像被禁用或过期后自动失效。

## 配置

- 配置文件：`~/.edgit/config.json`（首次 `init` 或 `mirrors add/remove/disable` 时生成，未生成时使用内置镜像）
- 命中缓存：`~/.edgit/cache.json`（自动管理，无需手动编辑）

## 从源码构建

需 Go ≥ 1.22，零第三方依赖：

```bash
git clone <本仓库>
cd edgit
go build -o edgit.exe .        # Windows
go build -o edgit .            # Linux / macOS
```

交叉编译全部平台：

```bash
GOOS=linux   GOARCH=amd64 go build -ldflags '-s -w' -o dist/edgit-linux-amd64 .
GOOS=darwin  GOARCH=arm64 go build -ldflags '-s -w' -o dist/edgit-darwin-arm64 .
# ... 其余同理
```

## 许可证

本项目采用 **CC BY-NC 4.0（署名-非商业）** 标准许可证（全文见 [LICENSE](LICENSE)）：

- ✅ 个人/非商业用途自由使用、修改、分发（源码或二进制）
- ⚠️ 须署名原作者，注明修改，标注本许可证
- 🚫 **商业使用须事先取得作者书面授权**（NC 条款禁止商业使用，商用请通过项目仓库联系作者获取单独授权，类似 Qt/MySQL 双许可模式）

完整条款以 [LICENSE](LICENSE) 中的英文原文为准。

## 常见问题

**克隆很慢？** 首次克隆需探测镜像（并行，通常几秒内）；命中后 10 分钟内克隆零探测。也可用 `mirrors disable` 关掉慢镜像调整优先级。

**某个镜像站挂了？** `edgit mirrors disable <id>` 禁用，或 `remove` 后 `add` 新镜像站。

**影响我平时用 git 吗？** 不影响。edgit 不修改 git 配置，只有主动调用 `edgit` 命令才生效。

**克隆后 push 会推到镜像站吗？** 不会。remote 已自动还原为 GitHub 地址。
