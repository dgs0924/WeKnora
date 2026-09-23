# LDAP / Active Directory 登录

WeKnora 支持独立的 LDAP 登录接口 `POST /api/v1/auth/ldap/login`。启用后，原本地账号密码登录接口仍可使用。LDAP 登录成功后签发普通 WeKnora JWT，后续权限与空间成员关系仍由 WeKnora 管理。系统管理员可在“设置 → LDAP”配置；也可以通过环境变量作为默认配置。

在后端环境变量中配置：

```dotenv
LDAP_AUTH_ENABLE=true
LDAP_AUTH_URL=ldaps://directory.example.com:636
LDAP_AUTH_BIND_DN=cn=weknora,ou=service,dc=example,dc=com
LDAP_AUTH_BIND_PASSWORD=由部署环境提供
LDAP_AUTH_BASE_DN=ou=users,dc=example,dc=com
LDAP_AUTH_USER_FILTER=(&(objectClass=person)(|(mail={username})(uid={username})(sAMAccountName={username})))
LDAP_AUTH_USERNAME_ATTRIBUTE=uid
LDAP_AUTH_EMAIL_ATTRIBUTE=mail
LDAP_AUTH_AUTO_PROVISION=true
```

使用 AD 时，通常把 `LDAP_AUTH_USERNAME_ATTRIBUTE` 设为 `sAMAccountName`。服务账号需要有用户搜索权限。`LDAP_AUTH_URL` 也可使用 `ldap://...:389`；推荐同时设置 `LDAP_AUTH_STARTTLS=true`，此时需要验证目录证书。若明确选择内网明文 LDAP，可关闭 StartTLS 并开启 `LDAP_AUTH_ALLOW_PLAINTEXT=true`（默认关闭）；该模式不使用证书，但 Bind 密码和用户密码会在网络中以明文传输。

若连接地址是 IP，但目录证书只包含域名，可在设置页填写“TLS 校验名称”（`LDAP_AUTH_TLS_SERVER_NAME`）为证书覆盖的域名；网络仍连接原 IP。自签名证书或私有 CA 可以粘贴到“受信任证书 PEM”（`LDAP_AUTH_TLS_CA_PEM`），系统会在系统 CA 的基础上信任该证书。两项都保持严格证书校验，不提供跳过验证开关。生产环境建议由目录服务安装与实际访问地址匹配的可信证书。

请求体为 `{"username":"alice","password":"..."}`。成功响应与 `/auth/login` 相同，包含访问令牌、刷新令牌、用户和空间信息。认证配置接口 `/api/v1/auth/config` 的 `ldap_enabled` 指示该入口是否启用。

系统管理员专用的 `GET/PUT/DELETE /api/v1/system/admin/ldap` 用于读取、保存或重置运行时配置。PUT 的 `bind_password` 留空表示保留当前密码；密码使用 `SYSTEM_AES_KEY` 加密后存入系统设置，读取接口只返回 `has_bind_password`。没有有效的 32 字节 `SYSTEM_AES_KEY` 时，带密码的配置不会保存。重置会恢复到环境变量配置。通用系统设置列表不会返回 LDAP 配置项。

“测试连接”调用 `POST /api/v1/system/admin/ldap/test`，使用当前表单内容临时检查配置、TLS、目录搜索账号绑定和用户 Base DN 读取；空白 `bind_password` 复用已保存密码。接口不保存表单、不验证普通用户密码，返回 `ok`、`stage` 与安全的诊断信息。请求日志会遮盖 `bind_password`。

自动开户默认关闭。开启后首次登录会创建本地用户，使用 LDAP 条目 DN 作为身份绑定键。若目录邮箱已经属于本地或 OIDC 用户，系统会拒绝自动接管该账号；需要管理员处理冲突。LDAP 条目 DN 变更后需要管理员迁移绑定；当前不自动同步 LDAP 组到 WeKnora 角色，也不自动禁用已经离开目录的本地账号。目录账号停用后，已经签发的 WeKnora 会话不会被自动撤销，应由管理员禁用本地用户或撤销会话。LDAP 密码只用于当次绑定校验，不会保存到 WeKnora。
