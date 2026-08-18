# 用户自助注册与邀请码方案

> 状态：待评审  
> 范围：Go `auth` 模块、现有登录页与管理员用户管理页  
> 依据：`AGENTS.md`、`docs/api-contract.md`、`docs/permission-audit.md`、`web-ui/docs/user-member-management.md` 与当前代码  
> 迁移基线：仓库当前最新迁移为 038；实现时使用下一个空闲序号（当前为 039）

## 1. 结论

采用一次性随机邀请码作为自助注册的第二道准入门：

- `ALLOW_REGISTER=false`：自助注册整体关闭，邀请码不生效，继续返回 `registration_disabled`。
- `ALLOW_REGISTER=true`：开放注册入口，但注册请求仍必须携带一个有效邀请码。
- 邀请码必须同时满足：存在、未使用、未过期、未撤销；成功注册后立即绑定新用户并变为已使用。
- 管理员创建、查看、撤销邀请码；所有管理端点仅允许系统 `admin`。
- 邀请码使用 `crypto/rand` 生成 32 字节随机数并以 base64url（无 padding）编码，约 43 字符；数据库只保存 SHA-256 摘要，不保存明文。
- 完整邀请码只在创建成功响应中返回一次。列表只显示前缀和状态，刷新后不能再次取得完整码。
- 注册用户固定为现有默认系统角色 `member`，邀请码不携带角色或项目权限；注册后加入项目继续走既有成员管理流程。

不新增独立 Go 模块、前端路由、Pinia store、依赖、邮件发送或批量邀请码。邀请码属于认证域，直接扩展现有 `go-server/auth`；管理界面直接放入 `AdminUsersView.vue`。

## 2. 需求与边界

### 2.1 用户故事

| 身份 | 操作 | 期望结果 |
|---|---|---|
| admin | 生成邀请码并设置有效期 | 获得一次性明文，可立即复制并私下发送 |
| admin | 查看邀请码列表 | 看到前缀、状态、生成者、使用者和时间，不看到完整码 |
| admin | 撤销未使用的邀请码 | 邀请码立即失效，后续注册被拒绝 |
| 被邀请人 | 用有效邀请码注册 | 账号创建，邀请码同时标记为已使用，随后按现有流程自动登录 |
| 外部人员 | 无码或猜码注册 | 请求被拒绝，无法探测邀请码状态或用户名是否存在 |
| 非 admin 登录用户 | 调用邀请码管理 API | 后端返回 403，前端不展示管理入口 |

### 2.2 `ALLOW_REGISTER` 与邀请码的关系

两个条件采用串联的 AND 语义：

```text
允许自助注册 = ALLOW_REGISTER == "true" AND 邀请码有效
```

处理顺序固定为：

1. 先检查 `ALLOW_REGISTER`；关闭时直接返回 403 `registration_disabled`，不解析或查询邀请码。
2. 开启时继续执行现有 IP 限流。
3. 再校验请求字段、密码和邀请码；邀请码缺失或无效均拒绝。
4. 管理员通过 `/api/v1/admin/users` 创建账号不受 `ALLOW_REGISTER` 或邀请码影响。

`ALLOW_REGISTER` 决定是否开放自助入口，邀请码决定谁能通过入口。生产启用邀请制时设置 `ALLOW_REGISTER=true`；紧急关闭注册只需恢复为 `false`，无需批量撤销邀请码。

### 2.3 API 兼容边界

- 保留现有 `POST /api/v1/auth/register` 路径、HTTP 方法、成功响应 `UserInfo` 和注册后登录流程。
- 注册请求从 `{username, password}` 增量增加必填字段 `invitation_code`。这是实现强制邀请制所必需的请求体收紧，不新建第二套注册端点。
- 登录、管理员建号、用户管理和项目成员 API 均保持原契约。
- 未升级的注册客户端在 `ALLOW_REGISTER=true` 时会收到 400 `invitation_code_required`；部署时必须同步发布前后端。

### 2.4 非目标

- 不发邀请邮件、短信或 ntfy，不在系统中保存收件人地址。
- 不支持多次使用、批量生成、永久有效、指定角色或自动加入项目。
- 不提供公开的邀请码检查接口；有效性只在最终注册请求中判断。
- 不增加“读取 `ALLOW_REGISTER`”的公开配置 API；登录页继续保留注册入口，并根据后端错误提示实际状态。
- 不让 AI Agent 创建、撤销或使用邀请码。

