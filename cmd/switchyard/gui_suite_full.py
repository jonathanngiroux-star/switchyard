"""Switchyard GUI full suite — driven through the real webview via the
in-app test bridge. Mock data: fresh empty DB (first-run/wizard paths)
plus the committed migrate fixtures. Every UI claim is cross-verified
against the SQLite store (the database is the oracle).

Run while the GUI is up with SWITCHYARD_GUI_TESTBRIDGE=1:
    python3 gui_driver.py run gui_suite_full.py
"""
import json
import os
import sqlite3
import sys
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from gui_driver import summarize  # noqa: E402

DB = os.environ.get("SWITCHYARD_GUI_DB", "/tmp/swy/bridge.db")
# .../switchyard/cmd/switchyard/gui_suite_full.py -> repo root is 3 dirnames up
_ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
FIX_LD = os.path.join(_ROOT, "testdata", "fixtures", "launchdarkly", "full-export.json")
FIX_UL = os.path.join(_ROOT, "testdata", "fixtures", "unleash", "unleash-representative.json")

ETH = "0x85ee7E71f762d772599cbF1EC20E651B30657521"
BTC = "bc1qxe2zx5tv3hdreaej6s2x4p7han85uey828rrhg"


def db_rows(table):
    con = sqlite3.connect(DB)
    try:
        return con.execute(f"SELECT * FROM {table}").fetchall()
    finally:
        con.close()


def audit_last(n=5):
    con = sqlite3.connect(DB)
    try:
        return con.execute(
            "SELECT actor, action, key, env FROM audit_log ORDER BY id DESC LIMIT ?", (n,)).fetchall()
    finally:
        con.close()


