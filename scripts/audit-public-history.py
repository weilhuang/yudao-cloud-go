#!/usr/bin/env python3
"""扫描待发布分支的可达提交、文件名和文本内容中的高风险公开信息。"""

from __future__ import annotations

import ipaddress
import re
import subprocess
import sys


def git(*args: str) -> bytes:
    return subprocess.check_output(("git", *args), stderr=subprocess.DEVNULL)


# 只放高置信度模式；它不能代替人工凭据盘点和专业秘密扫描器。
SECRET_PATTERNS = {
    "private-key": re.compile(rb"-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----"),
    "github-token": re.compile(rb"(?:gh[pousr]_[A-Za-z0-9_]{30,}|github_pat_[A-Za-z0-9_]{30,})"),
    "aws-key": re.compile(rb"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b"),
    "aliyun-key": re.compile(rb"\bLTAI[A-Za-z0-9]{12,}\b"),
    "jwt": re.compile(rb"\beyJ[A-Za-z0-9_-]{15,}\.eyJ[A-Za-z0-9_-]{15,}\.[A-Za-z0-9_-]{15,}\b"),
    "home-path": re.compile(rb"/(?:Users|home)/[^/\s]+/"),
    "credential-url": re.compile(rb"(?i)(?:mysql|redis|postgres|mongodb)://[^\s:@]+:[^\s@]+@"),
}
EMAIL = re.compile(rb"\b[A-Za-z0-9._%+-]+@(?:[A-Za-z0-9.-]+\.[A-Za-z]{2,})\b")
PHONE = re.compile(rb"(?<!\d)1[3-9]\d{9}(?!\d)")
IPV4 = re.compile(rb"\b(?:\d{1,3}\.){3}\d{1,3}\b")
BAD_NAME = re.compile(
    r"(?i)(^|/)(?:\.env(?:\..+)?|[^/]+\.(?:pem|key|p12|pfx|db|sqlite|log|bundle|zip|tar))(?:$|/)"
)

# 远端原始提交就是上游模板。下面四个 blob 已逐项复核：模板作者的公开联系邮箱、
# 测试用邮箱，以及由 fmt 占位符拼出的 MongoDB URI；均不是本项目的真实凭据。
# 同时锁定提交、路径、blob 和告警类型，后续任何内容变化都会重新触发审查。
INITIAL_TEMPLATE_COMMIT = "85f246cb237caebf6d6cb7618b3f22d56b022bf1"
INITIAL_TEMPLATE_EXCEPTIONS = {
    ("README.md", "a95d338475dc7155e2e47618c995890f480095b0", "non-example-email"),
    ("api/controller/profile_controller_test.go", "90d546d85b15e7fb9f71ec201ad01842423a4bf9", "non-example-email"),
    ("bootstrap/database.go", "cdff5fac86a102ed459c9454c665923c288fe300", "credential-url"),
    ("repository/user_repository_test.go", "9d958e8473c2205d054b9d55442ba0f3c56738af", "non-example-email"),
}

# 子模块只在本仓库保存一个提交指针；拒绝意外的外部 Git 对象。
ALLOWED_GITLINKS = {("ui/yudao-ui-admin-vben", "486870dd8496d94b79aaa183069df8407de8cbf9")}
ALLOWED_GITMODULES = (
    b'[submodule "vben"]\n'
    b"\tpath = ui/yudao-ui-admin-vben\n"
    b"\turl = https://github.com/yudaocode/yudao-ui-admin-vben.git\n"
)


def scan_text(label: str, data: bytes, issues: set[tuple[str, str]]) -> None:
    # 二进制只检查名称和体积；ip2region.xdb 等第三方数据另行核对许可。
    if b"\0" in data[:4096]:
        return
    for kind, pattern in SECRET_PATTERNS.items():
        if pattern.search(data):
            issues.add((kind, label))
    if label.endswith((".md", ".go", ".yaml", ".yml", ".sh", ".json", ".example")):
        for email in EMAIL.findall(data):
            if not email.lower().endswith(b"@example.com"):
                issues.add(("non-example-email", label))
                break
        if not label.endswith("_test.go") and PHONE.search(data):
            issues.add(("phone-like-literal", label))
        if not label.endswith("_test.go"):
            for raw in IPV4.findall(data):
                try:
                    if ipaddress.ip_address(raw.decode()).is_global:
                        issues.add(("public-ip-literal", label))
                        break
                except ValueError:
                    pass


def main() -> int:
    ref = sys.argv[1] if len(sys.argv) > 1 else "HEAD"
    try:
        commits = git("rev-list", ref).decode().splitlines()
    except subprocess.CalledProcessError:
        print(f"无法读取 Git ref: {ref}", file=sys.stderr)
        return 2
    issues: set[tuple[str, str]] = set()
    seen_blobs: set[str] = set()
    scanned_paths: set[tuple[str, str]] = set()
    for commit in commits:
        message = git("show", "-s", "--format=%B", commit)
        scan_text(f"commit:{commit[:12]}", message, issues)
        for entry in git("ls-tree", "-r", "-z", commit).split(b"\0"):
            if not entry:
                continue
            meta, path_bytes = entry.split(b"\t", 1)
            _mode, kind, sha = meta.decode().split()
            path = path_bytes.decode("utf-8", errors="replace")
            if kind == "commit":
                if (path, sha) not in ALLOWED_GITLINKS:
                    issues.add(("unexpected-gitlink", path))
                continue
            if kind != "blob":
                issues.add(("non-blob-entry", path))
                continue
            if BAD_NAME.search(path) and path != ".env.example":
                issues.add(("sensitive-filename", path))
            if path == "docs/node_modules" or path.startswith((".playwright-cli/", "docs/iterations/", "node_modules/", "website/node_modules/", "docs/.vitepress/dist/", "website/.vitepress/dist/")):
                issues.add(("internal-or-build-output", path))
            seen_blobs.add(sha)
            if (sha, path) in scanned_paths:
                continue
            scanned_paths.add((sha, path))
            size = int(git("cat-file", "-s", sha))
            if size > 8_000_000:
                issues.add(("large-blob", path))
                continue
            found: set[tuple[str, str]] = set()
            data = git("cat-file", "blob", sha)
            if path == ".gitmodules" and data != ALLOWED_GITMODULES:
                issues.add(("unexpected-submodule-config", path))
            scan_text(path, data, found)
            for kind, location in found:
                if commit == INITIAL_TEMPLATE_COMMIT and (path, sha, kind) in INITIAL_TEMPLATE_EXCEPTIONS:
                    continue
                issues.add((kind, location))
    print(f"扫描 {len(commits)} 条可达提交、{len(seen_blobs)} 个不同 blob")
    if issues:
        for kind, location in sorted(issues):
            # 不输出匹配值，避免把发现的秘密再写进 CI 日志。
            print(f"{kind}: {location}")
        return 1
    print("高置信度模式未检出；仍需人工检查真实凭据与第三方许可")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