## 3. 数据模型与迁移

### 3.1 表结构

新增 `invitation_codes`，归属 `auth` 模块：

```sql
-- migrations/039_invitation_codes.up.sql
CREATE TABLE invitation_codes (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    code_hash   TEXT        NOT NULL,
    code_prefix TEXT        NOT NULL,
    created_by  UUID        NOT NULL REFERENCES users(id),
    used_by     UUID        REFERENCES users(id),
    expires_at  TIMESTAMPTZ NOT NULL,
    used_at     TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    revoked_by  UUID        REFERENCES users(id),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT invitation_codes_hash_unique UNIQUE (code_hash),
    CONSTRAINT invitation_codes_use_pair CHECK (
        (used_at IS NULL AND used_by IS NULL) OR
        (used_at IS NOT NULL AND used_by IS NOT NULL)
    ),
    CONSTRAINT invitation_codes_revoke_pair CHECK (
        (revoked_at IS NULL AND revoked_by IS NULL) OR
        (revoked_at IS NOT NULL AND revoked_by IS NOT NULL)
    ),
    CONSTRAINT invitation_codes_terminal_once CHECK (
        NOT (used_at IS NOT NULL AND revoked_at IS NOT NULL)
    ),
    CONSTRAINT invitation_codes_expiry_after_create CHECK (expires_at > created_at)
);

CREATE INDEX idx_invitation_codes_created_at
    ON invitation_codes (created_at DESC);

CREATE INDEX idx_invitation_codes_expires_at
    ON invitation_codes (expires_at)
    WHERE used_at IS NULL AND revoked_at IS NULL;
```

```sql
-- migrations/039_invitation_codes.down.sql
DROP TABLE IF EXISTS invitation_codes;
```

实现时若 039 已被占用，仅顺延文件号，不修改已发布迁移。

### 3.2 字段说明

| 字段 | 说明 |
|---|---|
| `code_hash` | `hex(SHA-256(明文邀请码))`，唯一索引用于 O(1) 查找；永不返回前端 |
| `code_prefix` | 明文前 8 字符，仅用于管理员辨认，例如 `Ab3dE9_x…`，不能用于注册 |
| `created_by` | 生成邀请码的 admin 用户 ID |
| `expires_at` | 强制到期时间；默认创建后 7 天，最长 30 天 |
| `used_at/used_by` | 成功注册时在同一事务内写入 |
| `revoked_at/revoked_by` | admin 撤销时写入；已使用或已过期的码不可再撤销 |
| `created_at/updated_at` | 满足业务表通用约定；使用或撤销时同步更新 `updated_at` |

不增加单独 `status` 列，避免状态与时间字段不一致。API 按以下优先级派生：

```text
used_at != null       -> used
revoked_at != null    -> revoked
expires_at <= now()   -> expired
otherwise             -> active
```

由于服务层只允许撤销 active 邀请码，`used` 与 `revoked` 由数据库约束保证互斥。

### 3.3 邀请码格式与存储

Go 标准库即可完成，无需新依赖：

```go
raw := make([]byte, 32)
_, err := rand.Read(raw)
code := base64.RawURLEncoding.EncodeToString(raw)
sum := sha256.Sum256([]byte(code))
codeHash := hex.EncodeToString(sum[:])
```

- 32 字节随机量提供 256 bit 熵，不采用短数字码或递增编号。
- base64url 字符集便于复制，不包含空格、`+`、`/` 或 padding。
- SHA-256 无盐摘要在此可用，因为输入不是低熵密码；数据库泄露后无法实际穷举 256 bit 随机值。
- 后端对用户输入只做首尾空白 trim，不改变大小写。

## 4. 后端 API

### 4.1 注册（现有端点收紧）

`POST /api/v1/auth/register`

请求：

```json
{
  "username": "zhangsan",
  "password": "**********",
  "invitation_code": "Ab3dE9_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
}
```

成功响应保持现状，HTTP 201：

