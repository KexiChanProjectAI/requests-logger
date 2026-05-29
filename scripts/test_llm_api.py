#!/usr/bin/env python3
"""
LLM API Test Suite

Tests an LLM API endpoint using both OpenAI Chat Completions API
and the newer Responses API format.

Usage:
    python scripts/test_llm_api.py
    python scripts/test_llm_api.py --model gpt-5.4
    python scripts/test_llm_api.py --chat-only
    python scripts/test_llm_api.py --responses-only
"""

import argparse
import os
import sys

try:
    from openai import OpenAI
except ImportError:
    print("ERROR: openai package not installed")
    print("Run: pip install openai")
    sys.exit(1)


# Default configuration
DEFAULT_BASE_URL = "https://llmoxy.com/v1"
DEFAULT_API_KEY = "sk-GFvUezvxMLBWQgGpCpaYkC5JTSLrOGYpGZTqlMr25r0wcrIF"
DEFAULT_MODEL = "gpt-5.4"


def get_env_config():
    """Read configuration from environment variables."""
    return {
        "base_url": os.environ.get("OPENAI_BASE_URL", DEFAULT_BASE_URL),
        "api_key": os.environ.get("OPENAI_API_KEY", DEFAULT_API_KEY),
    }


def test_chat_completions(client, model):
    """Test Chat Completions API (POST /v1/chat/completions)."""
    print("[Chat Completions]")
    try:
        response = client.chat.completions.create(
            model=model,
            messages=[{"role": "user", "content": "Say hello in one word."}],
            max_tokens=10,
        )

        response_id = response.id
        response_model = response.model
        content = response.choices[0].message.content

        print(f"  Response ID: {response_id}")
        print(f"  Model: {response_model}")
        print(f"  Content: {content}")

        if content and content.strip():
            print("  ✓ PASS")
            return True
        else:
            print("  ✗ FAIL: Empty content in response")
            return False

    except Exception as e:
        print(f"  ✗ FAIL: {e}")
        return False


def test_responses_api(client, model):
    """Test Responses API (POST /v1/responses)."""
    print("[Responses API]")
    try:
        response = client.responses.create(
            model=model,
            input="Say hello in one word.",
        )

        response_id = response.id
        response_model = response.model

        # Responses API returns output as a list; extract text content
        output_text = ""
        if hasattr(response, "output_text") and response.output_text:
            output_text = response.output_text
        elif hasattr(response, "output") and response.output:
            # Try to extract from first output item
            first_output = response.output[0]
            if hasattr(first_output, "content") and first_output.content:
                output_text = first_output.content[0].text if first_output.content else ""
            elif hasattr(first_output, "text"):
                output_text = first_output.text

        print(f"  Response ID: {response_id}")
        print(f"  Model: {response_model}")
        print(f"  Content: {output_text}")

        if output_text and output_text.strip():
            print("  ✓ PASS")
            return True
        else:
            print("  ✗ FAIL: Empty content in response")
            return False

    except Exception as e:
        print(f"  ✗ FAIL: {e}")
        return False


def main():
    parser = argparse.ArgumentParser(
        description="Test LLM API endpoint with Chat Completions and Responses APIs"
    )
    parser.add_argument(
        "--model",
        default=DEFAULT_MODEL,
        help=f"Model to use (default: {DEFAULT_MODEL})",
    )
    parser.add_argument(
        "--chat-only",
        action="store_true",
        help="Only run Chat Completions test",
    )
    parser.add_argument(
        "--responses-only",
        action="store_true",
        help="Only run Responses API test",
    )

    args = parser.parse_args()

    config = get_env_config()
    base_url = config["base_url"]
    api_key = config["api_key"]

    print("=" * 40)
    print("LLM API Test Suite")
    print(f"Base URL: {base_url}")
    print(f"Model: {args.model}")
    print("=" * 40)
    print()

    # Initialize client
    client = OpenAI(base_url=base_url, api_key=api_key)

    results = []
    tests_run = 0

    # Run Chat Completions test
    if not args.responses_only:
        tests_run += 1
        results.append(test_chat_completions(client, args.model))
        print()

    # Run Responses API test
    if not args.chat_only:
        tests_run += 1
        results.append(test_responses_api(client, args.model))
        print()

    # Summary
    print("=" * 40)
    passed = sum(results)
    total = len(results)
    print(f"Results: {passed}/{total} tests passed")
    print("=" * 40)

    # Exit with non-zero if any test failed
    if passed < total:
        sys.exit(1)
    sys.exit(0)


if __name__ == "__main__":
    main()
