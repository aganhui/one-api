import requests
import json
import random
import os
from collections import defaultdict

# 直接使用 req.json 中的实际参数
API_KEY = "eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJpc3MiOiJpc3N1c2VyIiwiYXVkIjoiYXVkaWVuY2UiLCJ0ZW5hbnRfaWQiOiI0MTI2NjMiLCJyb2xlX25hbWUiOiIiLCJ1c2VyX2lkIjoiMjAyMDA0NzE4NTU5MzMzOTkwNSIsInJvbGVfaWQiOiItMSIsInVzZXJfbmFtZSI6ImJpbmdhaHVpIiwib2F1dGhfaWQiOiIyMDIwMDQ2ODAwMjU0MjQyODE4IiwidG9rZW5fdHlwZSI6ImFjY2Vzc190b2tlbiIsImRlcHRfaWQiOiItMSIsImFjY291bnQiOiJiaW5nYWh1aSIsImNsaWVudF9pZCI6InNhYmVyIiwiZXhwIjoxNzc0MjYzNTI2LCJuYmYiOjE3NzM2NTg3MjZ9.5cszL93uCIMdqOCjeqCrSOsPJydHyUag6AXO9W9Qnso"

# 可用的模型列表
AVAILABLE_MODELS = [
    "gpt-5.4-medium",
    "claude-4.6-opus-high-thinking",
    "claude-4.6-sonnet-medium-thinking"
]
DEFAULT_MODEL = "claude-4.6-opus-high-thinking"

# API 统计信息文件
STATS_FILE = "api_stats.json"

# API 统计信息
api_stats = defaultdict(lambda: {"success": 0, "failure": 0})


def load_stats():
    """从文件加载 API 统计信息"""
    global api_stats
    
    if os.path.exists(STATS_FILE):
        try:
            with open(STATS_FILE, "r") as f:
                data = json.load(f)
                api_stats = defaultdict(lambda: {"success": 0, "failure": 0}, data)
                print(f"已加载统计数据 ({len(api_stats)} 个 API)\n")
        except Exception as e:
            print(f"加载统计数据失败: {e}\n")


def save_stats():
    """保存 API 统计信息到文件"""
    try:
        with open(STATS_FILE, "w") as f:
            json.dump(dict(api_stats), f, indent=2, ensure_ascii=False)
    except Exception as e:
        print(f"保存统计数据失败: {e}")


def load_apis():
    """从 apis.txt 加载 API 地址列表"""
    import os
    
    # 尝试多个可能的路径
    possible_paths = [
        "apis.txt",
        "ca_reqres/apis.txt",
        os.path.join(os.path.dirname(__file__), "apis.txt")
    ]
    
    for path in possible_paths:
        try:
            with open(path, "r") as f:
                apis = [line.strip() for line in f if line.strip()]
            if apis:
                print(f"已加载 {len(apis)} 个 API 地址\n")
                return apis
        except FileNotFoundError:
            continue
    
    print("错误: 找不到 apis.txt 文件")
    print("请确保 apis.txt 在以下位置之一:")
    for path in possible_paths:
        print(f"  - {path}")
    return []


