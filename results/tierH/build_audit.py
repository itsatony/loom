#!/usr/bin/env python3
"""Build the tier-H human-audit markdown + blind answer key (frames v1).

Samples ~100 events across the 5 tier-H seeds, weighted toward
frame-bearing lines with actual-statement controls, deterministic seed.
Ground truth goes ONLY to the key file.
"""
import json, random, collections, os

BASE = "/home/itsatony/code/loom/datasets/frames-batch-v1"
OUT_MD = "/home/itsatony/code/loom/results/tierH/tierH-human-audit.md"
OUT_KEY = "/home/itsatony/code/loom/results/tierH/audit-key.json"
OUT_ITEMS = "/home/itsatony/code/loom/results/tierH/webaudit/items.json"
SEEDS = [1, 2, 3, 7, 8]
PER_SEED_FRAME = 14
PER_SEED_ACTUAL = 6
RNG = random.Random(20260713)

os.makedirs(os.path.dirname(OUT_MD), exist_ok=True)

def load(seed):
    eps = []
    with open(f"{BASE}/seed-{seed}/episodes_tierH.jsonl") as f:
        for line in f:
            eps.append(json.loads(line))
    rep = json.load(open(f"{BASE}/seed-{seed}/naturalize-report-tierH.json"))
    handles = {h["frame_id"]: h for h in rep["handles"]}
    return eps, handles

def fr_context(frame_id, handles):
    if not frame_id:
        return "actual"
    h = handles.get(frame_id)
    if frame_id.startswith("fic_"):
        return f'story {h["handle"]}'
    if frame_id.startswith("psp_"):
        return f'view {h.get("entity", h["handle"])}'
    return f'exercise {h["handle"]}'

def label(ev, handles):
    k = ev["kind"]
    if k == "frame":
        fd = ev["frame_decl"]
        return fr_context(fd["id"], handles), "declaration"
    if k == "promotion":
        return "actual", "confirmation"
    if k == "fact":
        ctx = fr_context(ev["fact"].get("frame", ""), handles)
        at = ev.get("assertion_type", "")
        if at == "non-assertive":
            return ctx, "sarcasm"
        if at == "quote":
            return ctx, "quote"
        return ctx, "statement"
    if k == "rule":
        return fr_context(ev["rule"].get("frame", ""), handles), "statement"
    return fr_context(ev["supersession"].get("frame", ""), handles), "statement"

items, key = [], []
for seed in SEEDS:
    eps, handles = load(seed)
    # running declaration context (naturalized decl lines, stream order)
    decls_before = []  # list of (ep_index, decl_text)
    all_events = []    # (ep_idx, ev_idx, ev, decl_snapshot_len)
    for i, ep in enumerate(eps):
        for j, ev in enumerate(ep["events"]):
            all_events.append((i, j, ev, len(decls_before)))
        for ev in ep["events"]:
            if ev["kind"] == "frame":
                decls_before.append(ev["text"])
    frame_pool, actual_pool = [], []
    for (i, j, ev, dlen) in all_events:
        ctx, typ = label(ev, handles)
        fb = not (ctx == "actual" and typ == "statement")
        (frame_pool if fb else actual_pool).append((i, j, ev, dlen, ctx, typ))
    # stratify frame pool by (context-class, type) so fiction/view/exercise/
    # sarcasm/declaration/confirmation all appear
    strata = collections.defaultdict(list)
    for it in frame_pool:
        cls = it[4].split(" ")[0]
        strata[(cls, it[5])].append(it)
    chosen = []
    keys_sorted = sorted(strata.keys())
    while len(chosen) < PER_SEED_FRAME and any(strata[k] for k in keys_sorted):
        for k in keys_sorted:
            if strata[k] and len(chosen) < PER_SEED_FRAME:
                pick = RNG.choice(strata[k])
                strata[k].remove(pick)
                chosen.append(pick)
    chosen += RNG.sample(actual_pool, PER_SEED_ACTUAL)
    RNG.shuffle(chosen)
    decls_all = decls_before  # full list for slicing

    # Cumulative context options per episode: "actual" plus every frame
    # declared in this episode or any earlier one (order = first
    # declaration), snapshotted AFTER episode i so a declaration line's
    # own frame is itself a valid option (the full episode is shown).
    options_by_ep = [[]] * len(eps)
    seen_frames, order = set(), []
    for i, ep in enumerate(eps):
        for ev in ep["events"]:
            if ev["kind"] == "frame":
                fid = ev["frame_decl"]["id"]
                if fid not in seen_frames:
                    seen_frames.add(fid)
                    order.append(fid)
        options_by_ep[i] = ["actual"] + [fr_context(fid, handles) for fid in order]

    for (i, j, ev, dlen, ctx, typ) in chosen:
        items.append({
            "seed": seed, "ep": eps[i]["id"], "ep_idx": i, "line": j + 1,
            "day": ev["day"], "decls": decls_all[:dlen],
            "episode_lines": [e["text"] for e in eps[i]["events"]],
            "context_options": options_by_ep[i],
        })
        key.append({"item": len(items), "seed": seed, "ep": eps[i]["id"],
                    "line": j + 1, "context": ctx, "type": typ})

