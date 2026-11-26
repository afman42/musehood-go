#!/usr/bin/env python3
"""Uji voting live di produksi memakai ruang sementara (dibersihkan di akhir)."""
import json
import sys
import time
import urllib.request
import urllib.error

B = "http://127.0.0.1:3001"


def call(method, path, body=None, timeout=30):
    req = urllib.request.Request(
        B + path, method=method,
        data=json.dumps(body).encode() if body is not None else None,
        headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            return r.status, json.loads(r.read() or b"{}")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read() or b"{}")


def state():
    return call("GET", "/api/state")[1]


def wait_done(deadline_s, what):
    start = time.time()
    while time.time() - start < deadline_s:
        st = state()
        busy = [m for m in st["messages"] if m.get("status") in ("typing", "waiting", "queued")]
        if not busy:
            return st
        time.sleep(5)
    raise SystemExit(f"TIMEOUT menunggu {what}")


def main():
    s, out = call("POST", "/api/rooms", {"name": "Uji Voting", "seatCodes": ["M01", "M02"]})
    assert s == 201, out
    room_id = out["activeRoomId"]
    print("ruang uji:", room_id)
    try:
        s, out = call("POST", "/api/message", {
            "text": "@Plamo @GLM Sebutkan satu kata yang menggambarkan pagi hari, lalu jelaskan alasannya dalam satu kalimat.",
            "roomId": room_id})
        assert s == 202, out
        print("pesan diskusi terkirim, menunggu jawaban kursi gateway...")
        st = wait_done(420, "jawaban diskusi")
        for m in st["messages"]:
            if m.get("authorType") == "seat":
                print(f"  pesan {m['seat']} status={m.get('status')} error={m.get('error')!r} teks={m.get('text','')[:90]!r}")
        answers = [m for m in st["messages"] if m.get("authorType") == "seat" and m.get("status") == "completed" and m.get("text")]
        assert len(answers) >= 2, f"jawaban kursi kurang: {len(answers)}"

        s, out = call("POST", "/api/vote", {})
        assert s == 202, out
        print("voting dimulai, menunggu hasil...")
        st = wait_done(420, "hasil voting")
        best = [m for m in st["messages"] if m.get("bestAnswer")]
        votes = [(m["seat"], m.get("votes", 0)) for m in st["messages"] if m.get("authorType") == "seat" and m.get("votes")]
        summary = [m for m in st["messages"] if "Voting" in (m.get("label") or "") or "voting" in m.get("text", "").lower()[:40]]
        print("suara:", votes)
        print("jawaban terbaik:", [(m["seat"], m.get("votes")) for m in best])
        for m in st["messages"]:
            if m.get("label"):
                print(f"  label {m['label']}: {m['text'][:120]!r}")
        assert best, "tidak ada bestAnswer setelah voting"
        print("VOTING LIVE: LULUS")
    finally:
        call("POST", "/api/rooms/room-utama/activate", {})
        s, out = call("DELETE", f"/api/rooms/{room_id}")
        print("ruang uji dihapus:", s, "| ruang tersisa:", len(out.get("rooms", [])))


if __name__ == "__main__":
    main()