def call_claude_api(model=None, max_retries=2):
    """
    调用 Claude API，支持重试和 API 轮询
    
    Args:
        model: 使用的模型名称，默认为 claude-4.6-opus-high-thinking
        max_retries: 最多重试次数（默认 2 次，加上首次共 3 次）
    """
    
    # 加载统计数据
    load_stats()
    
    # 使用默认模型或指定的模型
    if model is None:
        model = DEFAULT_MODEL
    
    if model not in AVAILABLE_MODELS:
        print(f"错误: 模型 '{model}' 不可用")
        print(f"可用的模型: {', '.join(AVAILABLE_MODELS)}")
        return
    
    # 加载 API 列表
    apis = load_apis()
    if not apis:
        return
    
    print("开始调用 API...")
    print(f"使用模型: {model}")
    print(f"可用 API 数量: {len(apis)}\n")
    
    # 请求头
    headers = {
        "content-type": "application/json",
        "accept": "text/event-stream",
        "x-api-key": API_KEY,
    }
    
    # 请求体
    payload = {
        "model": model,
        "messages": [
            {
                "role": "user",
                "content": [
                    {
                        "type": "text",
                        "text": "你好"
                    }
                ]
            }
        ]
    }
    
    # 随机打乱 API 列表
    shuffled_apis = apis.copy()
    random.shuffle(shuffled_apis)
    
    # 尝试调用 API，最多 3 次（首次 + 2 次重试）
    for attempt in range(max_retries + 1):
        if attempt >= len(shuffled_apis):
            print(f"\n错误: 没有更多的 API 可以尝试")
            break
        
        api_url = shuffled_apis[attempt]
        print(f"[尝试 {attempt + 1}/{max_retries + 1}] 使用 API: {api_url}")
        
        try:
            # 发送请求
            response = requests.post(api_url, headers=headers, json=payload, timeout=10)
            
            if response.status_code == 200:
                # 成功
                api_stats[api_url]["success"] += 1
                save_stats()
                print(f"✓ 成功\n")
                
                # 解析响应
                data = response.json()
                print("=== 响应内容 ===\n")
                
                # 提取文本内容
                if "choices" in data and len(data["choices"]) > 0:
                    choice = data["choices"][0]
                    
                    if "message" in choice:
                        content = choice["message"].get("content", "")
                        print(content)
                    elif "delta" in choice:
                        content = choice["delta"].get("content", "")
                        print(content)
                
                print("\n")
                return
            else:
                # 失败
                api_stats[api_url]["failure"] += 1
                save_stats()
                print(f"✗ 失败 (状态码: {response.status_code})")
                if attempt < max_retries:
                    print(f"  准备重试...\n")
                else:
                    print(f"  已达到最大重试次数\n")
        
        except requests.exceptions.Timeout:
            api_stats[api_url]["failure"] += 1
            save_stats()
            print(f"✗ 超时")
            if attempt < max_retries:
                print(f"  准备重试...\n")
            else:
                print(f"  已达到最大重试次数\n")
        
        except requests.exceptions.RequestException as e:
            api_stats[api_url]["failure"] += 1
            save_stats()
            print(f"✗ 请求异常: {e}")
            if attempt < max_retries:
                print(f"  准备重试...\n")
            else:
                print(f"  已达到最大重试次数\n")
        
        except Exception as e:
            api_stats[api_url]["failure"] += 1
            save_stats()
            print(f"✗ 错误: {e}")
            if attempt < max_retries:
                print(f"  准备重试...\n")
            else:
                print(f"  已达到最大重试次数\n")
    
    print("所有 API 调用均失败")


def print_stats():
    """打印 API 统计信息"""
    if not api_stats:
        print("暂无统计数据")
        return
    
    print("\n=== API 统计信息 ===\n")
    print(f"{'API 地址':<50} {'成功':<8} {'失败':<8} {'成功率':<10}")
    print("-" * 80)
    
    for api, stats in sorted(api_stats.items()):
        success = stats["success"]
        failure = stats["failure"]
        total = success + failure
        success_rate = (success / total * 100) if total > 0 else 0
        print(f"{api:<50} {success:<8} {failure:<8} {success_rate:.1f}%")
    
    print("-" * 80)
    total_success = sum(s["success"] for s in api_stats.values())
    total_failure = sum(s["failure"] for s in api_stats.values())
    total = total_success + total_failure
    overall_rate = (total_success / total * 100) if total > 0 else 0
    print(f"{'总计':<50} {total_success:<8} {total_failure:<8} {overall_rate:.1f}%\n")
    
    # 保存统计数据
    save_stats()


if __name__ == "__main__":
    import sys
    
    # 从命令行参数获取模型，或使用默认模型
    model = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_MODEL
    call_claude_api(model)
    
    # 打印统计信息
    print_stats()
