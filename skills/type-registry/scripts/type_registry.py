#!/usr/bin/env python3
import argparse
import io
import json
import shutil
import sys
import tempfile
import zipfile
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen

SKILL_DIR = Path(__file__).resolve().parent.parent
CONFIG_PATH = SKILL_DIR / "config.json"
MANIFEST_PATH = SKILL_DIR / "manifest.json"
WRITE_COMMANDS = {"allocate", "revoke"}


def load_config():
    with CONFIG_PATH.open("r", encoding="utf-8") as file:
        config = json.load(file)

    base_url = str(config.get("baseUrl", "")).strip().rstrip("/")
    if not base_url:
        raise RuntimeError(f"baseUrl is required in {CONFIG_PATH}")

    timeout = int(config.get("timeoutSeconds", 10))
    return base_url, timeout


def load_manifest():
    with MANIFEST_PATH.open("r", encoding="utf-8") as file:
        manifest = json.load(file)

    version = str(manifest.get("version", "")).strip()
    if not version:
        raise RuntimeError(f"version is required in {MANIFEST_PATH}")
    return manifest


def local_version():
    return str(load_manifest()["version"]).strip()


def version_tuple(value):
    core = str(value).strip().lstrip("v").split("+", 1)[0].split("-", 1)[0]
    parts = core.split(".")
    if len(parts) != 3:
        raise RuntimeError(f"invalid semantic version: {value}")
    try:
        return tuple(int(part) for part in parts)
    except ValueError as error:
        raise RuntimeError(f"invalid semantic version: {value}") from error


def request_headers(accept="application/json"):
    version = local_version()
    return {
        "Accept": accept,
        "User-Agent": f"x-type-center-skill/{version}",
        "X-Type-Registry-Skill-Version": version,
    }


def request_bytes(method, path, payload=None, accept="application/json"):
    base_url, timeout = load_config()
    body = None
    headers = request_headers(accept)

    if payload is not None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        headers["Content-Type"] = "application/json"

    request = Request(base_url + path, data=body, headers=headers, method=method)

    try:
        with urlopen(request, timeout=timeout) as response:
            return response.read()
    except HTTPError as error:
        raw = error.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {error.code}: {raw}") from error
    except URLError as error:
        raise RuntimeError(f"cannot connect to registry: {error.reason}") from error


def request_json(method, path, payload=None):
    raw = request_bytes(method, path, payload)
    return json.loads(raw.decode("utf-8")) if raw else {}


def print_json(data):
    print(json.dumps(data, ensure_ascii=False, indent=2))


def fetch_skill_version():
    return request_json("GET", "/api/v1/skill-version")


def check_skill_version(command):
    info = fetch_skill_version()
    current = local_version()
    latest = str(info.get("latestVersion", "")).strip()
    minimum = str(info.get("minSupportedVersion", "")).strip()

    if latest and version_tuple(current) < version_tuple(latest):
        package_state = "可更新" if info.get("packageReady") else "服务器下载包尚未更新"
        print(
            f"WARNING: Type Registry Skill {current} -> {latest}，{package_state}。"
            "执行: python3 <skill-dir>/scripts/type_registry.py update",
            file=sys.stderr,
        )

    if minimum and version_tuple(current) < version_tuple(minimum) and command in WRITE_COMMANDS:
        raise RuntimeError(
            f"当前 Skill {current} 低于最低支持版本 {minimum}，禁止执行写操作，请先运行 update"
        )


def command_namespaces(_):
    print_json(request_json("GET", "/api/v1/namespaces"))


def command_status(args):
    print_json(request_json("GET", "/api/v1/namespaces/" + quote(args.namespace, safe="")))


def command_resolve(args):
    query = {"q": args.keyword}
    print_json(request_json("GET", "/api/v1/namespaces/resolve?" + urlencode(query)))


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
    if args.count < 1 or args.count > 100:
        raise RuntimeError("count must be between 1 and 100")
    if args.count > 1 and args.symbol:
        raise RuntimeError("symbol must be empty when count is greater than 1")

    print_json(request_json("POST", "/api/v1/types/allocate-batch", {
        "namespace": args.namespace,
        "count": args.count,
        "project": args.project,
        "symbol": args.symbol,
        "description": args.description,
        "requirement": args.requirement,
        "requester": args.requester,
    }))


def command_revoke(args):
    if bool(args.id) == bool(args.allocation):
        raise RuntimeError("exactly one of --id or --allocation is required")
    payload = {
        "requester": args.requester,
        "reason": args.reason,
    }
    if args.id:
        path = "/api/v1/types/" + quote(str(args.id), safe="") + "/revoke"
    else:
        path = "/api/v1/allocations/" + quote(args.allocation, safe="") + "/revoke"
    print_json(request_json("POST", path, payload))


