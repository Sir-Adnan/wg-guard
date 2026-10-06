#!/usr/bin/env python3
"""Opt-in acceptance on the owner-authorized disposable Ubuntu VPS.

Run as root after the ordinary installer. The work directory must already exist,
be root-owned/private, and contain the private owner.password. Commands never emit
HTTP bodies, tokens, keys or configs. Clients and files use an owned phase20 prefix.
This is a real-host fixture, not a production daemon or an inferred support matrix.
"""
import argparse
import hashlib
import http.client
import http.server
import http.cookiejar
import ipaddress
import json
import os
from pathlib import Path
import re
import sqlite3
import ssl
import stat
import subprocess
import sys
import time
import threading
import urllib.error
import urllib.parse
import urllib.request


parser = argparse.ArgumentParser()
parser.add_argument("action", choices=["seed", "network", "snapshot", "compare", "clients-down", "measure", "tls", "enrich", "shaping", "load", "enforcement"])
parser.add_argument("--work", required=True)
parser.add_argument("--public-ip", required=True)
parser.add_argument("--seconds", type=int, default=60)
parser.add_argument("--subscription-host")
parser.add_argument("--count", type=int, default=100)
args = parser.parse_args()
work = Path(args.work).resolve(strict=True)
assert os.geteuid() == 0 and work.parent == Path("/root") and work.name.startswith("wgg-phase20-")
assert not Path(args.work).is_symlink() and work.stat().st_uid == 0 and stat.S_IMODE(work.stat().st_mode) == 0o700
os.umask(0o077)
base = (work / "api-base").read_text().strip() if (work / "api-base").exists() else "http://127.0.0.1:8080"
stage = args.action


def save(name, value):
    path = work / name
    assert path.parent == work and not path.is_symlink()
    path.write_text(json.dumps(value, sort_keys=True, indent=2) + "\n")
    path.chmod(0o600)


def run(*argv, timeout=60, input_bytes=None, check=True):
    result = subprocess.run(argv, input=input_bytes, stdout=subprocess.PIPE,
                            stderr=subprocess.PIPE, timeout=timeout)
    if check and result.returncode:
        raise RuntimeError("command failed at " + stage + " (exit " + str(result.returncode) + ")")
    return result


def report(name, **values):
    result = {"gate": name, "passed": True, **values}
    save("report-" + name + ".json", result)
    print(json.dumps(result), flush=True)


