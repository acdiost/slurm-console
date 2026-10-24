#!/usr/bin/env python3
"""Create an isolated OpenLDAP lab on Rocky Linux; never changes the existing directory."""
import base64
import hashlib
import json
import os
from pathlib import Path
import pwd
import secrets
import socket
import subprocess

CONFIG = Path('/etc/openldap/slurm-console-lab')
CERTS = Path('/etc/openldap/certs/slurm-console-lab')
DATA = Path('/var/lib/ldap/slurm-console-lab')
APP = Path('/etc/slurm-console')
SECRET = Path('/root/slurm-console-ldap-lab-credentials.json')
BASE = 'dc=console-lab,dc=internal'
READER = 'cn=console-reader,ou=service,' + BASE


def run(args, data=None):
    result = subprocess.run(args, input=data, text=True, capture_output=True, timeout=60)
    if result.returncode:
        raise RuntimeError('Failed: ' + ' '.join(args) + '\n' + result.stderr)
    return result.stdout


def write(path, text, mode, group=0):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, mode)
    with os.fdopen(fd, 'w') as target:
        target.write(text)
    os.chown(path, 0, group)


def password_hash(value):
    salt = os.urandom(16)
    return '{SSHA}' + base64.b64encode(hashlib.sha1(value.encode() + salt).digest() + salt).decode()


if os.geteuid() != 0:
    raise SystemExit('Run as root on the test VM.')
for path in [CONFIG, CERTS, DATA, SECRET, APP / 'ldap-lab.json', APP / 'ldap-bind-password']:
    if path.exists():
        raise SystemExit('Refusing to overwrite existing lab file: ' + str(path))
for port in [7389, 3269]:
    with socket.socket() as check:
        check.bind(('127.0.0.1', port))
ldap_user = pwd.getpwnam('ldap')
app_user = pwd.getpwnam('slurm-console')
for directory in [CONFIG, CERTS]:
    directory.mkdir(mode=0o750)
    os.chown(directory, 0, ldap_user.pw_gid)
