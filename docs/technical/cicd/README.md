# CI/CD 与发布文档入口

状态：active  
owner：文档与契约责任域  
更新时间：2026-06-28  
适用范围：CI/CD、发布、回滚、环境矩阵和上线验证文档导航

## 当前状态

当前阶段以本地开发和服务级联调为主，尚未进入正式上线阶段。因此本目录只维护发布文档契约，不维护可执行发布方案。

进入上线阶段前，必须在本目录补齐 CI/CD 和发布文档，并同步 `docs/standards/开发流程规范.md`、`docs/standards/GitHub仓库协作规范.md`、`docs/test/README.md`。

## 必须补齐的文档

| 文档 | 触发时机 | 模板 |
| --- | --- | --- |
| CI/CD 流水线设计 | 建立 GitHub Actions 或其他 CI/CD 前 | `docs/templates/CI-CD发布文档模板.md` |
| 发布与回滚手册 | 首次部署到测试或生产环境前 | `docs/templates/CI-CD发布文档模板.md` |
| 环境矩阵 | 存在 dev/test/staging/prod 环境时 | `docs/templates/CI-CD发布文档模板.md` |
| 上线验证清单 | 首次上线或重大版本发布前 | `docs/templates/测试报告模板.md` |

## 第一版非目标

- 不在本目录保存密钥、服务器密码、Token 或真实生产配置。
- 不用本目录替代 `.env.example`、本地配置规范或安全规范。
- 未进入上线阶段前，不把 CI/CD 文档标记为当前交付阻断项。