def api(method, route, body=None, raw=False):
    token = (work / "api.token").read_text().strip()
    request = urllib.request.Request(base + route, method=method,
        headers={"Authorization": "Bearer " + token, "Content-Type": "application/json"},
        data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            data = response.read(4_194_305)
            assert len(data) <= 4_194_304
            if raw:
                assert "no-store" in response.headers.get("Cache-Control", "")
                return data
            return json.loads(data)
    except urllib.error.HTTPError as error:
        try:
            code = json.loads(error.read(65536)).get("error", {}).get("code", "unknown")
        except (ValueError, AttributeError):
            code = "unknown"
        code = code if re.fullmatch(r"[A-Z_]{1,64}", str(code)) else "unknown"
        raise RuntimeError("API failed at " + stage + " HTTP " + str(error.code) + " code " + str(code)) from None


def login():
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    password = (work / "owner.password").read_text().strip()
    form = urllib.parse.urlencode({"username": "phase20-admin", "password": password}).encode()
    with opener.open(urllib.request.Request(base + "/login", data=form), timeout=30) as response:
        assert response.status == 200 and not response.url.endswith("/login"), "owner login failed"
        response.read(1_000_000)
    return opener


def mint_token():
    scopes = "interfaces.read,interfaces.write,users.read,users.create,users.update,users.delete,devices.read,devices.write,configs.read,templates.read,templates.write,subscriptions.read,subscriptions.rotate,traffic.read,traffic.update,stats.read,node.read"
    minted = run("wg-guard", "token", "create", "-name", "phase20-acceptance", "-expires-in", "24h", "-scopes", scopes)
    assert re.fullmatch(rb"wg_[A-Za-z0-9_-]+\s*", minted.stdout)
    (work / "api.token").write_bytes(minted.stdout)
    (work / "api.token").chmod(0o600)


def admin_post(opener, route, fields, source="/dashboard"):
    with opener.open(base + source, timeout=30) as response:
        html = response.read(2_000_000).decode()
    csrf = re.search(r'<meta name="csrf-token" content="([^"]+)"', html)
    assert csrf, "owner CSRF absent"
    body = urllib.parse.urlencode({**fields, "_csrf": csrf.group(1)}, doseq=True).encode()
    with opener.open(urllib.request.Request(base + route, data=body), timeout=30) as response:
        assert response.status == 200
        return response.read(2_000_000).decode()


def seed():
    global stage
    assert not (work / "fixture.json").exists(), "fixture already created"
    mint_token()
    opener = login()
    run("docker", "cp", "wg-guard:/usr/local/bin/awg", str(work / "awg"))
    run("docker", "cp", "wg-guard:/usr/local/bin/amneziawg-go", str(work / "amneziawg-go"))
    (work / "awg").chmod(0o700)
    (work / "amneziawg-go").chmod(0o700)
    fixture = {"profiles": [], "created_at": time.time()}
    for index, (preset, mode) in enumerate([("recommended", "kernel"), ("randomized", "kernel"), ("recommended", "userspace")]):
        stage = "seed-profile-" + str(index)
        number = (6, 7, 5)[index]  # stay below the default eight-interface index cap
        profile_input = {"name": "awg" + str(number),
            "listen_port": 42060 + index, "ipv4_pools": [f"10.220.{number}.0/24", f"10.221.{number}.0/24"],
            "mtu": 1380, "preset": preset, "backend_mode": mode}
        profile = next((item for item in api("GET", "/api/v1/interfaces")["items"] if item["name"] == profile_input["name"]), None)
        if profile is None:
            profile = api("POST", "/api/v1/interfaces", profile_input)
        assert profile["listen_port"] == profile_input["listen_port"] and profile["backend_mode"] == mode
        user_input = {"username": "phase20-" + str(index),
            "interface_id": profile["id"], "duration_seconds": 86400, "start_policy": "immediate",
            "device_limit": 2, "traffic_limit_bytes": 2_000_000_000, "enabled": True,
            "tags": ["phase20", preset], "note": "disposable acceptance fixture"}
        user = next((item for item in api("GET", "/api/v1/users?limit=20")["items"] if item["username"] == user_input["username"]), None)
        if user is None:
            user = api("POST", "/api/v1/users", user_input)
        assert user["note"] == user_input["note"] and user["interface_id"] == profile["id"]
        devices = api("GET", "/api/v1/users/" + user["id"] + "/devices")["items"]
        assert len(devices) <= 1
        device = devices[0] if devices else api("POST", "/api/v1/users/" + user["id"] + "/devices", {
            "name": "phase20-client", "interface_id": profile["id"], "preshared_key": True})
        config = api("GET", "/api/v1/devices/" + device["id"] + "/config", raw=True)
        qr = api("GET", "/api/v1/devices/" + device["id"] + "/qr", raw=True)
        (work / f"client-{index}.conf").write_bytes(config)
        (work / f"client-{index}.png").write_bytes(qr)
        run(str(work / "qrcheck"), str(work / f"client-{index}.png"), str(work / f"client-{index}.conf"))
        # Direct create-user/device does not provision a customer link. Use the
        # explicit owner action before exercising the read-only integration lookup.
        admin_post(opener, "/users/" + user["id"] + "/sub/create", {})
        link = api("GET", "/api/v1/users/" + user["id"] + "/subscription")
        fixture["profiles"].append({"index": index, "profile": profile, "user": user,
            "device": device, "link": link, "config_sha256": hashlib.sha256(config).hexdigest(),
            "namespace": "p20-client-" + str(index), "host_veth": "p20h" + str(index)})
    stage = "seed-template"
    template = api("POST", "/api/v1/templates", {"name": "phase20-template",
        "traffic_limit_bytes": 300_000_000, "duration_seconds": 86400,
        "device_limit": 2, "interface_id": fixture["profiles"][0]["profile"]["id"], "enabled": True})
    templated = api("POST", "/api/v1/users", {"username": "phase20-templated", "template_id": template["id"], "enabled": True})
    assert templated["traffic_limit_bytes"] == template["traffic_limit_bytes"]
    fixture["template"] = template
    fixture["templated_user"] = templated
    save("fixture.json", fixture)
    opener = login()
    for item in fixture["profiles"]:
        path = "/devices/" + item["device"]["id"] + "/config"
        with opener.open(base + path, timeout=30) as response:
            assert hashlib.sha256(response.read(4097)).hexdigest() == item["config_sha256"], "admin config parity failed"
    report("seed", profiles=3, kernel_profiles=2, userspace_profiles=1,
           users=4, qr_decoded=3, admin_config_parity=3, owner_login=True, template_terms=True)


def network():
    global stage
    fixture = json.loads((work / "fixture.json").read_text())
    existing_ns = run("ip", "netns", "list").stdout.decode().splitlines()
    names = {line.split()[0] for line in existing_ns}
    for item in fixture["profiles"]:
        index, namespace, veth = item["index"], item["namespace"], item["host_veth"]
        stage = "network-" + str(index)
        transport = "172.31." + str(220 + index)
        config = (work / f"client-{index}.conf").read_text()
        settings = {line.split("=", 1)[0].strip(): line.split("=", 1)[1].strip()
                    for line in config.splitlines() if "=" in line}
        stripped = "\n".join(line for line in config.splitlines()
                               if not re.match(r"^\s*(Address|DNS|MTU)\s*=", line)) + "\n"
        (work / f"client-{index}.stripped").write_text(stripped)
        if namespace in names:
            public = run("ip", "netns", "exec", namespace, str(work / "awg"), "show", "p20awg", "public-key").stdout.decode().strip()
            assert public == item["device"]["public_key"], "namespace ownership mismatch"
        else:
            assert run("ip", "link", "show", veth, check=False).returncode != 0, "veth collision"
            run("ip", "netns", "add", namespace)
            nsdir = Path("/etc/netns") / namespace
            if nsdir.exists():
                assert nsdir.is_dir() and not nsdir.is_symlink() and nsdir.stat().st_uid == 0
                assert set(path.name for path in nsdir.iterdir()) == {"resolv.conf"}
                assert (nsdir / "resolv.conf").read_text() == "nameserver 1.1.1.1\nnameserver 1.0.0.1\n"
            else:
                nsdir.mkdir(mode=0o700, parents=True)
            (nsdir / "resolv.conf").write_text("nameserver 1.1.1.1\nnameserver 1.0.0.1\n")
            run("ip", "link", "add", veth, "type", "veth", "peer", "name", "eth0", "netns", namespace)
            run("ip", "address", "add", transport + ".1/30", "dev", veth)
            run("ip", "link", "set", veth, "up")
            run("ip", "-n", namespace, "link", "set", "lo", "up")
            run("ip", "-n", namespace, "address", "add", transport + ".2/30", "dev", "eth0")
            run("ip", "-n", namespace, "link", "set", "eth0", "up")
            run("ip", "-n", namespace, "route", "add", args.public_ip + "/32", "via", transport + ".1", "dev", "eth0")
            run("ip", "-n", namespace, "link", "add", "p20awg", "type", "amneziawg")
            run("ip", "netns", "exec", namespace, str(work / "awg"), "setconf", "p20awg", str(work / f"client-{index}.stripped"))
            run("ip", "-n", namespace, "address", "add", settings["Address"], "dev", "p20awg")
            run("ip", "-n", namespace, "link", "set", "p20awg", "mtu", settings["MTU"], "up")
            run("ip", "-n", namespace, "route", "replace", "default", "dev", "p20awg")
        run("ip", "-n", namespace, "link", "set", "p20awg", "up")
        run("ip", "-n", namespace, "route", "replace", "default", "dev", "p20awg")
        gateway = str(ipaddress.ip_network(item["profile"]["ipv4_subnet"]).network_address + 1)
        alive = False
        for attempt in range(25):
            if run("ip", "netns", "exec", namespace, "ping", "-c", "1", "-W", "1", gateway, check=False).returncode == 0:
                alive = True
                break
            time.sleep(1)
        assert alive, "tunnel handshake/gateway failed"
        stage = "public-ping-" + str(index)
        for attempt in range(3):
            if run("ip", "netns", "exec", namespace, "ping", "-c", "2", "-W", "3", "1.1.1.1", check=False).returncode == 0:
                break
        else:
            raise AssertionError("public ping failed")
        stage = "public-dns-" + str(index)
        run("ip", "netns", "exec", namespace, "getent", "ahostsv4", "example.com")
        stage = "public-https-" + str(index)
        run("ip", "netns", "exec", namespace, "curl", "-4", "-fsS", "--max-time", "20", "https://example.com/")
        stage = "public-nat-" + str(index)
        observed = run("ip", "netns", "exec", namespace, "curl", "-4", "-fsS", "--max-time", "20", "https://api.ipify.org").stdout.decode().strip()
        assert observed == args.public_ip, "public NAT identity failed"
        stage = "reverse-ping-" + str(index)
        for attempt in range(3):
            if run("ping", "-c", "4", "-W", "3", item["device"]["ipv4_address"].split("/")[0], check=False).returncode == 0:
                break
        else:
            raise AssertionError("reverse client ping failed")
        # Userspace UAPI sockets belong to the owning container's private /run.
        transfer = run("docker", "exec", "wg-guard", "/usr/local/bin/awg", "show", item["profile"]["name"], "transfer").stdout.decode().splitlines()
        assert any(len(line.split()) == 3 and int(line.split()[1]) > 0 and int(line.split()[2]) > 0 for line in transfer), "bidirectional counters absent"
        report("traffic-" + str(index), preset=item["profile"]["preset"], backend=item["profile"]["backend_mode"],
               gateway=True, bidirectional=True, public_dns=True, public_https=True, nat=True)
    policy = run("iptables", "-S", "FORWARD").stdout.decode()
    rules = run("iptables", "-S", "DOCKER-USER").stdout.decode()
    assert "-P FORWARD DROP" in policy and rules.count("wgguard:managed:docker-forward") == 1
    run("wg-guard", "doctor")
    report("forwarding", docker_forward_drop=True, scoped_jump_count=1, doctor=True)


def inventory(db_path="/var/lib/wg-guard/wg-guard.db"):
    db = sqlite3.connect("file:" + str(db_path) + "?mode=ro", uri=True)
    db.execute("BEGIN")
    tables = [row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")]
    summaries = {}
    for table in tables:
        if table in {"audit_log", "admin_sessions", "idempotency_keys", "traffic_samples", "traffic_rollups", "webhook_events", "webhook_deliveries"}:
            continue
        columns = [row[1] for row in db.execute('PRAGMA table_info("' + table + '")')]
        columns = [name for name in columns if name not in {"last_used_at", "last_login_at"}]
        # Accounting refreshes observation timestamps/baselines after startup.
        # Keep cumulative charged usage, eligibility, key bytes and all identities.
        if table in {"users", "devices"}:
            columns = [name for name in columns if name != "updated_at"]
        if table == "devices":
            columns = [name for name in columns if name not in {"last_rx", "last_tx", "last_endpoint", "last_handshake_at"}]
        # Snapshot digests stay in the private work directory; raw rows never leave memory.
        select = ','.join('"' + name.replace('"', '""') + '"' for name in columns)
        rows = list(db.execute('SELECT ' + select + ' FROM "' + table.replace('"', '""') + '"'))
        canonical = sorted(json.dumps(list(row), default=lambda value: {"bytes_sha256": hashlib.sha256(value).hexdigest()}, sort_keys=True) for row in rows)
        summaries[table] = {"rows": len(rows), "sha256": hashlib.sha256("\n".join(canonical).encode()).hexdigest()}
    db.close()
    return summaries


def snapshot():
    fixture = json.loads((work / "fixture.json").read_text())
    summaries = inventory()
    save("snapshot.json", {"tables": summaries,
         "key_sha256": hashlib.sha256(Path("/var/lib/wg-guard/master.key").read_bytes()).hexdigest(),
         "config_sha256": {item["device"]["id"]: hashlib.sha256(api("GET", "/api/v1/devices/" + item["device"]["id"] + "/config", raw=True)).hexdigest() for item in fixture["profiles"]}})
    report("snapshot", tables=len(summaries), config_count=3)


def compare():
    saved = json.loads((work / "snapshot.json").read_text())
    assert saved["key_sha256"] == hashlib.sha256(Path("/var/lib/wg-guard/master.key").read_bytes()).hexdigest(), "master key changed"
    now = inventory()
    archive_stage = work / "archive-stage.path"
    expected = inventory(archive_stage.read_text().strip()) if archive_stage.exists() else saved["tables"]
    changed = [name for name, value in expected.items() if now.get(name) != value]
    assert not changed, "restored table digests changed: " + ",".join(changed)
    for device, digest in saved["config_sha256"].items():
        assert hashlib.sha256(api("GET", "/api/v1/devices/" + device + "/config", raw=True)).hexdigest() == digest, "config bytes changed"
    login()
    if (work / "enriched.json").exists():
        enriched = json.loads((work / "enriched.json").read_text())
        assert reseller_api("GET", "/api/v1/users/" + enriched["customer_id"])["id"] == enriched["customer_id"]
        login_as("phase20-reseller", work / "reseller.password")
    report("compare", key_preserved=True, config_bytes_preserved=3, api_credential_preserved=True,
           owner_login=True, stable_tables_preserved=len(saved["tables"]), reseller_credentials_preserved=(work / "enriched.json").exists())


def login_as(username, password_file):
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    form = urllib.parse.urlencode({"username": username, "password": password_file.read_text().strip()}).encode()
    with opener.open(urllib.request.Request(base + "/login", data=form), timeout=30) as response:
        assert response.status == 200 and not response.url.endswith("/login")
        response.read(1_000_000)
    return opener


def reseller_api(method, route, body=None, denied=False):
    request = urllib.request.Request(base + route, method=method,
        headers={"Authorization": "Bearer " + (work / "reseller.token").read_text().strip(),
                 "Content-Type": "application/json", "Idempotency-Key": "phase20-reseller-purchase"},
        data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            assert not denied, "reseller reached foreign data"
            return json.loads(response.read(1_000_000))
    except urllib.error.HTTPError as error:
        if denied:
            assert error.code in {403, 404}
            return None
        raise RuntimeError("reseller request failed HTTP " + str(error.code)) from None


def enrich():
    assert not (work / "enriched.json").exists()
    fixture = json.loads((work / "fixture.json").read_text())
    owner = login()
    grants = ["users.read", "purchases.create", "operations.read", "subscriptions.read", "configs.read", "api_tokens.manage"]
    admin_post(owner, "/resellers", {"slug": "phase20-reseller", "display_name": "Phase20 acceptance", "permissions": grants})
    db = sqlite3.connect("file:/var/lib/wg-guard/wg-guard.db?mode=ro", uri=True)
    reseller = db.execute("SELECT id FROM resellers WHERE slug=?", ("phase20-reseller",)).fetchone()
    assert reseller
    reseller = reseller[0]
    admin_post(owner, "/resellers/" + reseller + "/templates", {"templates": [fixture["template"]["id"]]}, source="/resellers")
    (work / "reseller.password").write_text(os.urandom(24).hex() + "\n")
    admin_post(owner, "/resellers/" + reseller + "/admins", {"username": "phase20-reseller",
        "password": (work / "reseller.password").read_text().strip(), "permissions": grants}, source="/resellers")
    html = admin_post(owner, "/resellers/" + reseller + "/tokens", {"name": "phase20-reseller",
        "expires_days": "1", "scopes": ["users.read", "purchases.create", "operations.read", "subscriptions.read", "configs.read"]},
        source="/resellers/" + reseller + "/tokens")
    tokens = set(re.findall(r"wg_[A-Za-z0-9_-]{43}", html))
    assert len(tokens) == 1
    (work / "reseller.token").write_text(tokens.pop() + "\n")
    purchase = reseller_api("POST", "/api/v1/purchases", {"username": "phase20-reseller-customer",
        "template_id": fixture["template"]["id"], "device_name": "tenant-client", "device_count": 2})
    assert purchase["user_id"]
    customer = purchase["user_id"]
    assert reseller_api("GET", "/api/v1/users/" + customer)["id"] == customer
    reseller_api("GET", "/api/v1/users/" + fixture["profiles"][0]["user"]["id"], denied=True)
    login_as("phase20-reseller", work / "reseller.password")
    save("enriched.json", {"reseller_id": reseller, "customer_id": customer})
    report("reseller", reseller=True, reseller_login=True, owned_purchase=True, foreign_owner_read_denied=True,
           template_assignment=True, devices=2)


def tls():
    assert args.subscription_host
    fixture = json.loads((work / "fixture.json").read_text())
    host = args.subscription_host
    context = ssl.create_default_context()
    checked = denied = 0
    for item in fixture["profiles"]:
        route = item["link"]["path"] + "/devices/" + item["device"]["id"] + "/config"
        with urllib.request.urlopen("https://" + host + route, context=context, timeout=20) as response:
            assert response.status == 200 and "no-store" in response.headers.get("Cache-Control", "")
            assert hashlib.sha256(response.read(4097)).hexdigest() == item["config_sha256"]
        checked += 1
    for route in ["/login", "/dashboard", "/api/v1/users", "/readyz"]:
        try:
            with urllib.request.urlopen("https://" + host + route, context=context, timeout=20) as response:
                response.read(128)
            raise AssertionError("public origin exposed private route")
        except urllib.error.HTTPError as error:
            assert error.code in {403, 404, 421}
            denied += 1
    connection = http.client.HTTPSConnection(host, context=context, timeout=15)
    connection.request("GET", "/login", headers={"Host": urllib.parse.urlparse(base).netloc})
    response = connection.getresponse()
    assert response.status in {403, 404, 421}
    response.read(128)
    connection.close()
    report("tls", public_config_parity=checked, private_routes_denied=denied, sni_host_mismatch_denied=True,
           system_trust=True, separate_origins=True)


def shaping():
    global stage
    fixture = json.loads((work / "fixture.json").read_text())
    item = fixture["profiles"][0]
    payload = os.urandom(524288)
    (work / "load.payload").write_bytes(payload)
    gateway = str(ipaddress.ip_network(item["profile"]["ipv4_subnet"]).network_address + 1)
    class Handler(http.server.BaseHTTPRequestHandler):
        def log_message(self, *unused):
            pass
        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        def do_POST(self):
            length = int(self.headers["Content-Length"])
            assert length == len(payload)
            assert self.rfile.read(length) == payload
            self.send_response(200)
            self.end_headers()
    server = http.server.HTTPServer((gateway, 39320), Handler)
    worker = threading.Thread(target=server.serve_forever, daemon=True)
    worker.start()
    run("ip", "-n", item["namespace"], "link", "set", "p20awg", "up")
    run("ip", "-n", item["namespace"], "route", "replace", "default", "dev", "p20awg")
    api("PATCH", "/api/v1/users/" + item["user"]["id"], {"speed_limit_down_kbps": 1000, "speed_limit_up_kbps": 1000})
    time.sleep(17)  # admission is persisted immediately; scheduler applies shaping
    times = {}
    try:
        for direction in ["download", "upload"]:
            stage = "shaping-" + direction
            command = ["ip", "netns", "exec", item["namespace"], "curl", "-fsS", "--max-time", "30", "-o", "/dev/null"]
            if direction == "upload":
                command += ["--data-binary", "@" + str(work / "load.payload")]
            command += ["http://" + gateway + ":39320/payload"]
            started = time.monotonic()
            result = run(*command, timeout=35, check=False)
            assert result.returncode == 0, "local payload curl exit " + str(result.returncode)
            elapsed = time.monotonic() - started
            assert 2.5 <= elapsed <= 25, "shaping duration outside expected range: " + str(round(elapsed, 3))
            times[direction + "_seconds"] = round(elapsed, 3)
    finally:
        api("PATCH", "/api/v1/users/" + item["user"]["id"], {"speed_limit_down_kbps": None, "speed_limit_up_kbps": None})
        server.shutdown()
        server.server_close()
    report("shaping", rate_kbps=1000, bytes_each_direction=len(payload), **times)


def load():
    global stage
    assert args.count in {100, 1000}
    fixture = json.loads((work / "fixture.json").read_text())
    profile = fixture["profiles"][0]["profile"]
    stage = "load-pools"
    api("PATCH", "/api/v1/interfaces/" + profile["id"], {
        "ipv4_pools": [profile["ipv4_subnet"], "10.221.6.0/24", "10.222.0.0/22"]})
    stage = "load-token"
    minted = run("wg-guard", "token", "create", "-name", "phase20-load", "-expires-in", "24h",
                 "-scopes", "users.read,purchases.create,interfaces.read")
    token = minted.stdout.decode().strip()
    assert re.fullmatch(r"wg_[A-Za-z0-9_-]{43}", token)
    started = time.monotonic()
    for index in range(1, args.count // 100 + 1):
        stage = "load-purchase-" + str(index)
        body = {"username": "phase20-load-" + str(index), "device_count": 100, "device_name": "idle-device",
            "entitlement": {"traffic_limit_bytes": 2_000_000_000, "duration_seconds": 86400,
                            "device_limit": 100, "interface_id": profile["id"], "start_policy": "immediate"}}
        request = urllib.request.Request(base + "/api/v1/purchases", data=json.dumps(body).encode(),
            headers={"Authorization": "Bearer " + token, "Content-Type": "application/json",
                     "Idempotency-Key": "phase20-load-" + str(index)})
        try:
            with urllib.request.urlopen(request, timeout=90) as response:
                result = json.loads(response.read(1_000_000))
                assert len(result["device_ids"]) == 100
        except urllib.error.HTTPError as error:
            code = json.loads(error.read(65536)).get("error", {}).get("code", "unknown")
            assert re.fullmatch(r"[A-Z_]{1,64}", str(code))
            raise RuntimeError("load purchase HTTP " + str(error.code) + " " + code) from None
    report("load-provision-" + str(args.count), synthetic_devices=args.count,
           synthetic_accounts=args.count // 100, provision_seconds=round(time.monotonic() - started, 3))
    measure()


def enforcement():
    fixture = json.loads((work / "fixture.json").read_text())
    item = fixture["profiles"][0]
    owner = login()
    expiry = api("POST", "/api/v1/users", {"username": "phase20-expiry", "interface_id": item["profile"]["id"],
        "duration_seconds": 20, "start_policy": "immediate", "device_limit": 1, "traffic_limit_bytes": 2_000_000_000, "enabled": True})
    expiry_device = api("POST", "/api/v1/users/" + expiry["id"] + "/devices", {"name": "expiry-client", "interface_id": item["profile"]["id"]})
    original = api("GET", "/api/v1/users/" + item["user"]["id"])
    used = original["traffic_used_rx"] + original["traffic_used_tx"]
    api("PATCH", "/api/v1/users/" + original["id"], {"traffic_limit_bytes": used + 1024})
    pid = int(run("docker", "inspect", "--format", "{{.State.Pid}}", "wg-guard").stdout)
    def rss():
        text = Path(f"/proc/{pid}/status").read_text()
        return int(re.search(r"VmRSS:\s+(\d+)", text).group(1)) * 1024
    baseline = peak = rss()
    backup_result = []
    def encrypted_backup():
        try:
            admin_post(owner, "/backups/create", {}, source="/backups")
            backup_result.append(True)
        except Exception:
            backup_result.append(False)
    worker = threading.Thread(target=encrypted_backup, daemon=True)
    started = time.monotonic()
    worker.start()
    run("ip", "-n", item["namespace"], "link", "set", "p20awg", "up")
    run("ip", "-n", item["namespace"], "route", "replace", "default", "dev", "p20awg")
    run("ip", "netns", "exec", item["namespace"], "curl", "-4", "-fsS", "--max-time", "10", "https://example.com/", check=False)
    quota_time = expiry_time = None
    write_latency = []
    for attempt in range(600):
        peak = max(peak, rss())
        elapsed = time.monotonic() - started
        if attempt % 5 == 0:
            quota_state = api("GET", "/api/v1/users/" + original["id"])
            expiry_state = api("GET", "/api/v1/users/" + expiry["id"])
            if quota_time is None and quota_state["status"] == "traffic_exceeded":
                quota_time = elapsed
            if expiry_time is None and expiry_state["status"] == "expired":
                expiry_time = elapsed
            if len(write_latency) < 8:
                tick = time.monotonic()
                api("PATCH", "/api/v1/users/" + fixture["templated_user"]["id"], {"note": "phase20-contention-" + str(attempt)})
                write_latency.append(time.monotonic() - tick)
        if quota_time is not None and expiry_time is not None and not worker.is_alive():
            break
        time.sleep(0.1)
    worker.join(timeout=30)
    assert quota_time is not None and quota_time <= 30, "quota lag exceeds two cadences"
    assert expiry_time is not None and expiry_time <= 50, "expiry lag exceeds two cadences"
    assert backup_result == [True], "encrypted panel backup failed under enforcement"
    peers = run("docker", "exec", "wg-guard", "/usr/local/bin/awg", "show", item["profile"]["name"], "peers").stdout.decode()
    assert item["device"]["public_key"] not in peers and expiry_device["public_key"] not in peers, "ineligible peer retained"
    api("PATCH", "/api/v1/users/" + original["id"], {"traffic_limit_bytes": original["traffic_limit_bytes"]})
    api("POST", "/api/v1/users/" + original["id"] + "/enable", {})
    report("enforcement", quota_lag_seconds=round(quota_time, 3), expiry_since_creation_seconds=round(expiry_time, 3),
        encrypted_backup=True, process_rss_baseline_bytes=baseline, process_rss_peak_bytes=peak,
        writer_latency_peak_seconds=round(max(write_latency), 4), removed_ineligible_peers=2)


def clients_down():
    fixture = json.loads((work / "fixture.json").read_text())
    for item in fixture["profiles"]:
        run("ip", "-n", item["namespace"], "link", "set", "p20awg", "down")
    report("clients-down", clients=3)


def measure():
    assert 10 <= args.seconds <= 600
    pid = int(run("docker", "inspect", "--format", "{{.State.Pid}}", "wg-guard").stdout)
    samples = []
    start = time.monotonic()
    ticks = os.sysconf("SC_CLK_TCK")
    first_ticks = last_ticks = 0
    for index in range(args.seconds):
        fields = Path(f"/proc/{pid}/stat").read_text().split()
        used = int(fields[13]) + int(fields[14])
        if index == 0:
            first_ticks = used
        last_ticks = used
        status = Path(f"/proc/{pid}/status").read_text()
        rss_kib = int(re.search(r"VmRSS:\s+(\d+)", status).group(1))
        threads = int(re.search(r"Threads:\s+(\d+)", status).group(1))
        samples.append({"rss_bytes": rss_kib * 1024, "threads": threads})
        time.sleep(1)
    elapsed = time.monotonic() - start
    save("measure-samples.json", samples)
    report("measure", duration_seconds=round(elapsed, 3),
        rss_average_bytes=round(sum(sample["rss_bytes"] for sample in samples) / len(samples)),
        rss_peak_bytes=max(sample["rss_bytes"] for sample in samples),
        cpu_one_core_percent=round((last_ticks - first_ticks) / ticks / elapsed * 100, 3),
        threads_peak=max(sample["threads"] for sample in samples))


try:
    {"seed": seed, "network": network, "snapshot": snapshot, "compare": compare,
     "clients-down": clients_down, "measure": measure, "tls": tls, "enrich": enrich,
     "shaping": shaping, "load": load, "enforcement": enforcement}[args.action]()
except Exception as error:
    # Error arguments may contain untrusted/secret values. Emit only class and fixed stage.
    print(json.dumps({"gate": stage, "passed": False, "error_type": type(error).__name__,
                      "reason": str(error) if isinstance(error, (RuntimeError, AssertionError)) else ""}), flush=True)
    raise SystemExit(1)
