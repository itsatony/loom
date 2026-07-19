#!/usr/bin/env python3
"""One-seed extraction-robustness probe (dev, diagnostic only).
Resamples ep_133's EXACT recorded extraction prompt against live gpt-5-mini
(reasoning_effort=high, temperature omitted — identical shape to the accepted
frames-v1 gpt-5-mini leg) N times, and reports how rul_019 is homed each draw.
The recorded (accepted) draw filed it into fiction frame "The Unsigned Page"
(wrong); truth is frame "" (a normal desk policy). Question: stochastic miss
(self-consistency recovers) or deterministic bias (it does not)?"""
import json, glob, os, sys, urllib.request, concurrent.futures as cf

CASS = os.path.expanduser("~/code/loom/cassettes/gpt5mini-frames-c2bf")
N = int(sys.argv[1]) if len(sys.argv) > 1 else 12
KEY = None
for line in open(os.path.expanduser("~/code/.creds")):
    if line.startswith("OPENAI_APIKEY="):
        KEY = line.strip().split("=", 1)[1]; break
assert KEY, "no OPENAI_APIKEY"

# locate ep_133's recorded prompt (system+user verbatim)
sysmsg = usermsg = None
for f in glob.glob(os.path.join(CASS, "*.json")):
    c = json.load(open(f))
    u = c.get("user") or ""
    if "rul_019" in u and "The Unsigned Page begins circulating on day 39" in u:
        sysmsg, usermsg = c["system"], u
        print("prompt from cassette:", os.path.basename(f)); break
assert usermsg, "ep_133 cassette not found"

def draw(i):
    body = {"model": "gpt-5-mini",
            "messages": [{"role": "system", "content": sysmsg},
                         {"role": "user", "content": usermsg}],
            "reasoning_effort": "high"}
    req = urllib.request.Request("https://api.openai.com/v1/chat/completions",
        data=json.dumps(body).encode(),
        headers={"Authorization": f"Bearer {KEY}", "Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=240) as r:
        out = json.load(r)
    txt = out["choices"][0]["message"]["content"]
    obj = json.loads(txt[txt.index("{"):txt.rindex("}") + 1])
    rule = next((x for x in obj.get("rules", []) if x.get("rule_id") == "rul_019"), None)
    frames = [(fr.get("name"), fr.get("kind")) for fr in obj.get("frames", [])]
    if rule is None:
        return i, "NO_RULE", None, frames
    # correctness of logical content (conditions/conclusion relations)
    conds = sorted(c["relation"] for c in rule.get("conditions", []))
    concl = rule.get("conclusion", {}).get("relation")
    content_ok = conds == ["classified_as", "exempt_from", "offered_in"] and concl == "priority_case"
    return i, rule.get("frame"), content_ok, frames

results = []
with cf.ThreadPoolExecutor(max_workers=6) as ex:
    for r in ex.map(draw, range(N)):
        results.append(r); print("draw", r[0], "-> frame=%r content_ok=%s frames=%s" % (r[1], r[2], r[3]))

frames_of = {}
for _, fr, ok, _ in results:
    frames_of[fr] = frames_of.get(fr, 0) + 1
correct = sum(1 for _, fr, ok, _ in results if fr in ("", None) and ok)
print("\n=== SUMMARY (N=%d) ===" % N)
print("rul_019 home-frame distribution:", frames_of)
print("correct (frame ''/actual AND content ok): %d/%d" % (correct, N))
json.dump(results, open(os.path.join(os.path.dirname(__file__), "resample_ep133_results.json"), "w"), indent=1)