```json
{
  "data": {
    "id": "...",
    "username": "zhangsan",
    "display_name": "",
    "role": "member",
    "disabled": false,
    "language": "zh",
    "created_at": "2026-08-18T10:00:00+08:00",
    "must_change_password": true
  },
  "request_id": "req_..."
}
```

错误：

| HTTP | code | 条件 | 对外文案原则 |
|---:|---|---|---|
| 403 | `registration_disabled` | `ALLOW_REGISTER!=true` | 保持现有语义 |
| 429 | `rate_limit_exceeded` | 同 IP 超过 5 次/小时 | 保持现有限流 |
| 400 | `invitation_code_required` | 字段缺失或 trim 后为空 | “请输入邀请码” |
| 400 | `invalid_invitation_code` | 格式错误、不存在、已用、过期或已撤销 | 统一为“邀请码无效或已失效” |
| 400 | `password_too_short` | 密码不符合现有规则 | 保持现有错误 |
| 409 | `username_taken` | 有效邀请码持有者提交重复用户名 | 保持现有错误 |

未知、已用、过期和已撤销不返回不同错误码或状态细节，防止把注册端点变成邀请码状态探测器。无有效邀请码时不得先执行用户名存在性查询，避免外部人员枚举用户名。

### 4.2 生成邀请码

`POST /api/v1/admin/invitation-codes`

- 鉴权：`AuthRequired` + `RequireRole(admin)`。
- 写保护：`Audit` + `RequireIdempotencyKey` + CSRF。
- 请求：`expires_at` 可省略；省略时为服务端当前时间加 7 天。必须晚于当前时间且不超过 30 天。

```json
{
  "expires_at": "2026-08-25T18:00:00+08:00"
}
```

响应 HTTP 201；`code` 只在本次响应出现：

```json
{
  "data": {
    "invitation": {
      "id": "...",
      "code_prefix": "Ab3dE9_x",
      "status": "active",
      "created_by": "usr_admin",
      "used_by": null,
      "expires_at": "2026-08-25T18:00:00+08:00",
      "used_at": null,
      "revoked_at": null,
      "created_at": "2026-08-18T10:00:00+08:00"
    },
    "code": "Ab3dE9_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  },
  "request_id": "req_..."
}
```

错误：400 `invalid_expiry`、401、403、409 `duplicate_idempotency_key`。

### 4.3 邀请码列表

`GET /api/v1/admin/invitation-codes?page=1&per_page=20&status=active`

- 仅 admin；不要求 `Idempotency-Key`。
- `status` 可选：`active / used / expired / revoked`。
- `page` 默认 1，`per_page` 默认 20、最大 100，按 `created_at DESC`。
- 响应绝不包含 `code` 或 `code_hash`。

```json
{
  "data": {
    "items": [
      {
        "id": "...",
        "code_prefix": "Ab3dE9_x",
        "status": "used",
        "created_by": "usr_admin",
        "used_by": "usr_new",
        "expires_at": "2026-08-25T18:00:00+08:00",
        "used_at": "2026-08-18T11:00:00+08:00",
        "revoked_at": null,
        "created_at": "2026-08-18T10:00:00+08:00"
      }
    ],
    "total": 1,
    "page": 1,
    "per_page": 20
  },
  "request_id": "req_..."
}
```

非法状态或分页参数返回 400 `bad_request`。

### 4.4 撤销邀请码

`POST /api/v1/admin/invitation-codes/{id}/revoke`

- 仅 admin；要求 CSRF、`Idempotency-Key` 和审计。
- 无请求体。
- 仅 `active` 状态可撤销；成功响应 200，返回更新后的邀请码摘要。
- 不硬删除记录，保留生成、撤销和后续失败尝试的可追溯性。

错误：404 `invitation_code_not_found`；409 `invitation_code_not_active`；409 `duplicate_idempotency_key`。

### 4.5 路由与来源门

在 `main.go` 的现有 admin 区域注册邀请码管理路由，和 `/api/v1/admin/users` 使用相同角色边界。建议结构：

```text
/api/v1/admin/invitation-codes
├── GET  /                    list
├── POST /                    create
└── POST /{id}/revoke         revoke
```