DATA.mkdir(mode=0o700)
os.chown(DATA, ldap_user.pw_uid, ldap_user.pw_gid)
APP.mkdir(mode=0o755, exist_ok=True)
admin, reader, demo = [secrets.token_urlsafe(24) for _ in range(3)]
write(SECRET, json.dumps({'admin': admin, 'reader': reader, 'username': 'ldapdemo', 'password': demo}), 0o600)
run(['openssl', 'req', '-x509', '-newkey', 'rsa:3072', '-nodes', '-days', '365', '-subj', '/CN=Slurm Console LDAP Lab CA', '-keyout', str(CONFIG / 'ca.key'), '-out', str(CONFIG / 'ca.pem')])
run(['openssl', 'req', '-newkey', 'rsa:3072', '-nodes', '-subj', '/CN=localhost', '-keyout', str(CERTS / 'server.key'), '-out', str(CONFIG / 'server.csr')])
write(CONFIG / 'server.ext', 'subjectAltName=DNS:localhost,IP:127.0.0.1\nbasicConstraints=CA:FALSE\nkeyUsage=digitalSignature,keyEncipherment\nextendedKeyUsage=serverAuth\n', 0o600)
run(['openssl', 'x509', '-req', '-in', str(CONFIG / 'server.csr'), '-CA', str(CONFIG / 'ca.pem'), '-CAkey', str(CONFIG / 'ca.key'), '-CAcreateserial', '-days', '365', '-extfile', str(CONFIG / 'server.ext'), '-out', str(CERTS / 'server.pem')])
os.chmod(CONFIG / 'ca.key', 0o600)
os.chown(CERTS / 'server.key', 0, ldap_user.pw_gid)
os.chmod(CERTS / 'server.key', 0o640)
write(APP / 'ldap-lab-ca.pem', (CONFIG / 'ca.pem').read_text(), 0o644)
write(APP / 'ldap-bind-password', reader, 0o640, app_user.pw_gid)
config = f'''include /etc/openldap/schema/core.schema
include /etc/openldap/schema/cosine.schema
include /etc/openldap/schema/inetorgperson.schema
pidfile /run/openldap/slurm-console-lab.pid
argsfile /run/openldap/slurm-console-lab.args
modulepath /usr/lib64/openldap
moduleload ppolicy.la
TLSCertificateFile {CERTS}/server.pem
TLSCertificateKeyFile {CERTS}/server.key
TLSCACertificateFile {CONFIG}/ca.pem
TLSProtocolMin 3.3
security simple_bind=128
database mdb
maxsize 1073741824
suffix "{BASE}"
rootdn "cn=admin,{BASE}"
rootpw {password_hash(admin)}
directory {DATA}
index objectClass eq
index uid eq
access to attrs=userPassword
  by anonymous auth
  by self auth
  by * none
access to *
  by dn.exact="{READER}" read
  by * none
overlay ppolicy
ppolicy_default "cn=default,ou=policies,{BASE}"
ppolicy_use_lockout
'''
write(CONFIG / 'slapd.conf', config, 0o640, ldap_user.pw_gid)
ldif = f'''dn: {BASE}
objectClass: top
objectClass: dcObject
objectClass: organization
dc: console-lab
o: Slurm Console isolated LDAP lab

dn: ou=people,{BASE}
objectClass: organizationalUnit
ou: people

dn: ou=service,{BASE}
objectClass: organizationalUnit
ou: service

dn: ou=policies,{BASE}
objectClass: organizationalUnit
ou: policies

dn: cn=default,ou=policies,{BASE}
objectClass: device
objectClass: pwdPolicy
cn: default
pwdAttribute: userPassword
pwdLockout: TRUE
pwdMaxFailure: 5
pwdLockoutDuration: 0
pwdCheckQuality: 0

dn: {READER}
objectClass: inetOrgPerson
cn: console-reader
sn: Reader
uid: console-reader
userPassword: {password_hash(reader)}

dn: uid=ldapdemo,ou=people,{BASE}
objectClass: inetOrgPerson
cn: LDAP Demo
sn: Demo
uid: ldapdemo
mail: ldapdemo@slurm-console.local
userPassword: {password_hash(demo)}
'''
run(['/usr/sbin/slaptest', '-f', str(CONFIG / 'slapd.conf'), '-u'])
run(['/usr/sbin/slapadd', '-f', str(CONFIG / 'slapd.conf'), '-l', '/dev/stdin'], ldif)
for file in DATA.iterdir():
    os.chown(file, ldap_user.pw_uid, ldap_user.pw_gid)
write(APP / 'ldap-lab.json', json.dumps({'url': 'ldaps://127.0.0.1:3269', 'baseDN': 'ou=people,' + BASE, 'bindDN': READER, 'bindPasswordFile': str(APP / 'ldap-bind-password'), 'caFile': str(APP / 'ldap-lab-ca.pem')}, indent=2) + '\n', 0o640, app_user.pw_gid)
unit = '''[Unit]
Description=Slurm Console isolated OpenLDAP test directory
After=network.target

[Service]
Type=simple
User=ldap
Group=ldap
ExecStart=/usr/sbin/slapd -f /etc/openldap/slurm-console-lab/slapd.conf -h "ldap://127.0.0.1:7389 ldaps://127.0.0.1:3269" -d 0
Restart=on-failure
UMask=0077

[Install]
WantedBy=multi-user.target
'''
write(Path('/etc/systemd/system/slurm-console-ldap-lab.service'), unit, 0o644)
run(['restorecon', '-RF', str(CONFIG), str(CERTS), str(DATA), str(APP), '/etc/systemd/system/slurm-console-ldap-lab.service'])
run(['systemctl', 'daemon-reload'])
run(['systemctl', 'enable', '--now', 'slurm-console-ldap-lab'])
print('Isolated LDAP lab installed on loopback ports 7389/3269. Secrets remain in a root-only file. Existing OpenLDAP configuration was not changed.')
