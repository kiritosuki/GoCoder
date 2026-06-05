#!/usr/bin/env python3
"""最小 MCP server — 返回两个简单工具，用于验证 GoCoder 的 MCP 连接"""
import json, sys, os

def log(msg):
    print(msg, file=sys.stderr, flush=True)

def send_response(id, result):
    resp = {"jsonrpc": "2.0", "id": id, "result": result}
    line = json.dumps(resp)
    log(f"<<< {line}")
    sys.stdout.write(line + "\n")
    sys.stdout.flush()

def main():
    log(f"MCP test server started, pid={os.getpid()}")

    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        log(f">>> {line}")
        try:
            req = json.loads(line)
        except json.JSONDecodeError:
            continue

        method = req.get("method", "")
        rid = req.get("id", 0)

        if method == "initialize":
            send_response(rid, {
                "protocolVersion": "2024-11-05",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "test-mcp", "version": "1.0.0"}
            })
        elif method == "notifications/initialized":
            pass  # notification, no response
        elif method == "tools/list":
            send_response(rid, {
                "tools": [
                    {
                        "name": "echo",
                        "description": "回显输入参数，用于测试 MCP 工具调用",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "message": {"type": "string", "description": "要回显的消息"}
                            },
                            "required": ["message"]
                        }
                    },
                    {
                        "name": "get_time",
                        "description": "返回当前时间",
                        "inputSchema": {
                            "type": "object",
                            "properties": {
                                "timezone": {"type": "string", "description": "时区，如 Asia/Shanghai"}
                            }
                        }
                    }
                ]
            })
        elif method == "tools/call":
            params = req.get("params", {})
            tool_name = params.get("name", "")
            arguments = params.get("arguments", {})

            if tool_name == "echo":
                msg = arguments.get("message", "no message")
                result = {
                    "content": [{"type": "text", "text": f"ECHO: {msg}"}]
                }
            elif tool_name == "get_time":
                from datetime import datetime
                tz = arguments.get("timezone", "UTC")
                now = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
                result = {
                    "content": [{"type": "text", "text": f"当前时间 ({tz}): {now}"}]
                }
            else:
                result = {
                    "content": [{"type": "text", "text": f"未知工具: {tool_name}"}],
                    "isError": True
                }
            send_response(rid, result)
        else:
            send_response(rid, {})

if __name__ == "__main__":
    main()
