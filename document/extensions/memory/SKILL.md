---
name: memory
description: 跨会话持久化知识与偏好，实现记忆管理
user-invocable: true
disable-mode-invocation: false
emoji: 🧠
home_page: 
os:
  - darwin
  - linux
  - windows
---

# 记忆管理技能

跨会话持久化知识、事实与用户偏好。

## 用法

- 记住一条事实：`/remember <content>`
- 查询记忆：`/recall <keyword>`
- 删除记忆：`/forget <keyword>`

## 说明

- 记忆按会话与用户隔离存储
- 支持全文检索与按时间排序
