# LDAP / Active Directory 登录

WeKnora 支持独立的 LDAP 登录接口 `POST /api/v1/auth/ldap/login`。启用后，原本地账号密码登录接口仍可使用。LDAP 登录成功后签发普通 WeKnora JWT，后续权限与空间成员关系仍由 WeKnora 管理。

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

使用 AD 时，通常把 `LDAP_AUTH_USERNAME_ATTRIBUTE` 设为 `sAMAccountName`。服务账号需要有用户搜索权限。`LDAP_AUTH_URL` 也可使用 `ldap://...:389`，但必须同时设置 `LDAP_AUTH_STARTTLS=true`；服务端证书须受容器或宿主机的系统 CA 信任。不会使用明文 LDAP 传输密码。

请求体为 `{"username":"alice","password":"..."}`。成功响应与 `/auth/login` 相同，包含访问令牌、刷新令牌、用户和空间信息。认证配置接口 `/api/v1/auth/config` 的 `ldap_enabled` 指示该入口是否启用。

自动开户默认关闭。开启后首次登录会创建本地用户，使用 LDAP 条目 DN 作为身份绑定键。若目录邮箱已经属于本地或 OIDC 用户，系统会拒绝自动接管该账号；需要管理员处理冲突。LDAP 条目 DN 变更后需要管理员迁移绑定；当前不自动同步 LDAP 组到 WeKnora 角色，也不自动禁用已经离开目录的本地账号。目录账号停用后，已经签发的 WeKnora 会话不会被自动撤销，应由管理员禁用本地用户或撤销会话。LDAP 密码只用于当次绑定校验，不会保存到 WeKnora。
