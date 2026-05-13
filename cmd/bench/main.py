#!/usr/bin/env python3
"""
nole provider benchmark runner.
Tests all configured providers across 12 task categories.
Outputs JSON results for routing matrix generation.
"""
import json
import os
import re
import statistics
import subprocess  # Kept for tests that assert secret providers do not call subprocess.run.
import sys
import time
import urllib.parse
import urllib.request

# Category -> list of test queries
CATEGORIES = {
    "general": [
        "capital of Australia",
        "how does Bluetooth work",
    ],
    "news": [
        "AI regulation 2026",
        "SpaceX latest launch",
    ],
    "docs": [
        "Go context package documentation",
        "Python asyncio gather vs wait",
    ],
    "academic": [
        "transformer attention mechanism paper",
        "reinforcement learning from human feedback survey",
    ],
    "factcheck": [
        "did NASA confirm alien life 2025",
        "is Python faster than Go",
    ],
    "semantic": [
        "alternatives to Stripe for payments",
        "open source LLM frameworks like LangChain",
    ],
    "extract": [
        "https://go.dev/doc/tutorial/getting-started",
        "https://docs.python.org/3/library/asyncio.html",
    ],
    "code": [
        "Go MCP server implementation example",
        "Python websocket server asyncio",
    ],
    "social": [
        "Go generics reddit discussion",
        "Rust vs Go developer sentiment 2025",
    ],
    "people": [
        "Andrej Karpathy current projects",
        "Jeff Dean Google AI role",
    ],
    "pricing": [
        "OpenAI API pricing gpt-4o 2025",
        "Vercel vs Netlify pricing comparison",
    ],
    "research": [
        "state of LLM agents 2025 survey",
        "retrieval augmented generation best practices",
    ],
}


def _read_json(req, timeout):
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


def _json_request(url, payload, timeout, headers=None, method="POST"):
    body = json.dumps(payload).encode("utf-8")
    req_headers = {"Content-Type": "application/json", **(headers or {})}
    req = urllib.request.Request(url, data=body, headers=req_headers, method=method)
    return _read_json(req, timeout)


def run_search(provider, query, timeout=15):
    """Run a search via direct provider APIs."""
    env = os.environ.copy()
    result = {
        "provider": provider,
        "query": query,
        "success": False,
        "result_count": 0,
        "latency_ms": 0,
        "has_snippets": False,
        "has_urls": False,
        "error": None,
        "titles": [],
    }

    start = time.time()
    try:
        if provider == "brave":
            r = run_brave(query, env, timeout)
        elif provider == "tavily":
            r = run_tavily(query, env, timeout)
        elif provider == "jina":
            r = run_jina_search(query, env, timeout)
        elif provider == "firecrawl":
            r = run_firecrawl_search(query, env, timeout)
        elif provider == "ddgs":
            r = run_ddgs(query, timeout)
        else:
            result["error"] = f"unknown provider: {provider}"
            return result

        elapsed = (time.time() - start) * 1000
        result["latency_ms"] = round(elapsed, 1)
        result.update(r)
        if not result.get("error"):
            result["success"] = True
    except Exception as e:
        elapsed = (time.time() - start) * 1000
        result["latency_ms"] = round(elapsed, 1)
        result["error"] = str(e)[:200]

    return result


def run_brave(query, env, timeout):
    key = env.get("BRAVE_SEARCH_API_KEY") or env.get("BRAVE_API_KEY")
    if not key:
        return {"error": "no key"}
    encoded_query = urllib.parse.quote(query)
    req = urllib.request.Request(
        f"https://api.search.brave.com/res/v1/web/search?q={encoded_query}&count=5",
        headers={
            "X-Subscription-Token": key,
            "Accept": "application/json",
        },
        method="GET",
    )
    d = _read_json(req, timeout)
    if "error" in d:
        raise ValueError(f"brave api error: {d['error']}")
    results = d.get("web", {}).get("results", [])
    return {
        "result_count": len(results),
        "has_snippets": all(r.get("description") for r in results[:3]),
        "has_urls": all(r.get("url") for r in results[:3]),
        "titles": [r.get("title", "")[:80] for r in results[:3]],
    }


def run_tavily(query, env, timeout):
    key = env.get("TAVILY_API_KEY")
    if not key:
        return {"error": "no key"}
    d = _json_request(
        "https://api.tavily.com/search",
        {"query": query, "max_results": 5, "api_key": key, "search_depth": "basic"},
        timeout,
    )
    results = d.get("results", [])
    return {
        "result_count": len(results),
        "has_snippets": all(r.get("content") for r in results[:3]),
        "has_urls": all(r.get("url") for r in results[:3]),
        "titles": [r.get("title", "")[:80] for r in results[:3]],
    }


def run_jina_search(query, env, timeout):
    key = env.get("JINA_API_KEY")
    if not key:
        return {"error": "no key"}
    d = _json_request(
        "https://s.jina.ai/",
        {"q": query, "num": 5},
        timeout,
        headers={
            "Authorization": f"Bearer {key}",
            "Accept": "application/json",
        },
    )
    results = d.get("data", [])
    return {
        "result_count": len(results),
        "has_snippets": all(r.get("description") for r in results[:3]),
        "has_urls": all(r.get("url") for r in results[:3]),
        "titles": [r.get("title", "")[:80] for r in results[:3]],
    }


