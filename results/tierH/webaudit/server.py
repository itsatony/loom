#!/usr/bin/env python3
"""Local single-user server for the tier-H human audit (frames v1).

stdlib only. Binds 127.0.0.1. One shared password, printed at startup
(or from AUDIT_PASSWORD env), constant-time compare, random session
token in an in-memory set, HttpOnly+SameSite=Lax cookie. Answers persist
to answers.json after every change so progress survives a restart. The
answer key is never sent to the browser except via /api/finish, which
the user triggers explicitly.
"""
import hmac
import http.server
import json
import os
import secrets
import socket
import socketserver
import sys
import time
import webbrowser

HERE = os.path.dirname(os.path.abspath(__file__))
ITEMS_PATH = os.path.join(HERE, "items.json")
ANSWERS_PATH = os.path.join(HERE, "answers.json")
KEY_PATH = os.path.join(os.path.dirname(HERE), "audit-key.json")
STATIC_DIR = os.path.join(HERE, "static")
PORT = int(os.environ.get("AUDIT_PORT", "8765"))
# Mobile access requires binding beyond loopback. Default to all
# interfaces so a phone on the same LAN/WiFi can reach it; set
# AUDIT_HOST=127.0.0.1 to go back to this-machine-only.
HOST = os.environ.get("AUDIT_HOST", "0.0.0.0")

PASSWORD = os.environ.get("AUDIT_PASSWORD") or secrets.token_urlsafe(9)
SESSIONS = set()
LOGIN_ATTEMPTS = {}  # ip -> (count, window_start)
COOKIE_NAME = "tierh_session"


def load_json(path, default):
    if os.path.exists(path):
        with open(path) as f:
            return json.load(f)
    return default


def save_answers(answers):
    tmp = ANSWERS_PATH + ".tmp"
    with open(tmp, "w") as f:
        json.dump(answers, f, indent=1)
    os.replace(tmp, ANSWERS_PATH)


def score(answers, key):
    by_item = {a["n"]: a for a in answers}
    rows, both = [], 0
    for k in key:
        a = by_item.get(k["item"], {})
        ctx_ok = (a.get("context") or "").strip().lower() == k["context"].strip().lower()
        typ_ok = (a.get("type") or "").strip().lower() == k["type"].strip().lower()
        if ctx_ok and typ_ok:
            both += 1
        rows.append({
            "item": k["item"], "seed": k["seed"], "ep": k["ep"], "line": k["line"],
            "expected_context": k["context"], "expected_type": k["type"],
            "given_context": a.get("context", ""), "given_type": a.get("type", ""),
            "flagged": bool(a.get("flagged")), "exact": ctx_ok and typ_ok,
            "context_ok": ctx_ok, "type_ok": typ_ok,
        })
    n = len(key) or 1
    ctx_acc = sum(r["context_ok"] for r in rows) / n
    typ_acc = sum(r["type_ok"] for r in rows) / n
    return {
        "total": len(key), "exact": both, "exact_rate": both / n,
        "context_accuracy": ctx_acc, "type_accuracy": typ_acc,
        "flagged": sum(r["flagged"] for r in rows),
        "answered": sum(1 for r in rows if r["given_context"] or r["given_type"]),
        "rows": rows,
    }


