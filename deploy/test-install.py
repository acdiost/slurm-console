"""Exercise real systemd installation only on a disposable Linux CI runner."""
import json
import os
from pathlib import Path
import re
import sqlite3
import subprocess
import sys
import urllib.request


def command(*args, **kwargs):
    result = subprocess.run(args, text=True, capture_output=True, **kwargs)
    if result.returncode:
        # Installer stdout may contain a generated credential; never print it.
        raise RuntimeError(f"{args[0]} failed: {result.stderr}")
    return result.stdout


def request(path, body=None, token=None):
    headers = {"Content-Type": "application/json"}
    if token:
        headers["Authorization"] = token
    payload = None if body is None else json.dumps(body).encode()
    req = urllib.request.Request("http://127.0.0.1:6829" + path, payload, headers)
    with urllib.request.urlopen(req, timeout=5) as response:
        return response.read()


def main():
    if sys.platform != "linux" or os.geteuid() != 0 or os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("Requires root on a disposable GitHub Actions Linux runner")
    binary = str(Path(sys.argv[1]).resolve())
    output = command(binary, "install", input="\n" * 5)
    match = re.search(r"首次管理员密码（仅显示本次，请保存）: ([^\n]+)", output)
    assert match, "first install did not generate a credential"
    password = match.group(1)
    assert command("systemctl", "show", "slurm-console", "-p", "User", "--value").strip() == "root"
    assert json.loads(request("/api/v1/health"))["data"]["status"] == "ok"
    # Prove the installed service uses embedded assets, including SPA deep links.
    public = Path("/opt/slurm-console/public")
    hidden = public.with_name("public-test-hidden")
    public.rename(hidden)
    try:
        assert b"<html" in request("/login").lower()
        assert b"<html" in request("/jobs").lower()
    finally:
        hidden.rename(public)
    output = command("/opt/slurm-console/slurm-console", "configure", input="\n" * 5)
    assert password not in output, "rerun disclosed or regenerated the password"
    login = json.loads(request("/api/collections/users/auth-with-password", {
        "identity": "admin@example.com", "password": password,
    }))
    me = json.loads(request("/api/v1/auth/me", token=login["token"]))["data"]
    assert "platform.users.manage" in me["access"]["platformPermissions"]
    with sqlite3.connect("/var/lib/slurm-console/pb_data/data.db") as db:
        assert db.execute("SELECT count(*) FROM users").fetchone()[0] == 1
        assert db.execute("SELECT count(*) FROM user_platform_roles").fetchone()[0] == 1
    assert list(Path("/var/backups/slurm-console").glob("upgrade-*.tar.gz.age"))
    assert command("systemctl", "is-active", "slurm-console-backup.timer").strip() == "active"
    data = Path("/var/lib/slurm-console/pb_data/data.db")
    config = Path("/etc/slurm-console/slurm-console.env")
    key = Path("/var/lib/slurm-console-backup/identity.txt")
    config_before, key_before = config.read_bytes(), key.read_bytes()
    snapshots = sorted(Path("/var/backups/slurm-console").glob("*.age"))
    # Cancellation, EOF and invalid integration config must not stop the service.
    uninstall_env = dict(os.environ, SLURM_CONSOLE_AI_CONFIG="/nonexistent/uninstall-config.json")
    command(binary, "uninstall", input="n\nn\nn\n\n", env=uninstall_env)
    cancelled = subprocess.run([binary, "uninstall"], input="n\nn\nn\n", text=True, capture_output=True, env=uninstall_env)
    assert cancelled.returncode != 0
    assert command("systemctl", "is-active", "slurm-console").strip() == "active"
    assert data.is_file() and config.read_bytes() == config_before and key.read_bytes() == key_before
    # Run the installed binary while deleting its own program directory.
    command("/opt/slurm-console/slurm-console", "uninstall", input="\n\n\nUNINSTALL\n", env=uninstall_env)
    assert not Path("/opt/slurm-console").exists()
    assert config.read_bytes() == config_before and key.read_bytes() == key_before
    assert all(path.is_file() for path in snapshots)
    with sqlite3.connect(data) as db:
        assert db.execute("SELECT count(*) FROM users").fetchone()[0] == 1
    for unit in ("slurm-console.service", "slurm-console-backup.service", "slurm-console-backup.timer"):
        assert not Path("/etc/systemd/system", unit).exists()
        assert subprocess.run(["systemctl", "is-active", "--quiet", unit]).returncode != 0
    # Reinstallation must reuse retained credentials; complete removal is repeatable.
    command(binary, "install", input="\n" * 5)
    assert json.loads(request("/api/collections/users/auth-with-password", {
        "identity": "admin@example.com", "password": password,
    }))["token"]
    command(binary, "uninstall", input="n\nn\nn\nUNINSTALL\n")
    command(binary, "uninstall", input="n\nn\nn\nUNINSTALL\n")
    for path in ("/opt/slurm-console", "/etc/slurm-console", "/var/lib/slurm-console",
                 "/var/backups/slurm-console", "/var/lib/slurm-console-backup"):
        assert not Path(path).exists(), f"uninstall left {path}"
    print("PASS: installation, retained credentials, cancellation, uninstall retention, reinstall, complete removal")


if __name__ == "__main__":
    main()
