---
name: code_search
description: 在工作区中检索代码与文档，支持关键字与正则匹配
user-invocable: true
disable-mode-invocation: false
emoji: 🔍
home_page: 
os:
  - darwin
  - linux
  - windows
requires:
  bins:
    - grep
---

# 代码搜索技能

在工作区中检索代码与文档内容。

## 用法

- 使用关键字搜索：`/search <keyword>`
- 使用正则搜索：`/search -re <pattern>`
- 限定文件类型：`/search -t go`

## 说明

- 搜索范围默认覆盖当前工作区
- 结果按相关度排序，最多返回 50 条
