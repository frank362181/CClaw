# LLM 配置信息

    主流厂商 context window 一览
    DeepSeek（官网数据）
    模型	Context Length	最大输出
    deepseek-v4-flash	1M (100万)	384K
    deepseek-v4-pro	1M (100万)	384K
    deepseek-v4-flash-vision-exp	1M (100万)	384K
    deepseek-v3 / v3.1 / v3.2 / r1（旧）	128K	—
    Qwen 通义千问（阿里云百炼）
    模型	上下文窗口
    qwen3.8-max / qwen3.7-max	1M
    qwen3.7-plus / qwen3.7-flash	1M
    qwen3-coder-plus / coder-flash	1M
    qwen-long（超长文本专用）	10M (1000万)
    qwen-turbo / qwq-plus	128K
    qwen3 系列开源版	128K~256K
    GLM 智谱（开放平台 + 百炼三方）
    模型	上下文窗口	最大输出
    GLM-4-Plus	128K	4K
    GLM-4-Air-250414	128K	16K
    GLM-4-AirX	8K（低延迟版，注意偏小）	4K
    GLM-4-Flash-250414 / FlashX	128K	16K
    glm-5.2（最新，百炼）	1M	—
    glm-5 / 5.1 / 4.7	198K	—
    glm-4.5	128K	—
    Kimi（月之暗面）& MiniMax
    模型	上下文窗口
    kimi-k2.7-code	256K
    kimi-k2.5 / k2-thinking	256K
    MiniMax-M3	192K
    mimo-v2.5-pro	1M
    MiniMax-M2.7 / M2.5 / M2.1	192K / 200K

# 项目简介

## 1. done

- 生成SSL所需的cert/key文件命令：(open command line with Administrator)
  - 生成CA私钥
    openssl genrsa -out ca.key 2048 //生成CA私钥
  - 生成CA根证书
    openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 -out ca.crt -subj "/C=CN/ST=Beijing/L=Beijing/O=Dev/CN=Local CA"
  - 生成服务器私钥
    openssl genrsa -out server.key 2048
  - 生成CSR（不包含SAN）
    openssl req -new -key server.key -out server.csr  -subj "/C=CN/ST=Beijing/L=Beijing/O=Dev/CN=localhost"
    - 使用CA签署，同时添加SAN
      - 创建server.cnf文件，其文件内容：
        [req]
        default_bits = 2048
        prompt = no
        default_md = sha256
        distinguished_name = dn
        req_extensions = v3_req

        [dn]
        C = CN
        ST = Beijing
        L = Beijing
        O = Dev
        CN = localhost
      
        [v3_req]
        keyUsage = keyEncipherment, digitalSignature
        extendedKeyUsage = serverAuth
        subjectAltName = @alt_names
      
        [alt_names]
        DNS.1 = localhost
        DNS.2 = *.localhost
        IP.1 = 127.0.0.1
        IP.2 = ::1
      - 执行1：
        openssl req -new -key server.key -out server.csr -config server.cnf
      - 执行2：
        openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out server.crt -days 365 -sha256 -extensions v3_req -extfile server.cnf
  - 验证证书
    openssl x509 -in server.crt -text -noout | findstr "Subject Alternative Name"
  - 添加证书
    certutil -addstore "Root" ca.crt

- 重新生成swagger文档
  - cd D:/MyIdea/CClaw/src/agentService
  - swag fmt ## 清空缓存
  - swag init -d .,./../platformutility,./../cclawservice --parseInternal --parseDependency --parseDependency ## 生成swagger文档

