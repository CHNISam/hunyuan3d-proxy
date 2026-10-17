"""Classify transport status separately from numeric business/account status."""


def _find_number(value, name):
    if isinstance(value, dict):
        child = value.get(name)
        if isinstance(child, (int, float)) and not isinstance(child, bool):
            return child
        for child in value.values():
            if isinstance(child, (dict, list)):
                found = _find_number(child, name)
                if found is not None:
                    return found
    elif isinstance(value, list):
        for child in value:
            found = _find_number(child, name)
            if found is not None:
                return found
    return None


def _http_ok(endpoint):
    code = endpoint.get("status")
    return isinstance(code, int) and 200 <= code < 300


def _business_ok(endpoint):
    body = endpoint.get("body")
    if isinstance(body, dict) and "errNo" in body:
        return body["errNo"] == 0
    return True


def analyze_quotas(main, studio):
    primary_main = main.get("quotainfo", {})
    primary_studio = studio.get("get_remaining_times", {})
    remaining_main = _find_number(primary_main.get("body"), "remainQuota")
    total_main = _find_number(primary_main.get("body"), "totalQuota")
    remaining_studio = _find_number(primary_studio.get("body"), "remainingTimes")
    failures = []
    for pool, data, key in (("main", main, "planCreditInfo"), ("studio", studio, "getuserinfo")):
        if key in data and not (_http_ok(data[key]) and _business_ok(data[key])):
            failures.append(pool + "." + key)
    return {"primary_quotas_verified": _http_ok(primary_main) and _business_ok(primary_main)
            and _http_ok(primary_studio) and _business_ok(primary_studio)
            and remaining_main is not None and remaining_studio is not None,
            "main_remaining": remaining_main, "main_total": total_main,
            "studio_remaining": remaining_studio, "auxiliary_failures": failures,
            "endpoint_http_statuses": {
                "main.quotainfo": primary_main.get("status"),
                "main.planCreditInfo": main.get("planCreditInfo", {}).get("status"),
                "studio.get_remaining_times": primary_studio.get("status"),
                "studio.getuserinfo": studio.get("getuserinfo", {}).get("status")}}