class Handler(http.server.BaseHTTPRequestHandler):
    server_version = "TierHAudit/1"

    def log_message(self, format, *args):
        sys.stderr.write("%s - %s\n" % (self.address_string(), format % args))

    def _cookie_token(self):
        raw = self.headers.get("Cookie", "")
        for part in raw.split(";"):
            part = part.strip()
            if part.startswith(COOKIE_NAME + "="):
                return part[len(COOKIE_NAME) + 1:]
        return None

    def _authed(self):
        return self._cookie_token() in SESSIONS

    def _send_json(self, obj, status=200):
        body = json.dumps(obj).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _send_file(self, path, content_type):
        try:
            with open(path, "rb") as f:
                body = f.read()
        except OSError:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _read_json_body(self):
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b"{}"
        return json.loads(raw or b"{}")

    def _rate_limited(self):
        ip = self.client_address[0]
        count, window = LOGIN_ATTEMPTS.get(ip, (0, time.time()))
        if time.time() - window > 60:
            count, window = 0, time.time()
        LOGIN_ATTEMPTS[ip] = (count + 1, window)
        return count >= 10

    # ---- routing ----
    def do_GET(self):
        if self.path in ("/", "/index.html"):
            if not self._authed():
                self.send_response(302)
                self.send_header("Location", "/login")
                self.end_headers()
                return
            return self._send_file(os.path.join(STATIC_DIR, "index.html"), "text/html")
        if self.path == "/login":
            return self._send_file(os.path.join(STATIC_DIR, "login.html"), "text/html")
        if self.path == "/app.js":
            if not self._authed():
                return self.send_error(403)
            return self._send_file(os.path.join(STATIC_DIR, "app.js"), "application/javascript")
        if self.path == "/style.css":
            return self._send_file(os.path.join(STATIC_DIR, "style.css"), "text/css")
        if self.path == "/api/items":
            if not self._authed():
                return self.send_error(403)
            return self._send_json(load_json(ITEMS_PATH, []))
        if self.path == "/api/progress":
            if not self._authed():
                return self.send_error(403)
            return self._send_json(load_json(ANSWERS_PATH, []))
        self.send_error(404)

    def do_POST(self):
        if self.path == "/api/login":
            if self._rate_limited():
                return self._send_json({"ok": False, "error": "too many attempts, wait a minute"}, 429)
            body = self._read_json_body()
            given = (body.get("password") or "").encode()
            if hmac.compare_digest(given, PASSWORD.encode()):
                token = secrets.token_urlsafe(32)
                SESSIONS.add(token)
                self.send_response(200)
                self.send_header("Set-Cookie", f"{COOKIE_NAME}={token}; HttpOnly; SameSite=Lax; Path=/")
                self.send_header("Content-Type", "application/json")
                payload = b'{"ok": true}'
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)
                return
            return self._send_json({"ok": False, "error": "wrong password"}, 401)

        if not self._authed():
            return self.send_error(403)

        if self.path == "/api/answer":
            body = self._read_json_body()
            n = body.get("n")
            answers = load_json(ANSWERS_PATH, [])
            answers = [a for a in answers if a.get("n") != n]
            answers.append({
                "n": n, "context": body.get("context", ""),
                "type": body.get("type", ""), "flagged": bool(body.get("flagged")),
                "ts": time.time(),
            })
            save_answers(answers)
            return self._send_json({"ok": True})

        if self.path == "/api/finish":
            answers = load_json(ANSWERS_PATH, [])
            key = load_json(KEY_PATH, [])
            if not key:
                return self._send_json({"ok": False, "error": "no answer key found on disk"}, 500)
            result = score(answers, key)
            with open(os.path.join(os.path.dirname(HERE), "human-audit-score.json"), "w") as f:
                json.dump(result, f, indent=1)
            return self._send_json({"ok": True, "score": result})

        self.send_error(404)


class ThreadingServer(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True


def lan_ips():
    ips = set()
    try:
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.connect(("8.8.8.8", 80))
        ips.add(s.getsockname()[0])
        s.close()
    except OSError:
        pass
    try:
        for info in socket.getaddrinfo(socket.gethostname(), None):
            ip = str(info[4][0])
            if ip.startswith(("192.168.", "10.", "172.")):
                ips.add(ip)
    except OSError:
        pass
    return sorted(ips)


def main():
    if not os.path.exists(ITEMS_PATH):
        sys.exit(f"missing {ITEMS_PATH} — run build_audit.py first")
    server = ThreadingServer((HOST, PORT), Handler)
    local_url = f"http://127.0.0.1:{PORT}/login"
    print("=" * 60, flush=True)
    print("  Tier-H audit server", flush=True)
    print(f"  On this machine: {local_url}", flush=True)
    if HOST != "127.0.0.1":
        for ip in lan_ips():
            print(f"  From your phone (same WiFi): http://{ip}:{PORT}/login", flush=True)
        print("  NOTE: plain HTTP on your LAN — fine at home/office, do not", flush=True)
        print("  use this over a public/untrusted WiFi network.", flush=True)
    else:
        print("  (bound to 127.0.0.1 only — not reachable from a phone)", flush=True)
    print(f"  Password: {PASSWORD}", flush=True)
    print("=" * 60, flush=True)
    if os.environ.get("AUDIT_NO_BROWSER") != "1":
        try:
            webbrowser.open(local_url)
        except Exception:
            pass
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