当前公网 `SourceGate` 不允许 `/api/v1/admin/*` 写操作，现有用户管理写操作也同样只能从允许来源执行。本功能保持该安全边界，不把邀请码管理加入公网写白名单。若以后确需公网生成/撤销邀请码，必须单独安全评审，并同步 `docs/permission-audit.md` 的精确路径白名单及 `source_gate` 测试。

## 5. 注册校验与事务

### 5.1 Handler 顺序

沿用现有 `Register` handler，只增加请求字段和错误映射：

```text
检查 ALLOW_REGISTER
  -> IP 限流（5 次/小时/IP）
  -> 解码 {username,password,invitation_code}
  -> 设置审计 username
  -> service.Register(username,password,invitationCode)
  -> 通知新用户注册
  -> 201 UserInfo
```

保留当前限流、密码规则、默认角色、通知和成功响应，不增加预检查端点。

### 5.2 Service 与 Repository 事务

密码校验和 Argon2id 计算在开启事务前完成，避免持锁期间执行昂贵哈希。Repository 在一个事务中完成：

1. 对邀请码明文计算 SHA-256。
2. `SELECT ... FOR UPDATE` 按 `code_hash` 锁定记录。
3. 检查 `used_at IS NULL AND revoked_at IS NULL AND expires_at > now()`；任一不满足均回滚并返回统一错误。
4. 插入 `users`，角色沿用 `member`；由数据库唯一约束最终处理并发重名。
5. 更新邀请码的 `used_at=now()`、`used_by=<new user id>`、`updated_at=now()`。
6. 提交并返回新用户。

```sql
BEGIN;

SELECT id, expires_at, used_at, revoked_at
FROM invitation_codes
WHERE code_hash = $1
FOR UPDATE;

INSERT INTO users (...)
VALUES (...)
RETURNING ...;

UPDATE invitation_codes
SET used_at = now(), used_by = $2, updated_at = now()
WHERE id = $1;

COMMIT;
```

同一码并发注册时，第一个事务持有行锁；第二个事务等待后看到 `used_at`，返回 `invalid_invitation_code`。用户插入或提交失败会回滚邀请码状态，不会产生“码已用但账号不存在”。

Service 不再在事务外先调用 `IsUsernameTaken`；直接依赖现有 `users.username` 唯一约束并映射为 `ErrUsernameTaken`，减少一次查询并避免 TOCTOU。

### 5.3 审计

| 动作 | action | 必记字段 | 禁止记录 |
|---|---|---|---|
| 注册成功/失败 | 现有 `auth.register` | username、结果、request_id、client_ip；成功可记 invitation ID | 邀请码明文、hash、密码 |
| 生成邀请码 | `admin.invitation_codes.create` | invitation ID、前缀、expires_at、admin | 完整 code、code_hash |
| 撤销邀请码 | `admin.invitation_codes.revoke` | invitation ID、前缀、admin | 完整 code、code_hash |

列表 GET 不写审计，沿用普通管理列表行为。注册失败日志不得区分“已使用/过期/撤销/不存在”，详细状态仅 admin 列表可见。

## 6. 前端设计

### 6.1 注册页 `LoginView.vue`

在现有注册弹窗的确认密码下增加一个必填输入框：

```text
用户名        [________________]
密码          [________________]
确认密码      [________________]
邀请码        [________________]
               请向实验室管理员获取一次性邀请码
                              [取消] [注册]
```

- `registerForm` 增加 `invitationCode`；提交前 trim 并校验非空。
- `auth.ts` 的 `register` 请求 body 增加 `invitation_code`。
- 输入使用普通文本，不使用密码遮罩，方便粘贴和核对；关闭浏览器自动更正与自动大写。
- 失败时保留用户名和邀请码，允许修正；成功或关闭弹窗时清空邀请码。
- `registration_disabled` 显示“当前未开放自助注册，请联系管理员创建账号”。
- `invitation_code_required` 与 `invalid_invitation_code` 分别显示本地化提示；不展示后台状态细节。
- 注册成功后继续调用现有 `auth.login()` 并跳转，不改核心流程。

### 6.2 管理员用户页 `AdminUsersView.vue`

在现有用户列表下方增加“邀请码”管理区，不新增路由或组件：

