---
name: web_fetch
description: 抓取网页内容并提取信息
user-invocable: true
disable-mode-invocation: false
emoji: 🌐
home_page: 
os:
  - darwin
  - linux
  - windows
requires:
  bins:
    - curl
---

# 网页抓取技能

抓取并解析网页内容。

## 用法

- 抓取网页正文：`/fetch <url>`
- 指定输出格式：`/fetch <url> -f markdown`

## 说明

- 仅抓取静态 HTML / 公开 JSON API，动态渲染页面可能不完整
- 单次抓取内容上限 256KB

