# 绑定 ChatGPT 并使用个人套餐

本 fork 支持 OpenAI 官方的开源应用 Sign in with ChatGPT。先使用本地账号或 LDAP 登录 WeKnora，再在设置的「ChatGPT 套餐」中绑定符合条件的 ChatGPT Plus/Pro 账号，选择并启用该账号当前可用的模型。它不会替代 WeKnora 登录，也不会读取 ChatGPT 历史对话。

## 使用步骤

1. 在个人设置中打开「ChatGPT 套餐」，点击 **Continue with ChatGPT**。
2. 在系统浏览器中登录 ChatGPT，选择账号与工作空间并授权套餐使用。浏览器与本机 WeKnora 必须能访问同一个本机回调监听器。
3. 授权成功后，可在「模型管理 → 添加模型」中选择「对话 → API → ChatGPT 套餐（我的账号）」，再从账号的可用模型中选择并保存，无需填写 API Key 或接口地址。也可以在「ChatGPT 套餐」设置页直接启用模型。启用后，在现有问答模型选择器中选择对应的 `ChatGPT · …` 模型。
4. 断开连接会删除本机凭据并取消未完成的授权。要撤销 OpenAI 侧的应用访问或管理套餐用量，请同时前往 ChatGPT 设置。

首次确认说明会提示该应用的合格请求使用个人套餐。额度耗尽、套餐不可用、授权过期或连接撤销时，应查看 ChatGPT 设置或重新授权；不会自动切换到付费 API Key。

## 本机配置

无需 API Key、客户端密钥或预先申请的 client ID。首次授权使用官方动态注册入口，后续使用 OpenAI 返回的客户端 ID。应用使用 PKCE S256、随机 state/nonce 和经签名验证的 ID token。

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `CHATGPT_AUTH_DIR` | 系统用户配置目录下的 `weknora/chatgpt` | 持久化主机 ID 和分用户凭据；目录权限 0700、文件权限 0600 |
| `CHATGPT_CALLBACK_PORT` | `1455` | 本机回调端口；`0` 为自动选择临时端口 |
| `CHATGPT_CALLBACK_BIND` | `127.0.0.1` | 本机监听地址；仅容器内转发场景可使用 `0.0.0.0` |

凭据仅保存在本服务自己的受保护本地目录，不写入模型 API Key、浏览器存储或数据库。不要将此目录提交到 Git、放入日志、共享给其他用户或复制主机 ID 到另一安装。容器需为该目录挂载持久卷；单个凭据目录由一个后端进程管理，不能由多个实例同时刷新同一令牌。

## Docker 和远端自托管

开发环境的 `docker-compose.dev.yml` 仅运行基础设施；后端本机运行时无需容器回调配置。

完整 Docker 部署可使用本仓库的 `docker-compose.chatgpt.yml` 覆盖文件：

```bash
docker compose -f docker-compose.yml -f docker-compose.chatgpt.yml up -d
```

覆盖文件将回调端口仅发布到宿主机 `127.0.0.1`，在容器内部使用 `0.0.0.0` 监听，并为凭据配置独立持久卷。使用前需先构建包含此功能的个人 fork 后端镜像，官方发行镜像不包含该定制。不要公开发布回调端口。端口可通过 `CHATGPT_CALLBACK_PORT` 调整，但 Docker 转发场景不能使用 `0`。

远端服务器的 `127.0.0.1` 不等于浏览器所在电脑。对于自己管理的远端实例，可先建立 SSH 本机转发，再启动授权：

```bash
ssh -N -L 127.0.0.1:1455:127.0.0.1:1455 user@your-server
```

服务器与本机均须保持该端口可用；非默认端口时同时调整配置与隧道。没有本机转发时不能直接通过服务器域名完成该开源回调。付费或托管产品应按官方要求申请对应接入资格，不能用此个人流程替代网站身份登录。

## 用户与模型隔离

绑定归属于当前已登录的 WeKnora 个人账号。启用的模型仅在当前空间中对该用户可见，模型引用不包含 OAuth 凭据。每次调用重新检查用户身份并读取有效令牌；其他成员、API Key、匿名嵌入请求以及缺少个人身份的后台任务不能使用该连接。

该接入仅提供问答模型，不提供 Embedding、Rerank、ASR 等套餐接口。知识库仍需配置独立的嵌入与重排模型。后台任务若不能保留原始个人调用身份，应继续使用独立 API 模型。

套餐调用固定使用公共 `https://api.openai.com/v1/responses`，每次设置 `store:false`、`stream:true`，只在收到 `response.completed` 后视为成功。非流式业务接口也会在后端聚合官方流式响应，失败、额度限制或中途断流不会被视为成功。

## 官方参考

- [开源应用概览](https://developers.openai.com/siwc/token-sharing-open-source)
- [动态注册与授权](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
- [账号与刷新](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions)
- [模型列表与 Responses 调用](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
- [自托管虚拟机](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms)

真实授权、账号模型权限和套餐额度只能在用户完成官方登录后验证。自动化测试使用隔离的 OAuth/JWKS/模型及流式响应，不代替真实账号验收。
