#!/usr/bin/env python3
"""EPICS Gateway — thin HTTP proxy for IOC PV access. Run as systemd service on gascell.
   Only allows whitelisted PVs. Zero new dependencies (stdlib http.server + pyepics)."""

import json, sys
from http.server import HTTPServer, BaseHTTPRequestHandler
from epics import caget, caput

HOST, PORT = "0.0.0.0", 5070

WL = {  # PV → read? write?
    "GasCell:Piezo:A1":        (True, False),
    "GasCell:Piezo:ValveSP":   (True, True),
    "GasCell:Piezo:Running":   (True, True),
    "GasCell:Piezo:Setpoint":  (True, True),
    "GasCell:Piezo:Kp":         (True, True),
    "GasCell:Piezo:Ki":         (True, True),
    "GasCell:Piezo:Kd":         (True, True),
    "GasCell:Piezo:Error":     (True, False),
    "GasCell:Piezo:Delta":     (True, False),
    "GasCell:Piezo:Cycle":     (True, False),
    "GasCell:Safety:A5Max":    (True, True),
    "GasCell:Safety:A5Trip":   (True, False),
    "GasCell:Safety:A5TripPV": (True, False),
    "GasCell:Safety:A5TripTime": (True, False),
    "GasCell:Safety:A5Clear":  (False, True),
    "GasCell:Vac:A5":          (True, False),
}

class Handler(BaseHTTPRequestHandler):
    def _ok(self, data):
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.end_headers(); self.wfile.write(json.dumps(data).encode())

    def _err(self, code, msg):
        self.send_response(code); self.send_header("Content-Type", "application/json")
        self.end_headers(); self.wfile.write(json.dumps({"error": msg}).encode())

    def do_GET(self):
        pv = self.path.strip("/")
        if not pv:
            return self._ok({"status": "ok"})
        if pv not in WL or not WL[pv][0]:
            return self._err(403, f"PV not in read whitelist: {pv}")
        try:
            val = caget(pv, timeout=3)
            self._ok({"pv": pv, "value": val if val is not None else None})
        except Exception as e:
            self._err(502, str(e))

    def do_POST(self):
        pv = self.path.strip("/")
        if pv not in WL or not WL[pv][1]:
            return self._err(403, f"PV not in write whitelist: {pv}")
        try:
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            caput(pv, body["value"], wait=True, timeout=5)
            self._ok({"pv": pv, "value": body["value"]})
        except Exception as e:
            self._err(502, str(e))

if __name__ == "__main__":
    HTTPServer((HOST, PORT), Handler).serve_forever()
