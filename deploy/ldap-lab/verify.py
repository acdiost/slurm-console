#!/usr/bin/env python3
"""End-to-end LDAP checks against the isolated lab and a disposable application DB."""
import base64
import hashlib
import http.client
import json
import os
from pathlib import Path
import pwd
import secrets
import shutil
import subprocess
import tempfile
import time

BINARY = '/opt/slurm-console/slurm-console-ldap-candidate'
BASE = 'dc=console-lab,dc=internal'
PEOPLE = 'ou=people,' + BASE
CONFIG = '/etc/slurm-console/ldap-lab.json'
ACCOUNT = 'ldap_qa'
DN = 'uid=' + ACCOUNT + ',' + PEOPLE
UNIT = 'slurm-console-ldap-qa'
data = None
created = []
environment = {**os.environ, 'SLURM_CONSOLE_LDAP_CONFIG': CONFIG}


def run(args, text=None, env=None, success=True):
    result = subprocess.run(args, input=text, text=True, capture_output=True, timeout=30, env=env)
    if success and result.returncode:
        raise RuntimeError('Failed: ' + ' '.join(args) + '\n' + result.stderr)
    return result


def cli(*args, secret=None):
    return run(['runuser', '-u', 'slurm-console', '--', BINARY, *args, '--dir=' + str(data)], secret, environment)


def directory(operation, ldif):
    if operation == 'ldapdelete':
        ldif += '\n'
    return run([operation, '-x', '-H', 'ldaps://127.0.0.1:3269', '-D', 'cn=admin,' + BASE, '-y', admin_file], ldif,
               {**os.environ, 'LDAPTLS_CACERT': '/etc/slurm-console/ldap-lab-ca.pem'})


def add_user(dn, username, password):
    salt = os.urandom(16)
    hashed = '{SSHA}' + base64.b64encode(hashlib.sha1(password.encode() + salt).digest() + salt).decode()
    directory('ldapadd', f'dn: {dn}\nobjectClass: inetOrgPerson\ncn: LDAP QA\nsn: QA\nuid: {username}\nuserPassword: {hashed}\n')
    created.append(dn)


def api(path, method='GET', body=None, token='', peer='127.0.0.1'):
    conn = http.client.HTTPConnection('127.0.0.1', 18091, timeout=15, source_address=(peer, 0))
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = token
    try:
        conn.request(method, path, json.dumps(body) if body is not None else None, headers)
        response = conn.getresponse()
        return response.status, json.loads(response.read())
    finally:
        conn.close()


def login(identity, password, expected, peer='127.0.0.1'):
    status, body = api('/api/collections/users/auth-with-password', 'POST', {'identity': identity, 'password': password}, peer=peer)
    assert status == expected, ('login', expected, status, body.get('message'))
    return body.get('token', '')


def start(config):
    run(['systemd-run', '--collect', '--unit=' + UNIT, '--property=User=slurm-console', '--property=Group=slurm-console', '--property=UMask=0077',
         '--setenv=SLURM_CONSOLE_LDAP_CONFIG=' + config, '--setenv=SLURM_CONSOLE_PAM_ENABLED=true', '--setenv=SLURM_CONSOLE_PUBLIC_DIR=/opt/slurm-console/public',
         BINARY, 'serve', '--http=127.0.0.1:18091', '--dir=' + str(data)])
    for _ in range(50):
        try:
            if api('/api/v1/health')[0] == 200:
                return
        except Exception:
            time.sleep(.1)
    raise RuntimeError('QA backend did not start')


