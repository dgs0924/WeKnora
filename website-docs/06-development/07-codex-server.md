# 服务器 Codex 个人连接

通过官方 Codex app-server 的 stdio 协议使用服务器上已经登录的 Codex。
它与「Continue with ChatGPT」的 SIWC 凭据、模型目录分别管理，不把 Codex 的
`auth.json` 转换为 API Key，也不通过非官方推理网关转发令牌。

此入口用于个人开源部署。绑定的套餐不对空间其他成员、API Key 调用者或没有
原始用户身份的后台任务开放；不要将个人登录部署成公共或商业托管推理服务。
Codex 官方集成及适用范围见 [App Server 文档](https://learn.chatgpt.com/docs/app-server)。

## 登录与绑定

1. 在服务器安装官方 Codex CLI，使用普通用户执行 `codex login --device-auth`。
   在浏览器打开 CLI 提供的地址，亲自输入设备码并完成授权，不需要回调端口转发。
2. 使用 `codex login status` 验证登录。凭据目录权限设为 `0700`，文件为 `0600`。
   不把文件内容写进日志、聊天、源码或镜像。
3. 取得要使用套餐的 WeKnora 用户 ID，由部署管理员设置以下变量。
4. 重建包含此功能的个人 fork 镜像，加载 `docker-compose.codex.yml` 后重建 app 容器。
5. 使用绑定的用户登录，进入「设置 → ChatGPT → 服务器 Codex 账号」，刷新模型并启用。

| 变量 | 说明 |
| --- | --- |
| `WEKNORA_CODEX_OWNER_USER_ID` | 唯一允许使用此连接的 WeKnora 用户 ID；未设置时关闭入口 |
| `WEKNORA_CODEX_BINARY` | 后端可执行的官方 Codex 二进制绝对路径 |
| `WEKNORA_CODEX_HOME` | Codex 自己管理和续期的凭据目录 |
| `WEKNORA_CODEX_WORKDIR` | 隔离的空工作目录，不应挂载业务源码或宿主根目录 |
| `CODEX_HOST_VENDOR_DIR` | Compose 覆盖文件使用的宿主目录，包含 `bin/codex` |
| `CODEX_HOST_HOME` | Compose 覆盖文件使用的宿主 Codex 登录目录 |

容器内执行用户必须能访问凭据目录。当前 app 镜像的 `appuser` 为 UID/GID 1000；
不要为解决权限问题把凭据改为全员可读。凭据挂载需要可写，以便 Codex 自动续期。
二进制目录只读挂载，工作目录使用临时文件系统。

## 调用边界

- 仅对话模型。Embedding、ReRank、语音和图片输入不通过此入口提供。
- 由 Codex 维护登录状态；退出服务器 Codex 后，此连接也会不可用。
- 每次请求验证已认证的个人用户身份，模型记录不包含访问或刷新令牌。
- Codex 子进程只处理模型对话，不授予其执行服务器命令或直接访问业务文件的能力。
- 服务器需要能访问 OpenAI。若使用代理，设置后端的 `HTTPS_PROXY`、`HTTP_PROXY`
  及正确的 `NO_PROXY`；容器的 `127.0.0.1` 不等于宿主机。
- 模型目录不等于套餐额度保证，以真实请求完成状态为准。

官方设备码登录说明：[无桌面设备登录](https://learn.chatgpt.com/docs/auth#login-on-headless-devices)。
