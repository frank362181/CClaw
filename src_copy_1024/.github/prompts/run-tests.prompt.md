---
description: "运行测试并校验改动：跑 Go 测试 + 前端检查 + 构建，验证改动没有破坏仓库约定。Use when: 改动后想跑测试/校验/validate/check"
agent: "agent"
---
对当前改动运行完整校验，确保符合 CClaw 仓库约定且不破坏现有功能。

## 步骤

1. **定位改动范围**
   先看当前工作区改动（`git status` / `git diff` 或用户选中的文件），判断改的是后端（`agentService` / `cclawservice` / `platformutility`）还是前端（`cclawchat`）。

2. **后端 Go 测试**（改动涉及 Go 代码时，必跑）
   - `cd cclawservice && go test ./...`
   - `cd platformutility && go test ./...`
   - 若改动了 prompt / 预算 / front matter 逻辑，重点确认 `prompt_service_test.go` 覆盖了 front matter、缺文档、非 SKILL.md、超大文件等用例且全部通过。

3. **前端检查**（改动涉及 `cclawchat` 时，必跑）
   - `cd cclawchat && npm test && npm run check`

4. **构建校验**（改动涉及可部署入口 `agentService` 时）
   - 运行 `./build_agentService.sh` 确认能编译产出 `bin/agentService`。

5. **静态检查**
   - 用编辑器错误检查确认改动文件无新错误（compile / lint）。
   - 忽略仓库预存的既有告警（例如 `controller/model/chat_model.go` 的 `sessions` 字段未导出），不要把预存问题当新错误修。

## 汇报格式

按以下格式给出结果摘要：

- ✅ / ❌ 每项检查的名称 + 结果
- 若有失败：给出失败原因与修复建议，修复后重跑直至通过
- 最后一句总结改动是否可安全提交

## 注意

- 遵循仓库 `AGENTS.md` 约定：代码注释/文档用中文；**不要**新增把 `platformutility` ↔ `tui` 环闭合的包级 import。
- 只跑与改动相关的检查，避免无谓的全量构建或测试。
