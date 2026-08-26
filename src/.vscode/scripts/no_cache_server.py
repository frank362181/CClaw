#!/usr/bin/env python3
"""
cclawchat 开发调试用静态文件服务器(禁用缓存)。

背景:浏览器(Safari/Chrome)会缓存旧的 JS 模块/HTML/CSS,
导致改了前端代码后仍加载旧模块(如聊天接口曾因缓存旧 api.js 报 nonce invalid)。
本服务器对所有响应附加 Cache-Control: no-store,彻底规避该问题。

用法: python3 no_cache_server.py [port]
目录:脚本位于 src/.vscode/scripts,启动时自动切换到工作区根(src)。
"""
import http.server
import os
import socket
import sys


class NoCacheHandler(http.server.SimpleHTTPRequestHandler):
    def end_headers(self):
        # 关闭缓存:每次都让浏览器重新拉取,避免命中旧模块
        self.send_header('Cache-Control', 'no-store, no-cache, must-revalidate, max-age=0')
        self.send_header('Pragma', 'no-cache')
        self.send_header('Expires', '0')
        super().end_headers()


class NoCacheHTTPServer(http.server.ThreadingHTTPServer):
    # 绑定 :: (IPv6 双栈),同时服务 IPv4/IPv6 的 localhost(Safari 走 IPv6)
    address_family = socket.AF_INET6


if __name__ == '__main__':
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 5500
    root = os.path.abspath(os.path.join(os.path.dirname(__file__), '..', '..'))
    os.chdir(root)
    server = NoCacheHTTPServer(('::', port), NoCacheHandler)
    print(f'[cclawchat] no-cache static server on :{port} root={root}')
    server.serve_forever()