def command_validate(args):
    print_json(request_json("POST", "/api/v1/types/validate", {
        "namespace": args.namespace,
        "value": args.value,
        "symbol": args.symbol,
        "project": args.project,
    }))


def command_version(_):
    print_json({
        "localVersion": local_version(),
        "server": fetch_skill_version(),
    })


def safe_skill_members(archive):
    prefix = "type-registry/"
    members = []
    for info in archive.infolist():
        if info.is_dir() or not info.filename.startswith(prefix):
            continue
        relative = Path(info.filename[len(prefix):])
        if not relative.parts or relative.is_absolute() or ".." in relative.parts:
            raise RuntimeError(f"unsafe skill package entry: {info.filename}")
        members.append((info, relative))
    return members


def command_update(_):
    info = fetch_skill_version()
    latest = str(info.get("latestVersion", "")).strip()
    if not latest:
        raise RuntimeError("server did not return latestVersion")
    if not info.get("packageReady"):
        package_version = info.get("packageVersion") or "unknown"
        raise RuntimeError(
            f"server skill package is not ready: package={package_version}, latest={latest}"
        )

    before = local_version()
    data = request_bytes("GET", "/api/v1/skill-package", accept="application/zip")
    with zipfile.ZipFile(io.BytesIO(data)) as archive:
        members = safe_skill_members(archive)
        if not members:
            raise RuntimeError("skill package is empty")

        with tempfile.TemporaryDirectory(prefix="type-registry-update-") as temp_dir:
            temp_skill = Path(temp_dir) / "type-registry"
            for info_item, relative in members:
                target = temp_skill / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                with archive.open(info_item) as source, target.open("wb") as output:
                    shutil.copyfileobj(source, output)

            required = [
                temp_skill / "SKILL.md",
                temp_skill / "config.json",
                temp_skill / "manifest.json",
                temp_skill / "scripts" / "type_registry.py",
            ]
            missing = [str(path.relative_to(temp_skill)) for path in required if not path.is_file()]
            if missing:
                raise RuntimeError("skill package missing: " + ", ".join(missing))

            with (temp_skill / "manifest.json").open("r", encoding="utf-8") as file:
                package_manifest = json.load(file)
            package_version = str(package_manifest.get("version", "")).strip()
            if package_version != latest:
                raise RuntimeError(
                    f"skill package version mismatch: package={package_version}, latest={latest}"
                )

            for source in temp_skill.rglob("*"):
                if not source.is_file():
                    continue
                relative = source.relative_to(temp_skill)
                target = SKILL_DIR / relative
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(source, target)

    print_json({
        "updated": True,
        "fromVersion": before,
        "toVersion": local_version(),
    })


def build_parser():
    parser = argparse.ArgumentParser(description="X Type Center Skill client")
    commands = parser.add_subparsers(dest="command", required=True)

    namespaces = commands.add_parser("namespaces")
    namespaces.set_defaults(handler=command_namespaces)

    status = commands.add_parser("status")
    status.add_argument("namespace")
    status.set_defaults(handler=command_status)

    resolve = commands.add_parser("resolve")
    resolve.add_argument("keyword")
    resolve.set_defaults(handler=command_resolve)

    search = commands.add_parser("search")
    search.add_argument("keyword", nargs="?", default="")
    search.add_argument("--namespace", default="")
    search.add_argument("--project", default="")
    search.add_argument("--limit", type=int, default=50)
    search.set_defaults(handler=command_search)

    allocate = commands.add_parser("allocate")
    allocate.add_argument("--namespace", required=True)
    allocate.add_argument("--count", type=int, default=1)
    allocate.add_argument("--project", default="")
    allocate.add_argument("--symbol", default="")
    allocate.add_argument("--description", default="")
    allocate.add_argument("--requirement", default="")
    allocate.add_argument("--requester", default="")
    allocate.set_defaults(handler=command_allocate)

    revoke = commands.add_parser("revoke")
    revoke.add_argument("--id", type=int, default=0)
    revoke.add_argument("--allocation", default="")
    revoke.add_argument("--reason", default="")
    revoke.add_argument("--requester", default="")
    revoke.set_defaults(handler=command_revoke)

    validate = commands.add_parser("validate")
    validate.add_argument("--namespace", required=True)
    validate.add_argument("--value", type=int, required=True)
    validate.add_argument("--symbol", default="")
    validate.add_argument("--project", default="")
    validate.set_defaults(handler=command_validate)

    version = commands.add_parser("version")
    version.set_defaults(handler=command_version)

    update = commands.add_parser("update")
    update.set_defaults(handler=command_update)

    return parser


def main():
    parser = build_parser()
    args = parser.parse_args()
    try:
        if args.command not in {"version", "update"}:
            check_skill_version(args.command)
        args.handler(args)
    except (OSError, ValueError, RuntimeError, json.JSONDecodeError, zipfile.BadZipFile) as error:
        print(f"error: {error}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