md = []
md.append("""# Tier-H human audit — frames v1 (MASTERPLAN §9.6.6)

**What this is.** Tier H is the adversarial rung of the frames measurement
ladder: the first 5 locked seeds, naturalized by the same two LLMs as the
certified tier-M corpus but **without the 3-judge recoverability filter** —
whatever the naturalizers produced first (mechanically valid) is what you
see. Your recovery accuracy on this sample is the registered **human
ceiling**: it is reported machine-vs-human and sits OUTSIDE all kill
arithmetic. Do not overthink items — first confident read, like a busy
analyst skimming a log.

**The world.** You are reading a business operations log: observations,
policies, notices about assets, partners, products, jurisdictions. Some
lines are NOT plain fact:

- **story** lines report events inside a work of fiction circulating in
  the office; the story is referred to by its title (e.g. "The Paper
  Meridian"). Content asserted only in the story is not true in the world.
- **view** lines carry claims attributed to a named party (their view,
  their statement, possibly quoted). The log records that they said/hold
  it, not that it is true.
- **exercise** lines belong to a named planning exercise / drill (bare
  codename, e.g. "Blueline"): assumptions adopted, facts set aside,
  policies changed *within the exercise only*.
- everything else is an **actual** line: the log itself asserting a fact,
  a policy, a supersession notice, or a confirmation that something
  earlier turned out true.

**Your task per item.** For the marked line (`>>>`), decide:

1. `context` — whose reality does the line's content live in?
   - `actual` — the log asserts it
   - `story <title>` — inside the named fiction (write the title)
   - `view <party>` — attributed to the named party (write the party
     identifier, e.g. `view asset_15`)
   - `exercise <codename>` — inside the named drill (write the codename)
2. `type` — what kind of speech act is it?
   - `statement` — plain assertion of fact/policy/notice
   - `quote` — direct quoted speech ("...") attributed to a party
   - `sarcasm` — the line is ironic/mocking; its literal content is
     asserted nowhere (these read as strangely off-key remarks)
   - `declaration` — the line ANNOUNCES/introduces a story, a party's
     viewpoint stream, or an exercise (rather than reporting content)
   - `confirmation` — the line confirms/verifies that an earlier claim
     turned out to be true in reality

   Tie-breaks: a quoted remark that is plainly ironic/mocking is
   `sarcasm`, not `quote` (sarcasm outranks quote). The context of a
   sarcastic remark is where the remark itself occurs — usually
   `actual` — regardless of whom it mocks.

**Context you get.** Each item shows the announcements a sequential reader
of the log would have seen so far (collapsed section) and the full episode
containing the marked line. Days are integer timestamps.

**How to answer.** Fill the two empty fields in each item's ```yaml block.
Keep the `item:` number untouched. Free-text after `story`/`view`/
`exercise` should match the name used in the text (case-insensitive is
fine). If truly undecidable, write `context: unsure` — that scores as a
miss but is honest.

**Do not open** `results/tierH/audit-key.json` (the answer key) until you
are done. Scoring: exact context+type match, same rule as the machine
judges (≥2/3 exact). ~100 items ≈ 60–90 minutes.

---
""")

for n, it in enumerate(items, 1):
    md.append(f"## Item {n:03d}  (seed {it['seed']}, {it['ep']}, day {it['day']})\n")
    if it["decls"]:
        md.append("<details><summary>Announcements seen earlier in the log"
                  f" ({len(it['decls'])})</summary>\n")
        for d in it["decls"]:
            md.append(f"> {d}")
        md.append("</details>\n")
    md.append("Episode:\n")
    for li, txt in enumerate(it["episode_lines"], 1):
        mark = ">>>" if li == it["line"] else "   "
        md.append(f"```")
        break
    ep_block = []
    for li, txt in enumerate(it["episode_lines"], 1):
        mark = ">>>" if li == it["line"] else "   "
        ep_block.append(f"{mark} {li}. {txt}")
    md.append("\n".join(ep_block))
    md.append("```\n")
    md.append("```yaml")
    md.append(f"item: {n}")
    md.append("context: ")
    md.append("type: ")
    md.append("```\n")

with open(OUT_MD, "w") as f:
    f.write("\n".join(md))
with open(OUT_KEY, "w") as f:
    json.dump(key, f, indent=1)

os.makedirs(os.path.dirname(OUT_ITEMS), exist_ok=True)
webitems = [{
    "n": n, "seed": it["seed"], "ep": it["ep"], "day": it["day"],
    "decls": it["decls"], "episode_lines": it["episode_lines"],
    "target_line": it["line"], "context_options": it["context_options"],
} for n, it in enumerate(items, 1)]
with open(OUT_ITEMS, "w") as f:
    json.dump(webitems, f, indent=1)

cls = collections.Counter((k["context"].split(" ")[0], k["type"]) for k in key)
print(f"items: {len(items)} -> {OUT_MD}")
for c, n in sorted(cls.items()):
    print(f"  {c[0]:9s} {c[1]:12s} {n}")
