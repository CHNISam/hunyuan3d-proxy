"""Called by the in-memory WeChat probe; never prints or persists credentials."""
import asyncio
import json
import os
from pathlib import Path
import sys
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "toolchain/python"))
import httpx2
from mcp import Client
from mcp.client.streamable_http import streamable_http_client
from query_evidence import analyze_quotas


def summarize(value):
    output = []
    if isinstance(value, dict):
        for key, child in value.items():
            if key.lower() in {"status", "errno", "errcode", "errorcode", "remainquota", "totalquota", "remainingtimes", "remaintimes", "totaltimes"} and isinstance(child, (int, float, bool)):
                output.append({"field": key, "value": child})
            elif key not in {"raw", "full"}:
                output.extend(summarize(child))
    elif isinstance(value, list):
        for child in value:
            output.extend(summarize(child))
    return output[:40]


async def main():
    base = os.environ.pop("RESEARCH_QUERY_URL")
    token = os.environ.pop("RESEARCH_QUERY_TOKEN")
    assert urlparse(base).hostname == "127.0.0.1"
    report = {"login_method": "owner-approved WeChat QR", "credentials": "memory only; never serialized",
              "generation_calls": 0, "texture_calls": 0, "cloud_calls": 0, "queries": []}
    phase = "start"
    pools = {}
    try:
        async with httpx2.AsyncClient(headers={"Authorization": "Bearer " + token}, trust_env=False, timeout=65) as http:
            for pool in ("main", "concept"):
                phase = "quota " + pool
                response = await http.get(base + "/api/credit/" + pool)
                data = response.json().get("data", {})
                pools[pool] = data
                record = {"pool": pool, "local_http": response.status_code, "numeric_fields": summarize(data)}
                report["queries"].append(record)
                print(json.dumps(record), flush=True)
            phase = "MCP task lists"
            async with Client(streamable_http_client(base + "/mcp", http_client=http), mode="legacy", read_timeout_seconds=65) as client:
                report["mcp_tool_count"] = len((await client.list_tools()).tools)
                for name in ("hunyuan_main_creations", "hunyuan_works"):
                    result = await client.call_tool(name, {"limit": 5})
                    data = json.loads(next(block.text for block in result.content if block.type == "text"))
                    record = {"tool": name, "is_error": result.is_error, "numeric_fields": summarize(data), "count": data.get("count")}
                    report["queries"].append(record)
                    print(json.dumps(record), flush=True)
            report["quota_assessment"] = analyze_quotas(pools["main"], pools["concept"])
            report["account_quota_verified"] = report["quota_assessment"]["primary_quotas_verified"]
            return 0 if report["account_quota_verified"] else 1
    except Exception as exc:
        report.update(failed_phase=phase, error_type=type(exc).__name__, account_quota_verified=False)
        return 1
    finally:
        token = ""
        (ROOT / "evidence/wechat-account-readonly.json").write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8")


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
