# runner-home：更新系统的专用 git 凭据家目录

这是 Web 触发更新（go/shell 引擎 runner 容器）与宿主机手工 `update.sh` 共用的
**最小 git 凭据目录**（R2/R17）：只放一把 GitHub deploy key + known_hosts +
最小 .gitconfig，**不放任何用户家目录内容**（避免把全部私钥/其他主机凭据挂进
持有 docker.sock 的 runner 容器）。

默认值：`UPDATE_GIT_HOME=<REPO_ROOT>/.hermes/runner-home`（代码内建默认，
`.env` 可显式覆盖）。runner 容器以只读方式挂载本目录的 `.gitconfig` 与 `.ssh/`
并设 `HOME=` 本目录。

## 目录结构

```
runner-home/
├── .gitconfig          # 最小 git 配置（随仓库分发，非机密）
└── .ssh/
    ├── id_ed25519.example  # 私钥占位模板（随仓库分发，非真实 key）
    ├── id_ed25519          # ← 真实 deploy key（运维放入，已被 .gitignore 排除）
    └── known_hosts         # github.com 主机公钥（可自动积累）
```

## gascell 首次配置步骤

```bash
cd /home/gascell/hiaf-lab-system/.hermes/runner-home

# 1. 生成专用 deploy key（无口令；server/runner 容器无法交互输口令）
ssh-keygen -t ed25519 -f .ssh/id_ed25519 -N "" -C "gascell-lab-deploy"

# 2. 公钥加入 GitHub 仓库 Settings → Deploy keys（只读即可）
cat .ssh/id_ed25519.pub

# 3. 采集 github.com 主机公钥（在能出 GitHub 的机器上执行，或先信任首连自动写入）
ssh-keyscan -t ed25519,ecdsa,rsa github.com >> .ssh/known_hosts

# 4. 属主对齐 runner 运行用户（compose 的 UPDATE_RUN_UID/GID，默认 1000:1000）
sudo chown -R 1000:1000 .
chmod 700 .ssh && chmod 600 .ssh/id_ed25519
```

`.env` 中无需显式配置 `UPDATE_GIT_HOME`（默认即本目录）；如需指向别处，设为
**宿主机绝对路径**。

## 自检

配置完成后触发一次更新即可验证：流水线步骤 1（预检）会执行
`ssh -o BatchMode=yes -T git@github.com` 凭据自检——认证成功日志为
「SSH 凭据自检通过」，失败会给出上面同样的配置指引并中止。

宿主机手工验证：

```bash
HOME=$PWD/.hermes/runner-home ssh -o BatchMode=yes -T git@github.com
# 期望 stderr: Hi <user>! You've successfully authenticated, ... （退出码 1 是正常的）
```

## 安全说明

- 真实私钥 `id_ed25519` 被 `.gitignore` 排除，绝不入库；只有 `.example` 占位与
  `known_hosts`（公钥，非机密）随仓库分发。
- 本目录被 runner 容器只读挂载，key 权限须 600 且属主为 UPDATE_RUN_UID。
