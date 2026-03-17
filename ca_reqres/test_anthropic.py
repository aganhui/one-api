#!/usr/bin/env python3
"""
测试：以 Anthropic 原生格式向 One API 发请求并解析结果
"""

import anthropic

# ============================================================
# 配置
# ============================================================
ANTHROPIC_AUTH_TOKEN       = "sk-SigfHOT7Y0TvP6AN1e3e485e4cE74014AcCf6fD268777205"
ANTHROPIC_BASE_URL         = "https://one-api-r6bq.onrender.com"
ANTHROPIC_DEFAULT_OPUS_MODEL   = "claude-4.6-opus-high-thinking"
ANTHROPIC_DEFAULT_SONNET_MODEL = "claude-4.6-sonnet-medium-thinking"
ANTHROPIC_DEFAULT_HAIKU_MODEL  = "gpt-5.4-medium"

client = anthropic.Anthropic(
    api_key=ANTHROPIC_AUTH_TOKEN,
    base_url=ANTHROPIC_BASE_URL,
)


def test_model(model_name: str, label: str, stream: bool = False):
    print(f"\n{'='*60}")
    print(f"模型: {label} ({model_name})  stream={stream}")
    print('='*60)

    messages = [
        {"role": "user", "content": "用一句话介绍你自己。"}
    ]

    try:
        if stream:
            # ---- 流式 ----
            with client.messages.stream(
                model=model_name,
                max_tokens=256,
                messages=messages,
            ) as s:
                print("[流式输出] ", end="", flush=True)
                for text in s.text_stream:
                    print(text, end="", flush=True)
                final = s.get_final_message()
            print()  # 换行
            print(f"stop_reason : {final.stop_reason}")
            print(f"usage       : input={final.usage.input_tokens}  output={final.usage.output_tokens}")
        else:
            # ---- 非流式 ----
            resp = client.messages.create(
                model=model_name,
                max_tokens=256,
                messages=messages,
            )
            print(f"id          : {resp.id}")
            print(f"stop_reason : {resp.stop_reason}")
            print(f"usage       : input={resp.usage.input_tokens}  output={resp.usage.output_tokens}")
            print(f"内容        :")
            for block in resp.content:
                if hasattr(block, 'text'):
                    print(f"  {block.text}")

    except anthropic.APIStatusError as e:
        print(f"[API错误] status={e.status_code}  message={e.message}")
    except anthropic.APIConnectionError as e:
        print(f"[连接错误] {e}")
    except Exception as e:
        import traceback
        print(f"[异常] {type(e).__name__}: {e}")
        traceback.print_exc()


if __name__ == "__main__":
    # # 非流式测试
    # test_model(ANTHROPIC_DEFAULT_OPUS_MODEL,   "Opus",   stream=False)
    # test_model(ANTHROPIC_DEFAULT_SONNET_MODEL, "Sonnet", stream=False)
    # test_model(ANTHROPIC_DEFAULT_HAIKU_MODEL,  "Haiku",  stream=False)

    # 流式测试
    test_model(ANTHROPIC_DEFAULT_OPUS_MODEL,   "Opus",   stream=True)
    test_model(ANTHROPIC_DEFAULT_SONNET_MODEL, "Sonnet", stream=True)
    test_model(ANTHROPIC_DEFAULT_HAIKU_MODEL,  "Haiku",  stream=True)

    print("\n\n全部测试完成。")