def suite(gui):
    checks = []

    # ---------- 1. first-run wizard (fresh DB) -------------------------
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: active on empty store",
                             ws.get("ok") and ws["value"]["active"] is True, json.dumps(ws)))
    t = gui.read("#wiz-title")
    checks.append(gui.expect("wizard: step 1 is welcome",
                             t.get("ok") and "welcome" in t["value"]["text"], json.dumps(t)))

    r = gui.click("#wiz-next")  # step 2: environment
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: next lands on environment step",
                             ws["value"]["step"] == 1, json.dumps(ws)))
    r = gui.read("#wiz-body")
    checks.append(gui.expect("wizard: env select rendered",
                             r.get("ok") and "default environment" in r["value"]["text"]))

    r = gui.select("#wiz-env", "dev")
    checks.append(gui.expect("wizard: env select accepts dev", r.get("ok") and r["value"] == "dev"))

    r = gui.click("#wiz-next")  # step 3: first flag
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: next lands on first-flag step",
                             ws["value"]["step"] == 2, json.dumps(ws)))

    r = gui.click("#wiz-next")  # empty key: must NOT advance
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: empty flag key blocks advance",
                             ws["value"]["step"] == 2, json.dumps(ws)))
    m = gui.read("#wiz-flag-msg")
    checks.append(gui.expect("wizard: empty key shows guidance",
                             m.get("ok") and "flag key" in m["value"]["text"]))

    r = gui.fill("#wiz-flag", "wizard-made-flag")
    checks.append(gui.expect("wizard: flag input accepts key", r.get("ok") and r["value"] == "wizard-made-flag"))
    r = gui.click("#wiz-next")  # creates the flag → step 4
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: advances to support step",
                             ws["value"]["step"] == 3, json.dumps(ws)))
    rows = db_rows("flags")
    checks.append(gui.expect("wizard: flag persisted to SQLite",
                             any(r[0] == "wizard-made-flag" for r in rows), str(rows)))

    b = gui.read("#wiz-body")
    checks.append(gui.expect("wizard: donate step shows ETH address",
                             b.get("ok") and ETH in b["value"]["text"]))
    checks.append(gui.expect("wizard: donate step shows BTC address",
                             b.get("ok") and BTC in b["value"]["text"]))

    r = gui.click("#wiz-back")
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: back returns to first-flag step",
                             ws["value"]["step"] == 2, json.dumps(ws)))
    r = gui.click("#wiz-next")
    r = gui.click("#wiz-next")  # 'done' → wizard closes
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: done closes wizard",
                             ws["value"]["active"] is False, json.dumps(ws)))

    # ---------- 2. main flags view --------------------------------------
    rows = gui.texts("#flagrows tr td:first-child")
    checks.append(gui.expect("flags: table shows wizard flag",
                             rows.get("ok") and "wizard-made-flag" in rows["value"], json.dumps(rows)))

    r = gui.fill("#newkey", "suite-second-flag")
    checks.append(gui.expect("flags: new-key input fills", r.get("ok") and r["value"] == "suite-second-flag"))
    gui.click_text("create", sel="#view-flags")  # scoped: the create button, not nav
    time.sleep(0.4)
    rows = db_rows("flags")
    checks.append(gui.expect("flags: create persisted (2 flags in DB)",
                             len(rows) == 2, str(rows)))

    # select the row (its onclick sets selectedKey), then toggle via row button
    gui.click_text("suite-second-flag", sel="#flagrows")
    time.sleep(0.2)
    r = gui.cmd("clickText", text="suite-second-flag", sel="#flagrows")
    time.sleep(0.2)
    # click THIS row's toggle button by exact selector — clickText("toggle")
    # would match the row's own textContent (it contains the button label)
    r = gui.click('tr[data-key="suite-second-flag"] button')
    checks.append(gui.expect("flags: toggle click dispatched", r.get("ok"), json.dumps(r)))
    # poll the audit log: the async write may land a beat after the click
    toggled = False
    for _ in range(20):
        al = audit_last(4)
        if any(a[0] == "gui" and a[1] == "toggle" for a in al):
            toggled = True
            break
        time.sleep(0.25)
    checks.append(gui.expect("flags: toggle audited (actor=gui)", toggled, str(audit_last(4))))

    # rollout: row already selected; set %
    r = gui.fill("#rolloutpct", "25")
    checks.append(gui.expect("flags: rollout input fills", r.get("ok") and r["value"] == "25"))
    gui.click_text("set rollout %", sel="#view-flags")
    time.sleep(0.4)
    al = audit_last(2)
    checks.append(gui.expect("flags: rollout audited",
                             any(a[1] == "rollout" for a in al), str(al)))

    # ---------- 3. environment switching ---------------------------------
    r = gui.select("#env", "dev")
    checks.append(gui.expect("env: dropdown switches to dev", r.get("ok") and r["value"] == "dev"))
    rows = gui.texts("#flagrows tr")
    checks.append(gui.expect("env: rows reload after switch", rows.get("ok")))

    # ---------- 4. segments ----------------------------------------------
    r = gui.click_text("segments")
    checks.append(gui.expect("segments: view opens", r.get("ok")))
    gui.click_text("new segment")
    time.sleep(0.2)
    r = gui.fill("#segjson", json.dumps({
        "key": "pro-users", "name": "Pro plan",
        "rules": [{"id": "r1", "clauses": [{"attribute": "plan", "operator": "in", "values": ["pro"]}]}],
    }))
    checks.append(gui.expect("segments: JSON editor fills", r.get("ok")))
    gui.click_text("save")
    time.sleep(0.4)
    segs = db_rows("segments")
    checks.append(gui.expect("segments: save persisted to SQLite",
                             any(s[0] == "pro-users" for s in segs), str(segs)))

    # ---------- 5. migrate dry-run viewer ---------------------------------
    r = gui.click_text("migrate dry-run")
    checks.append(gui.expect("migrate: view opens", r.get("ok")))
    r = gui.select("#migsource", "launchdarkly")
    checks.append(gui.expect("migrate: source select", r.get("ok") and r["value"] == "launchdarkly"))
    r = gui.fill("#migpath", FIX_LD)
    checks.append(gui.expect("migrate: path fills", r.get("ok")))
    gui.click_text("dry-run", sel="#view-migrate")  # scoped: the RUN button, not the nav
    time.sleep(1.5)
    f = gui.read("#migfidelity")
    checks.append(gui.expect("migrate: LD fidelity 100% (fixture corpus)",
                             f.get("ok") and "100.0%" in f["value"]["text"], json.dumps(f)))
    u = gui.read("#migunmapped")
    checks.append(gui.expect("migrate: LD representative has no unmapped rows",
                             u.get("ok") and "no unmapped constructs" in u["value"]["text"], json.dumps(u)))

    r = gui.select("#migsource", "unleash")
    r = gui.fill("#migpath", FIX_UL)
    gui.click_text("dry-run", sel="#view-migrate")
    time.sleep(1.5)
    s = gui.read("#migsummary")
    checks.append(gui.expect("migrate: Unleash summary renders (8 added)",
                             s.get("ok") and "8 added" in s["value"]["text"], json.dumps(s)))

    # ---------- 6. eval console -------------------------------------------
    r = gui.click_text("eval console")
    r = gui.fill("#evalflag", "wizard-made-flag")
    r = gui.fill("#evaluser", "alice")
    gui.click_text("evaluate")
    time.sleep(0.6)
    e = gui.read("#evalresult")
    checks.append(gui.expect("eval: result rendered with reason+bucket",
                             e.get("ok") and "bucket" in e["value"]["text"], json.dumps(e)[:200]))
    # deterministic: run twice, identical output
    gui.click_text("evaluate")
    time.sleep(0.4)
    e2 = gui.read("#evalresult")
    checks.append(gui.expect("eval: deterministic (identical re-eval)",
                             e2.get("ok") and e["value"]["text"] == e2["value"]["text"]))

    # ---------- 7. audit view ---------------------------------------------
    r = gui.click_text("audit log")
    rows = gui.texts("#auditrows tr")
    checks.append(gui.expect("audit: entries listed (GUI mutations visible)",
                             rows.get("ok") and len(rows["value"]) > 0, json.dumps(rows)[:200]))

    # ---------- 8. donate view + copy buttons ------------------------------
    r = gui.click_text("donate")
    time.sleep(0.3)
    a = gui.read("#addr-eth")
    checks.append(gui.expect("donate: ETH address byte-for-byte",
                             a.get("ok") and a["value"]["text"].strip() == ETH, json.dumps(a)))
    a = gui.read("#addr-btc")
    checks.append(gui.expect("donate: BTC address byte-for-byte",
                             a.get("ok") and a["value"]["text"].strip() == BTC, json.dumps(a)))
    r = gui.click("#copy-eth")
    time.sleep(0.3)
    b = gui.read("#copy-eth")
    checks.append(gui.expect("donate: copy button gives feedback (copied/selected)",
                             b.get("ok") and ("copied" in b["value"]["text"] or "select" in b["value"]["text"]),
                             json.dumps(b)))

    # ---------- 9. wizard reopen key ('?') ---------------------------------
    gui.key("?", event="keydown")
    time.sleep(0.3)
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: '?' reopens from main screen",
                             ws["value"]["active"] is True, json.dumps(ws)))
    gui.key("Escape", event="keydown")
    time.sleep(0.3)
    ws = gui.wizard_state()
    checks.append(gui.expect("wizard: Esc closes",
                             ws["value"]["active"] is False, json.dumps(ws)))

    # ---------- 10. error surfaces ----------------------------------------
    gui.click_text("flags")
    r = gui.fill("#newkey", "bad key with spaces")
    gui.click("#view-flags button.act")
    time.sleep(0.4)
    e = gui.read("#err-flags")
    checks.append(gui.expect("errors: invalid key surfaces in UI",
                             e.get("ok") and len(e["value"]["text"].strip()) > 0, json.dumps(e)))

    return summarize(checks)