with tempfile.TemporaryDirectory(prefix='slurm-console-ldap-qa-') as root:
    root = Path(root)
    admin_file = str(root / 'admin-secret')
    Path(admin_file).write_text(json.loads(Path('/root/slurm-console-ldap-lab-credentials.json').read_text())['admin'])
    os.chmod(admin_file, 0o600)
    app_user = pwd.getpwnam('slurm-console')
    try:
        password = secrets.token_urlsafe(24)
        platform_fixture = secrets.token_urlsafe(24)
        add_user(DN, ACCOUNT, password)
        unmapped = 'uid=ldap_unmapped,' + PEOPLE
        add_user(unmapped, 'ldap_unmapped', password)
        data = Path(tempfile.mkdtemp(prefix='ldap-qa-', dir='/var/lib/slurm-console'))
        os.chown(data, app_user.pw_uid, app_user.pw_gid)
        cli('users', 'check-ldap')
        cli('users', 'create', '--email', 'ldap-qa@example.com', '--name', 'LDAP QA', secret=platform_fixture + '\n')
        cli('access', 'register-cluster', '--slug', 'ldap-qa-alpha', '--name', 'LDAP QA Alpha')
        cli('access', 'register-cluster', '--slug', 'ldap-qa-beta', '--name', 'LDAP QA Beta')
        cli('access', 'grant', '--email', 'ldap-qa@example.com', '--role', 'viewer', '--cluster', 'ldap-qa-alpha')
        cli('users', 'link-ldap', '--email', 'ldap-qa@example.com', '--username', ACCOUNT)
        start(CONFIG)
        token = login(ACCOUNT, password, 200)
        login('ldap-qa@example.com', password, 200)
        login(ACCOUNT, 'wrong-directory-password', 400)
        login(ACCOUNT, platform_fixture, 400)
        login('ldap_unmapped', password, 400)
        login(ACCOUNT, password, 429)
        status, me = api('/api/v1/auth/me', token=token)
        assert status == 200 and me['data']['authSource'] == 'ldap' and me['data']['username'] == ACCOUNT
        assert me['data']['access']['clusters'][0]['roles'] == ['Viewer']
        status, clusters = api('/api/v1/clusters', token=token)
        assert status == 200 and clusters['data']['totalItems'] == 1 and clusters['data']['items'][0]['slug'] == 'ldap-qa-alpha'
        assert api('/api/collections/users/auth-refresh', 'POST', {}, token)[0] == 200
        print('PASS LDAPS username/email login, no password fallback, explicit mapping, rate limiting, token refresh and cluster RBAC', flush=True)
        directory('ldapmodify', f'dn: {DN}\nchangetype: modify\nadd: pwdAccountLockedTime\npwdAccountLockedTime: 000001010000Z\n')
        assert api('/api/v1/auth/me', token=token)[0] == 401
        assert api('/api/collections/users/auth-refresh', 'POST', {}, token)[0] == 401
        login(ACCOUNT, password, 400, '127.0.0.2')
        directory('ldapmodify', f'dn: {DN}\nchangetype: modify\ndelete: pwdAccountLockedTime\n')
        assert api('/api/v1/auth/me', token=token)[0] == 200
        directory('ldapmodify', f'dn: {DN}\nchangetype: modify\nadd: pwdReset\npwdReset: TRUE\n')
        assert api('/api/v1/auth/me', token=token)[0] == 401
        login(ACCOUNT, password, 400, '127.0.0.2')
        directory('ldapmodify', f'dn: {DN}\nchangetype: modify\ndelete: pwdReset\n')
        duplicate = 'cn=duplicate-ldap-qa,' + PEOPLE
        add_user(duplicate, ACCOUNT, password)
        assert api('/api/v1/auth/me', token=token)[0] == 401
        login(ACCOUNT, password, 400, '127.0.0.2')
        directory('ldapdelete', duplicate)
        created.remove(duplicate)
        assert api('/api/v1/auth/me', token=token)[0] == 200
        directory('ldapdelete', DN)
        created.remove(DN)
        add_user(DN, ACCOUNT, password)
        assert api('/api/v1/auth/me', token=token)[0] == 401
        assert api('/api/collections/users/auth-refresh', 'POST', {}, token)[0] == 401
        login(ACCOUNT, password, 400, '127.0.0.2')
        print('PASS directory lock, ambiguous uid, and deleted/recreated uid revoke existing-token access', flush=True)
        run(['systemctl', 'stop', UNIT])
        cli('users', 'link-ldap', '--email', 'ldap-qa@example.com', '--username', ACCOUNT)
        start(CONFIG)
        token = login(ACCOUNT, password, 200)
        run(['systemctl', 'stop', 'slurm-console-ldap-lab'])
        assert api('/api/v1/auth/me', token=token)[0] == 503
        assert api('/api/collections/users/auth-refresh', 'POST', {}, token)[0] == 503
        login(ACCOUNT, password, 503)
        run(['systemctl', 'start', 'slurm-console-ldap-lab'])
        for _ in range(50):
            if api('/api/v1/auth/me', token=token)[0] == 200:
                break
            time.sleep(.1)
        else:
            raise RuntimeError('LDAP did not recover after restart')
        print('PASS directory outage returns 503 and recovers without switching authentication sources', flush=True)
        run(['systemctl', 'stop', UNIT])
        starttls = dict(json.loads(Path(CONFIG).read_text()), url='ldap://127.0.0.1:7389')
        config_path = data / 'starttls.json'
        config_path.write_text(json.dumps(starttls))
        os.chown(config_path, app_user.pw_uid, app_user.pw_gid)
        os.chmod(config_path, 0o600)
        start(str(config_path))
        token = login(ACCOUNT, password, 200)
        assert api('/api/v1/auth/me', token=token)[0] == 200
        assert api('/api/collections/users/auth-refresh', 'POST', {}, token)[0] == 200
        untrusted = dict(starttls, url='ldaps://127.0.0.1:3269', caFile='')
        config_path = data / 'untrusted.json'
        config_path.write_text(json.dumps(untrusted))
        os.chown(config_path, app_user.pw_uid, app_user.pw_gid)
        os.chmod(config_path, 0o600)
        assert run(['runuser', '-u', 'slurm-console', '--', BINARY, 'users', 'check-ldap'], env={**environment, 'SLURM_CONSOLE_LDAP_CONFIG': str(config_path)}, success=False).returncode != 0
        print('PASS StartTLS login/refresh and untrusted CA rejection', flush=True)
        assert run(['getenforce']).stdout.strip() == 'Enforcing'
        assert run(['systemctl', 'is-active', 'slapd', 'slurmctld', 'slurmd', 'slurm-console']).stdout.split() == ['active'] * 4
        print('PASS existing directory, PAM platform service and Slurm remain active with SELinux enforcing', flush=True)
    finally:
        run(['systemctl', 'start', 'slurm-console-ldap-lab'], success=False)
        run(['systemctl', 'stop', UNIT], success=False)
        cleanup_errors = []
        for dn in reversed(created):
            try:
                directory('ldapdelete', dn)
            except Exception as error:
                cleanup_errors.append(str(error))
        if data:
            shutil.rmtree(data)
        if cleanup_errors:
            raise RuntimeError('LDAP test fixture cleanup failed: ' + '\n'.join(cleanup_errors))
