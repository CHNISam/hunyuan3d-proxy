# TASK-206 interactive login probe

## Site and quota scope

- China web edition: https://3d.hunyuan.tencent.com/
- International web edition: https://hy3d.tencent.ai/

The owner confirmed on 2026-10-17 that the two web editions have independent
quota pools and expects free quota in both accounts. International account
balance has not been measured in this pilot. Do not infer it from China results.
The observed China main-site and Studio balances are also tracked separately;
neither web edition establishes Tencent Cloud API free-credit eligibility.
This probe targets the China edition only. No international generation or
billable cloud API request is authorized by this documentation update.

This opt-in research smoke test uses the repository's WeChat QR flow, keeps
the session only in memory, and checks account quotas and MCP task queries.
It never submits generation, texture or cloud billing calls. It does not enable
public login endpoints or change the default spending lock.

From the proxy checkout, with the isolated MCP Python SDK available in the parent
workspace's `toolchain/python` directory (or installed in the selected Python):

```powershell
$env:RESEARCH_INTERACTIVE_WECHAT='1'
go test -tags researchlive -run '^TestResearchWeChatReadOnlySession$' -count=1 -v -timeout 8m
```

The owner opens the temporary official HTTPS image locator in
`../evidence/wechat-qr-url.txt` and scans it with WeChat. Browser automation may
be unable to open the provider; the owner can perform this step directly.
No Cookie or OAuth code needs to be copied into chat. After login the local
listener binds only 127.0.0.1, requires a random bearer, and stops after querying.
The session and temporary QR files are cleared when the test finishes.

Reports contain numeric quota/status evidence, not account identifiers, tokens
or model URLs. `status` inside a response body is a business value, not an HTTP
status. Primary quota checks and auxiliary endpoint failures are reported
separately. The first live run exposed a helper error in that distinction; its
original failed report is retained rather than rewritten as a successful run.

Offline classifier regression tests:

```powershell
python -m unittest discover -s research -p 'test_*.py'
```

This does not change the upstream project's learning-only scope or license status.
