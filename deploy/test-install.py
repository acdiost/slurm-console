"""Exercise real systemd installation only on a disposable Linux CI runner."""
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import secrets
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


@contextmanager
def pam_system_identity():
    username = "sc-pam-" + secrets.token_hex(4)
    password = secrets.token_urlsafe(24)
    command("useradd", "--no-create-home", "--user-group", "--shell", "/bin/sh", username)
    try:
        command("chpasswd", input=f"{username}:{password}\n")
        yield username, password
    finally:
        command("userdel", username)


def verify_pam_configuration(token):
    status = json.loads(request("/api/v1/setup/status", token=token))["data"]["pam"]
    assert not status["configured"]
    helper = Path("/usr/local/libexec/slurm-console-pam-helper")
    policy = Path("/etc/pam.d/slurm-console")
    assert not helper.exists() and not policy.exists(), "PAM must start unconfigured"
    configured = json.loads(request("/api/v1/setup/pam/configure", {"revision": status["revision"]}, token))["data"]
    assert configured["configured"] and configured["available"]
    assert helper.stat().st_uid == 0 and helper.stat().st_mode & 0o777 == 0o755
    assert policy.stat().st_uid == 0 and policy.stat().st_mode & 0o022 == 0
    original_policy = policy.read_bytes()
    repeated = json.loads(request("/api/v1/setup/pam/configure", {"revision": configured["revision"]}, token))["data"]
    assert repeated["configured"] and repeated["available"]
    assert policy.read_bytes() == original_policy
    with pam_system_identity() as (username, password):
        payload = {"mode": "authenticate", "username": username, "password": password}
        assert json.loads(command(str(helper), input=json.dumps(payload)))["ok"]
        payload["password"] = "incorrect-system-password"
        assert json.loads(command(str(helper), input=json.dumps(payload)))["code"] == "denied"
        assert json.loads(command(str(helper), input=json.dumps({"mode": "account", "username": "root"})))["code"] == "denied"
        email = username + "@example.test"
        request("/api/v1/platform-users", {"email": email, "name": "PAM release smoke", "authSource": "pam", "username": username, "password": ""}, token)
        pam_login = json.loads(request("/api/collections/users/auth-with-password", {"identity": email, "password": password}))
        assert pam_login["record"]["auth_source"] == "pam"
        command("systemctl", "restart", "slurm-console")
        command("/opt/slurm-console/slurm-console", "healthcheck")
        assert json.loads(request("/api/collections/users/auth-with-password", {"identity": email, "password": password}))["token"]
        restored = json.loads(request("/api/v1/setup/status", token=token))["data"]["pam"]
        assert restored["configured"] and restored["available"]

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
    verify_pam_configuration(login["token"])
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
        assert db.execute("SELECT count(*) FROM users").fetchone()[0] == 2
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
    print("PASS: release PAM configuration and real login, installation, retention, reinstall, complete removal")


if __name__ == "__main__":
    main()
