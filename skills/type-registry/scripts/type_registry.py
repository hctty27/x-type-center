#!/usr/bin/env python3
import argparse
import json
import sys
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

SKILL_DIR = Path(__file__).resolve().parent.parent
CONFIG_PATH = SKILL_DIR / "config.json"


def load_config():
    with CONFIG_PATH.open("r", encoding="utf-8") as file:
        config = json.load(file)

    base_url = str(config.get("baseUrl", "")).strip().rstrip("/")
    if not base_url:
        raise RuntimeError(f"baseUrl is required in {CONFIG_PATH}")

    timeout = int(config.get("timeoutSeconds", 10))
    return base_url, timeout


def request_json(method, path, payload=None):
    base_url, timeout = load_config()
    body = None
    headers = {"Accept": "application/json"}

    if payload is not None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"

    request = Request(base_url + path, data=body, headers=headers, method=method)

    try:
        with urlopen(request, timeout=timeout) as response:
            raw = response.read().decode("utf-8")
    except HTTPError as error:
        raw = error.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {error.code}: {raw}") from error
    except URLError as error:
        raise RuntimeError(f"cannot connect to registry: {error.reason}") from error

    return json.loads(raw) if raw else {}


def print_json(data):
    print(json.dumps(data, ensure_ascii=False, indent=2))


def command_namespaces(_):
    print_json(request_json("GET", "/api/v1/namespaces"))


def command_status(args):
    print_json(request_json("GET", "/api/v1/namespaces/" + quote(args.namespace, safe="")))


def command_search(args):
    query = {
        "q": args.keyword,
        "page": 1,
        "pageSize": args.limit,
    }
    if args.namespace:
        query["namespace"] = args.namespace
    if args.project:
        query["project"] = args.project
    print_json(request_json("GET", "/api/v1/types/search?" + urlencode(query)))


def command_allocate(args):
    print_json(request_json("POST", "/api/v1/types/allocate", {
        "namespace": args.namespace,
        "project": args.project,
        "symbol": args.symbol,
        "description": args.description,
        "requirement": args.requirement,
        "requester": args.requester,
    }))


def command_validate(args):
    print_json(request_json("POST", "/api/v1/types/validate", {
        "namespace": args.namespace,
        "value": args.value,
        "symbol": args.symbol,
        "project": args.project,
    }))


def build_parser():
    parser = argparse.ArgumentParser(description="X Type Center Skill client")
    commands = parser.add_subparsers(dest="command", required=True)

    namespaces = commands.add_parser("namespaces")
    namespaces.set_defaults(handler=command_namespaces)

    status = commands.add_parser("status")
    status.add_argument("namespace")
    status.set_defaults(handler=command_status)

    search = commands.add_parser("search")
    search.add_argument("keyword", nargs="?", default="")
    search.add_argument("--namespace", default="")
    search.add_argument("--project", default="")
    search.add_argument("--limit", type=int, default=50)
    search.set_defaults(handler=command_search)

    allocate = commands.add_parser("allocate")
    allocate.add_argument("--namespace", required=True)
    allocate.add_argument("--project", required=True)
    allocate.add_argument("--symbol", required=True)
    allocate.add_argument("--description", required=True)
    allocate.add_argument("--requirement", default="")
    allocate.add_argument("--requester", default="")
    allocate.set_defaults(handler=command_allocate)

    validate = commands.add_parser("validate")
    validate.add_argument("--namespace", required=True)
    validate.add_argument("--value", type=int, required=True)
    validate.add_argument("--symbol", default="")
    validate.add_argument("--project", default="")
    validate.set_defaults(handler=command_validate)

    return parser


def main():
    parser = build_parser()
    args = parser.parse_args()
    try:
        args.handler(args)
    except (OSError, ValueError, RuntimeError, json.JSONDecodeError) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