def run_firecrawl_search(query, env, timeout):
    key = env.get("FIRECRAWL_API_KEY")
    if not key:
        return {"error": "no key"}
    d = _json_request(
        "https://api.firecrawl.dev/v2/search",
        {"query": query, "limit": 5},
        timeout,
        headers={"Authorization": f"Bearer {key}"},
    )
    web = d.get("data", {}).get("web", [])
    return {
        "result_count": len(web),
        "has_snippets": all(r.get("description") or r.get("snippet") for r in web[:3]) if web else False,
        "has_urls": all(r.get("url") for r in web[:3]) if web else False,
        "titles": [r.get("title", "")[:80] for r in web[:3]],
    }


def run_ddgs(query, timeout):
    body = urllib.parse.urlencode({"q": query, "b": "Web Search"}).encode("utf-8")
    req = urllib.request.Request(
        "https://html.duckduckgo.com/html/",
        data=body,
        headers={
            "Content-Type": "application/x-www-form-urlencoded",
            "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
        },
        method="POST",
    )
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        html = resp.read().decode("utf-8", errors="replace")
    links = re.findall(r'class="result__a"[^>]*href="([^"]*)"', html)
    titles = re.findall(r'class="result__a"[^>]*>([^<]+)', html)
    # Filter ads
    good_links = [(l, t) for l, t in zip(links, titles)
                  if "duckduckgo.com/y.js" not in l and "bing.com/aclick" not in l]
    return {
        "result_count": len(good_links),
        "has_snippets": len(good_links) > 0,
        "has_urls": len(good_links) > 0,
        "titles": [t[:80] for _, t in good_links[:3]],
    }


def score_result(result):
    """Simple quality score 0-100."""
    if not result["success"]:
        return 0
    s = 0
    s += min(result["result_count"] * 15, 45)  # up to 45 for result count
    if result["has_snippets"]:
        s += 20
    if result["has_urls"]:
        s += 15
    if result["result_count"] >= 3:
        s += 10
    # Latency bonus: under 2s = 10, under 5s = 5, over = 0
    if result["latency_ms"] < 2000:
        s += 10
    elif result["latency_ms"] < 5000:
        s += 5
    return min(s, 100)


def main():
    providers = ["brave", "tavily", "jina", "firecrawl", "ddgs"]

    # Load env from optional env file (--env flag or NOLE_ENV).
    # SEARCHMCP_ENV remains as a deprecated migration alias only.
    env_path = os.environ.get("NOLE_ENV", os.environ.get("SEARCHMCP_ENV", ""))
    if not env_path and len(sys.argv) > 1:
        for i, arg in enumerate(sys.argv):
            if arg == "--env" and i + 1 < len(sys.argv):
                env_path = sys.argv[i + 1]
                break
    if env_path and os.path.exists(env_path):
        with open(env_path) as f:
            for line in f:
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    k, v = line.split("=", 1)
                    os.environ.setdefault(k.strip(), v.strip())

    all_results = {}
    errors = []

    for cat, queries in CATEGORIES.items():
        print(f"\n{'='*60}", file=sys.stderr)
        print(f"CATEGORY: {cat}", file=sys.stderr)
        print(f"{'='*60}", file=sys.stderr)
        cat_results = {}

        for provider in providers:
            prov_scores = []
            prov_latencies = []
            prov_errors = 0
            sample_titles = []

            for query in queries:
                print(f"  {provider:12s} | {query[:50]}", file=sys.stderr)
                r = run_search(provider, query)
                s = score_result(r)
                prov_scores.append(s)
                prov_latencies.append(r["latency_ms"])
                if not r["success"]:
                    prov_errors += 1
                    errors.append(f"{cat}/{provider}/{query[:30]}: {r.get('error', 'unknown')}")
                if r["titles"] and not sample_titles:
                    sample_titles = r["titles"][:2]
                time.sleep(0.5)  # be gentle

            avg_score = statistics.mean(prov_scores) if prov_scores else 0
            avg_latency = statistics.mean(prov_latencies) if prov_latencies else 0
            cat_results[provider] = {
                "avg_score": round(avg_score, 1),
                "avg_latency_ms": round(avg_latency, 1),
                "errors": prov_errors,
                "sample_titles": sample_titles,
            }
            status = "OK" if prov_errors == 0 else f"{prov_errors} ERR"
            print(f"    -> score: {avg_score:5.1f}  latency: {avg_latency:6.0f}ms  {status}", file=sys.stderr)

        all_results[cat] = cat_results

    # Generate routing matrix
    print(f"\n{'='*60}", file=sys.stderr)
    print("ROUTING MATRIX (sorted by score per category)", file=sys.stderr)
    print(f"{'='*60}", file=sys.stderr)

    matrix = {}
    for cat, provs in all_results.items():
        ranked = sorted(provs.items(), key=lambda x: x[1]["avg_score"], reverse=True)
        ranked = [(p, d) for p, d in ranked if d["avg_score"] > 0]
        order = [p for p, _ in ranked]
        matrix[cat] = order
        line = " -> ".join(f"{p}({d['avg_score']:.0f})" for p, d in ranked)
        print(f"  {cat:12s}: {line}", file=sys.stderr)

    output = {
        "categories": all_results,
        "matrix": matrix,
        "errors": errors,
        "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S"),
    }

    print(json.dumps(output, indent=2))


if __name__ == "__main__":
    main()