```text
邀请码                                      [生成邀请码]
[全部] [可用] [已使用] [已过期] [已撤销]
-------------------------------------------------------
Ab3dE9_x…  可用    2026-08-25 18:00 到期      [撤销]
K92aPq_1…  已使用  usr_new / 2026-08-18 11:00  —
```

- 管理区复用 `ListPage` 页面内现有布局、`ResponsiveTable`、`StateBlock`、`StatusBadge`、`FormDialog`、`formatDateTime` 和分页模式。
- 列表包含前缀、状态、生成时间、到期时间、使用者；仅 active 行显示“撤销”。
- 状态使用 `statusMeta.ts` 新增 `invitationCode` domain 的显式映射，不在模板动态拼 i18n key。
- 生成弹窗默认有效期 7 天，可选未来日期时间；前端限制只是 UX，后端仍校验最长 30 天。
- 生成成功打开结果弹窗，显示完整邀请码、到期时间、`request_id` 和“复制邀请码”按钮。
- 结果弹窗明确提示“完整邀请码仅显示一次”；尚未复制即关闭时二次确认。
- 复制复用 `AdminUsersView` 现有 Clipboard API + HTTP fallback，不新增工具或依赖。
- 列表中没有完整码，因此不提供误导性的“再次复制”；需要新码时生成一个并撤销旧码。
- 撤销使用 `ElMessageBox.confirm`，成功提示包含 `request_id`，然后刷新当前列表。
- 加载、空、错误独立于用户列表，邀请码加载失败不阻断用户管理。

### 6.3 前端 API

在 `web-ui/src/api/auth.ts` 增加类型和函数，继续复用 `request` / `requestWithMeta`：

```ts
type InvitationCode = {
  id: string
  code_prefix: string
  status: 'active' | 'used' | 'expired' | 'revoked'
  created_by: string
  used_by: string | null
  expires_at: string
  used_at: string | null
  revoked_at: string | null
  created_at: string
}

listInvitationCodes(params)
createInvitationCode({ expires_at? })
revokeInvitationCode(id)
```

写方法使用 `requestWithMeta`，以便成功和失败界面显示 `request_id`。不新建 `invitations.ts`：三个调用只服务于 auth 管理页，留在现有 auth API 文件最清晰。

## 7. i18n 文案清单

`zh.ts` 与 `en.ts` 同一提交新增同构 key；通用按钮继续复用 `common.*`。

### 7.1 `login`

| key | 中文 | English |
|---|---|---|
| `invitationCode` | 邀请码 | Invitation code |
| `invitationCodePlaceholder` | 粘贴管理员提供的邀请码 | Paste the code from an administrator |
| `invitationCodeHelp` | 请向实验室管理员获取一次性邀请码 | Ask a lab administrator for a one-time invitation code |
| `invitationCodeRequired` | 请输入邀请码 | Enter an invitation code |
| `invitationCodeInvalid` | 邀请码无效或已失效，请联系管理员 | The invitation code is invalid or no longer active. Contact an administrator. |
| `registrationDisabled` | 当前未开放自助注册，请联系管理员创建账号 | Self-registration is unavailable. Contact an administrator to create an account. |

### 7.2 `adminUsers.invitationCodes`

| key | 中文 | English |
|---|---|---|
| `title` | 邀请码 | Invitation codes |
| `generate` | 生成邀请码 | Generate code |
| `generateTitle` | 生成邀请码 | Generate invitation code |
| `expiresAt` | 到期时间 | Expires at |
| `defaultExpiryHint` | 留空则 7 天后过期，最长 30 天 | Leave blank for 7 days; maximum 30 days |
| `codePrefix` | 邀请码 | Code |
| `createdAt` | 生成时间 | Created at |
| `usedBy` | 使用者 | Used by |
| `statusAll` | 全部 | All |
| `statusActive` | 可用 | Active |
| `statusUsed` | 已使用 | Used |
| `statusExpired` | 已过期 | Expired |
| `statusRevoked` | 已撤销 | Revoked |
| `copy` | 复制邀请码 | Copy code |
| `copied` | 邀请码已复制 | Invitation code copied |
| `oneTimeWarning` | 完整邀请码仅显示一次，请立即复制并安全发送 | The full code is shown only once. Copy it now and share it securely. |
| `closeWithoutCopy` | 尚未复制邀请码，确认关闭？关闭后无法再次查看完整邀请码。 | The code has not been copied. Close anyway? The full code cannot be viewed again. |
| `generateSuccess` | 邀请码已生成（request_id: {requestId}） | Invitation code generated (request_id: {requestId}) |
| `generateFailed` | 生成邀请码失败 | Failed to generate invitation code |
| `revoke` | 撤销 | Revoke |
| `revokeConfirm` | 确认撤销邀请码“{prefix}…”？撤销后无法恢复。 | Revoke invitation code “{prefix}…”? This cannot be undone. |
| `revokeSuccess` | 邀请码已撤销（request_id: {requestId}） | Invitation code revoked (request_id: {requestId}) |
| `revokeFailed` | 撤销邀请码失败 | Failed to revoke invitation code |
| `loadFailed` | 邀请码加载失败 | Failed to load invitation codes |
| `empty` | 暂无邀请码 | No invitation codes |

