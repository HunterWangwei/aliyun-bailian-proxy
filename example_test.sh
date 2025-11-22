#!/bin/bash

# 示例测试脚本
# 使用前请确保服务已启动

BASE_URL="http://localhost:8081"

echo "=== 健康检查 ==="
curl -s "$BASE_URL/health" | jq .
echo -e "\n"

echo "=== 普通聊天请求 ==="
curl -s -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [
      {"role": "system", "content": "你是一个有帮助的助手。"},
      {"role": "user", "content": "你好，请介绍一下你自己"}
    ],
    "temperature": 0.7,
    "max_tokens": 500
  }' | jq .
echo -e "\n"

echo "=== 流式聊天请求 ==="
curl -s -X POST "$BASE_URL/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [
      {"role": "user", "content": "用一句话介绍Go语言"}
    ],
    "stream": true
  }'
echo -e "\n"