状态 label 必须通过 `statusMeta.ts` 的显式 `labelKey` 引用上述 key，中英文 key 结构由现有 `keys.test.ts` 双向校验。

## 8. 权限与安全控制

### 8.1 权限矩阵

| 操作 | 未登录 | member / viewer / maintainer | admin | Agent / SERVICE_TOKEN |
|---|---:|---:|---:|---:|
| 使用邀请码注册 | 仅 `ALLOW_REGISTER=true` | 同左，但已登录无特殊权利 | 同左 | 拒绝/不提供专用能力 |
| 列表 | 401 | 403 | 允许 | 403 |
| 生成 | 401 | 403 | 允许 | 403 |
| 撤销 | 401 | 403 | 允许 | 403 |

前端仅 admin 显示邀请码区域，但后端 `RequireRole(admin)` 是最终边界。邀请码管理不授予项目 owner/maintainer，因为它创建的是全局账号，不是项目成员关系。

### 8.2 防枚举与防暴力

- 邀请码使用 256 bit CSPRNG，不可按前缀或时间预测。
- 数据库只存摘要；列表、日志、审计和通知都不返回明文。
- 注册端点没有邀请码预检 API，所有失效状态统一 `invalid_invitation_code`。
- 无有效邀请码时不查询用户名是否存在。
- 保留现有 5 次/小时/IP 限流，且在邀请码查询前计数；猜码会很快被限制。
- 继续由 VPS/Nginx 和 `SourceGate` 提供外围来源控制；后端限流仍不可省略。
- 创建邀请码端点要求 admin JWT、CSRF、幂等键和审计；写请求不自动重试。

### 8.3 一次性、到期与撤销

- 每码最多成功注册一个账号；数据库行锁与单事务保证并发下也只能消费一次。
- 所有码必须过期，默认 7 天，最长 30 天；不提供永久码。
- active 码可由 admin 随时撤销；撤销不可恢复。
- 已使用记录不删除，保留 `used_by` 作为邀请来源追踪。
- 邀请码泄露时，管理员撤销旧码并生成新码；若已被使用，则立即停用异常用户并检查 `auth.register` 审计。

### 8.4 敏感数据处理

- 完整邀请码属于短期 bearer credential，复制后应通过可信私聊渠道发送，不发公共群、不截图到工单。
- 创建响应设置现有 JSON 安全头，并建议网关对 `/api/v1/admin/invitation-codes` 响应使用 `Cache-Control: no-store`；前端不写 localStorage/sessionStorage。
- 不把邀请码放入 URL、查询参数、前端埋点、错误 details、ntfy 或 slog。
- 管理页离开或结果弹窗关闭后清空内存中的明文 ref。

## 9. 改动文件清单

| 文件 | 改动 |
|---|---|
| `migrations/039_invitation_codes.up.sql` | 新建邀请码表、约束和索引（若 039 被占用则顺延） |
| `migrations/039_invitation_codes.down.sql` | 删除邀请码表 |
| `go-server/auth/model.go` | 注册请求增加 `invitation_code`；新增邀请码 DTO |
| `go-server/auth/repository.go` | 邀请码 CRUD；注册与消费邀请码的原子事务 |
| `go-server/auth/service.go` | 生成、列表、撤销、注册校验与领域错误 |
| `go-server/auth/handler.go` | 管理 handler、注册参数透传、统一错误映射与 no-store |
| `go-server/main.go` | 注册 admin-only 邀请码路由 |
| `go-server/auth/service_test.go` | 生成格式、有效期、状态和错误测试 |
| `go-server/auth/repository_db_test.go` | 单次/并发消费、回滚、过期和撤销的真实 DB 测试 |
| `go-server/auth/handler_test.go` | `ALLOW_REGISTER`、无码、无效码、权限和 HTTP 映射 |
| `go-server/auth/handler_db_test.go` | 管理端点、幂等、审计与完整注册链路 |
| `docs/api-contract.md` | 更新 register 请求和新增 admin 邀请码契约 |
| `docs/permission-audit.md` | 登记邀请码权限、敏感字段脱敏和 SourceGate 边界 |
| `deploy/.env.example` | 说明邀请制启用时设置 `ALLOW_REGISTER=true` |
| `web-ui/src/api/auth.ts` | 注册 body 与邀请码管理 API/类型 |
| `web-ui/src/views/LoginView.vue` | 注册邀请码字段及错误提示 |
| `web-ui/src/views/AdminUsersView.vue` | 生成/一次复制/列表/筛选/撤销 UI |
| `web-ui/src/utils/statusMeta.ts` | 邀请码四状态显式映射 |
| `web-ui/src/i18n/zh.ts` | 中文文案 |
| `web-ui/src/i18n/en.ts` | 同构英文文案 |
| 现有相邻前端测试文件 | API 契约、注册表单、管理页和 i18n 测试 |
| `go-server/static/` | 前端构建完成后全量同步嵌入产物 |

不修改 `router/index.ts`、`config/navigation.ts`、Pinia store、基础组件、Python Agent 或项目成员 API。

## 10. 实现步骤

1. 添加下一序号迁移并执行 up/down/up，确认约束和索引可重复部署。
2. 在 auth model/repository/service 中实现邀请码生成、派生状态、列表、撤销和原子注册事务。
3. 收紧现有 register 请求，在 handler 中增加统一错误映射；补齐管理 handler 和 `main.go` admin 路由。
4. 先完成 Go 单元、真实 DB、并发消费、权限、幂等和审计测试。
5. 更新 `api-contract.md`、`permission-audit.md` 与部署变量说明，明确双门语义和明文脱敏规则。
6. 更新 `auth.ts` 与 `LoginView.vue`，保持注册成功后自动登录的现有流程。
7. 在 `AdminUsersView.vue` 增加邀请码管理区，复用现有表格、弹窗、复制和反馈能力。
8. 补齐 `statusMeta`、中英文文案、API/组件/i18n 测试。
9. 运行后端和前端完整验证；构建并全量同步 `web-ui/dist` 到 `go-server/static/`。
10. 前后端同批部署后把生产 `ALLOW_REGISTER` 改为 `true`，由 admin 生成短期测试码完成冒烟，再正式发码。

## 11. 验证方式

### 11.1 后端自动化

至少覆盖：

- `ALLOW_REGISTER=false` 时即使携带有效码仍返回 `registration_disabled`，邀请码状态不变。
- `ALLOW_REGISTER=true` 且无码时返回 `invitation_code_required`。
- 随机错误、格式错误、已用、过期、已撤销都返回同一 `invalid_invitation_code`。
- 有效码注册成功，用户角色为 `member`，`used_by/used_at` 正确。
- 同一码两个并发注册只有一个成功，另一个失败且不产生第二个用户。
- 用户名冲突、用户插入失败或事务提交失败时邀请码仍为 active。
- 非 admin 的 list/create/revoke 均 403；admin 成功。
- create/revoke 缺 `Idempotency-Key` 为 400，复用 key 为 409，并写正确审计 action。
- 列表与审计响应不包含 `code`、`code_hash` 或密码。
- 到期默认值、未来时间、30 天上限和分页边界正确。

执行：

```bash
cd go-server
go test ./auth ./middleware
go test -race ./...
go vet ./...
```

真实 DB 测试按仓库约定设置 `TEST_DATABASE_URL`；迁移验证执行全库 up/down 流程，不能修改历史迁移。

### 11.2 前端自动化

至少覆盖：

- 注册请求准确发送 `invitation_code`；空码不发请求。
- 注册关闭与邀请码无效显示不同的中英文文案。
- admin 可见管理区，生成后只能在结果弹窗取得和复制完整码。
- 未复制关闭二次确认；关闭后清空明文。
- active 行可撤销，used/expired/revoked 无撤销按钮。
- 列表加载、空、错误、分页和移动端 card 状态正确。
- 写成功/失败反馈包含 `request_id`，写请求不自动重试。
- 中英文 key 双向一致，`statusMeta` 四状态完整。

执行：

```bash
cd web-ui
npm test
npm run test:coverage
npm run build
grep -rnE '#[0-9a-fA-F]{3,8}\b|rgba?\(' src --include='*.vue'
```

构建后按仓库现有流程全量同步 `dist` 到 `go-server/static/`，再运行静态产物一致性检查。

### 11.3 手工验收矩阵

| 场景 | 期望 |
|---|---|
| 注册开关关闭，提交有效码 | 403；不消耗邀请码 |
| 注册开关开启，不填邀请码 | 400；账号不创建 |
| 错码/已用/过期/撤销码 | 相同通用错误；不暴露具体状态 |
| admin 生成默认邀请码 | 7 天后过期；完整码仅出现一次；request_id 可见 |
| 复制后刷新列表 | 只见前缀，无法重新获取完整码 |
| 两个浏览器同时使用同一码 | 仅一个注册成功 |
| 注册过程中用户名冲突 | 邀请码仍可改用另一个用户名注册 |
| admin 撤销 active 码 | 立即不可注册，列表状态变为 revoked |
| member 手工调用管理 API | 403，无数据泄露 |
| 新用户注册成功 | 自动登录；角色为 member；无项目时走既有等待加入提示 |
| 中文/English、手机、深色模式、键盘操作 | 文案完整，无横向滚动，焦点和危险操作清晰 |

## 12. 风险与处理

| 风险 | 处理 |
|---|---|
| 邀请码在发送途中泄露 | 默认 7 天、最长 30 天、一次性；可信私聊发送；发现后立即撤销并重发 |
| 泄露码已被抢先使用 | 停用异常账号、检查 `auth.register` 审计与来源 IP、生成新码；邀请码列表通过 `used_by` 定位 |
| 同一码并发复用 | `SELECT ... FOR UPDATE` 与单事务保证只有一个成功 |
| 账号创建失败但码被消耗 | 用户插入和邀请码消费处于同一事务，任一步失败全部回滚 |
| 数据库或列表泄露 | 只保存 SHA-256 摘要，列表仅返回 8 字符前缀；256 bit 随机值不可实际穷举 |
| 通过错误响应枚举状态或用户 | 失效状态统一错误；无有效邀请码时不查询用户名 |
| 永久未使用码长期暴露 | 强制过期；默认 7 天、最长 30 天；admin 可提前撤销 |
| 前后端发布不同步 | 注册请求字段变为必填，必须同批发布并在开启开关前完成冒烟 |
| 明文落入日志、缓存或前端存储 | 审计/日志脱敏，响应 `no-store`，仅组件内存暂存，关闭即清空 |
| 管理员误撤销或误发 | 撤销二次确认；不可恢复但可生成新码；列表保留状态与审计 |
| 公网管理员写操作扩大攻击面 | 保持现有 SourceGate，不新增公网 admin 写白名单 |
| 过期行持续增长 | 当前实验室规模分页查询足够；出现可测量的表膨胀后再增加定期归档，不预建清理任务 |

## 13. 验收标准

- `ALLOW_REGISTER=false` 能一键关闭全部自助注册；`true` 时无码绝不创建账号。
- 只有有效、未用、未过期、未撤销的邀请码能注册，且每码最多成功一次。
- 注册与邀请码消费原子完成，并发、失败回滚测试通过。
- 邀请码管理 API 仅 admin 可用，生成/撤销具备 CSRF、幂等和审计保护。
- 数据库、列表、日志和审计均不保存或暴露完整邀请码；明文只在生成响应显示一次。
- 注册页和用户管理页具备完整中英文文案、加载/空/错误状态、request_id 反馈和移动端可用性。
- 不新增依赖、独立路由、store、邮件服务或跨模块数据库访问。
