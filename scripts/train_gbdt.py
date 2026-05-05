import argparse
import json
import math
from datetime import datetime, timezone
from pathlib import Path

import numpy as np


MCC_RISK = {
    "5411": 0.15,
    "5812": 0.30,
    "5912": 0.20,
    "5944": 0.45,
    "7801": 0.80,
    "7802": 0.75,
    "7995": 0.85,
    "4511": 0.35,
    "5311": 0.25,
    "5999": 0.50,
}


def clamp(v):
    return 0.0 if v < 0 else 1.0 if v > 1 else float(v)


def parse_time(s):
    dt = datetime.fromisoformat(s.replace("Z", "+00:00")).astimezone(timezone.utc)
    epoch_min = int(dt.timestamp() // 60)
    return epoch_min, dt.hour, dt.weekday()


def vectorize(req):
    tx = req["transaction"]
    customer = req["customer"]
    merchant = req["merchant"]
    terminal = req["terminal"]
    req_min, hour, dow = parse_time(tx["requested_at"])
    out = np.zeros(14, dtype=np.float32)
    out[0] = clamp(tx["amount"] / 10000.0)
    out[1] = clamp(tx["installments"] / 12.0)
    if customer["avg_amount"] > 0:
        out[2] = clamp((tx["amount"] / customer["avg_amount"]) / 10.0)
    out[3] = hour / 23.0
    out[4] = dow / 6.0
    last = req.get("last_transaction")
    if last is None:
        out[5] = -1.0
        out[6] = -1.0
    else:
        last_min, _, _ = parse_time(last["timestamp"])
        out[5] = clamp((req_min - last_min) / 1440.0)
        out[6] = clamp(last["km_from_current"] / 1000.0)
    out[7] = clamp(terminal["km_from_home"] / 1000.0)
    out[8] = clamp(customer["tx_count_24h"] / 20.0)
    out[9] = 1.0 if terminal["is_online"] else 0.0
    out[10] = 1.0 if terminal["card_present"] else 0.0
    out[11] = 0.0 if merchant["id"] in customer["known_merchants"] else 1.0
    out[12] = MCC_RISK.get(merchant["mcc"], 0.5)
    out[13] = clamp(merchant["avg_amount"] / 10000.0)
    return out


def features(v):
    last_null = (v[:, 5] < 0).astype(np.float32)
    return np.column_stack(
        [
            v,
            last_null,
            v[:, 0] * v[:, 7],
            v[:, 0] * v[:, 11],
            v[:, 7] * v[:, 11],
            v[:, 2] * v[:, 8],
            v[:, 9] * (1 - v[:, 10]),
            v[:, 12] * v[:, 11],
            v[:, 0] * v[:, 12],
            v[:, 7] * v[:, 12],
            v[:, 8] * v[:, 11],
        ]
    ).astype(np.float32)


def sigmoid(x):
    return 1.0 / (1.0 + np.exp(-np.clip(x, -40, 40)))


def make_bins(x, max_bins):
    bins = np.empty(x.shape, dtype=np.uint16)
    thresholds = []
    qs = np.linspace(0, 1, max_bins + 1)[1:-1]
    for j in range(x.shape[1]):
        th = np.unique(np.quantile(x[:, j], qs).astype(np.float32))
        thresholds.append(th)
        bins[:, j] = np.searchsorted(th, x[:, j], side="right")
    return bins, thresholds


class Trainer:
    def __init__(self, x, y, max_depth, min_leaf, lam, max_bins):
        self.x = x
        self.y = y
        self.max_depth = max_depth
        self.min_leaf = min_leaf
        self.lam = lam
        self.bins, self.thresholds = make_bins(x, max_bins)
        self.nodes = []

    def fit_tree(self, grad, hess):
        self.grad = grad
        self.hess = hess
        self.nodes = []
        self._build(np.arange(self.x.shape[0], dtype=np.int32), 0)
        return self.nodes

    def _leaf_value(self, idx):
        g = float(self.grad[idx].sum())
        h = float(self.hess[idx].sum())
        return g / (h + self.lam)

    def _build(self, idx, depth):
        ni = len(self.nodes)
        self.nodes.append([255, 0.0, -1, -1, self._leaf_value(idx)])
        if depth >= self.max_depth or len(idx) < self.min_leaf * 2:
            return ni
        parent_g = float(self.grad[idx].sum())
        parent_h = float(self.hess[idx].sum())
        parent_gain = parent_g * parent_g / (parent_h + self.lam)
        best = None
        for f, th in enumerate(self.thresholds):
            if len(th) == 0:
                continue
            b = self.bins[idx, f]
            n_bins = len(th) + 1
            cnt = np.bincount(b, minlength=n_bins)
            gsum = np.bincount(b, weights=self.grad[idx], minlength=n_bins)
            hsum = np.bincount(b, weights=self.hess[idx], minlength=n_bins)
            lc = np.cumsum(cnt)[:-1]
            rc = len(idx) - lc
            ok = (lc >= self.min_leaf) & (rc >= self.min_leaf)
            if not np.any(ok):
                continue
            lg = np.cumsum(gsum)[:-1]
            lh = np.cumsum(hsum)[:-1]
            rg = parent_g - lg
            rh = parent_h - lh
            gain = lg * lg / (lh + self.lam) + rg * rg / (rh + self.lam) - parent_gain
            gain[~ok] = -np.inf
            split_bin = int(np.argmax(gain))
            if not math.isfinite(float(gain[split_bin])):
                continue
            if best is None or gain[split_bin] > best[0]:
                best = (float(gain[split_bin]), f, split_bin, float(th[split_bin]))
        if best is None or best[0] <= 1e-9:
            return ni
        _, f, split_bin, threshold = best
        left_mask = self.bins[idx, f] <= split_bin
        left_idx = idx[left_mask]
        right_idx = idx[~left_mask]
        if len(left_idx) < self.min_leaf or len(right_idx) < self.min_leaf:
            return ni
        left = self._build(left_idx, depth + 1)
        right = self._build(right_idx, depth + 1)
        self.nodes[ni] = [f, threshold, left, right, 0.0]
        return ni


def eval_tree(x, tree):
    pred = np.zeros(x.shape[0], dtype=np.float32)
    for i in range(x.shape[0]):
        ni = 0
        while True:
            f, th, left, right, value = tree[ni]
            if left < 0:
                pred[i] = value
                break
            ni = left if x[i, f] <= th else right
    return pred


def eval_trees(x, base, trees):
    pred = np.full(x.shape[0], base, dtype=np.float32)
    for tree in trees:
        for i in range(x.shape[0]):
            ni = 0
            while True:
                f, th, left, right, value = tree[ni]
                if left < 0:
                    pred[i] += value
                    break
                ni = left if x[i, f] <= th else right
    return pred


def best_threshold(score, expected_approved):
    order = np.argsort(score)
    fraud = ~expected_approved
    fp = int(expected_approved.sum())
    fn = 0
    best = (fp + 3 * fn, float(score[order[0]]) - 1.0, fp, fn)
    for idx in order:
        if fraud[idx]:
            fn += 1
        else:
            fp -= 1
        e = fp + 3 * fn
        if e < best[0]:
            best = (e, float(score[idx]), fp, fn)
    return best


def write_go(path, base, threshold, trees):
    lines = [
        "package vector",
        "",
        "// Code generated by scripts/train_gbdt.py; DO NOT EDIT.",
        "",
        f"const GBDTBase = float32({base:.9g})",
        f"const GBDTThreshold = float32({threshold:.9g})",
        "const gbdtFeatureCount = 24",
        "",
        "type gbdtNode struct {",
        "\tFeature uint8",
        "\tThreshold float32",
        "\tLeft int16",
        "\tRight int16",
        "\tValue float32",
        "}",
        "",
        "func GBDTApproved(v [Dims]float32) bool {",
        "\treturn GBDTScore(v) < GBDTThreshold",
        "}",
        "",
        "func GBDTScore(v [Dims]float32) float32 {",
        "\tx := gbdtFeatures(v)",
        "\tscore := GBDTBase",
        "\tfor _, tree := range gbdtTrees {",
        "\t\tidx := int16(0)",
        "\t\tfor {",
        "\t\t\tn := tree[idx]",
        "\t\t\tif n.Left < 0 {",
        "\t\t\t\tscore += n.Value",
        "\t\t\t\tbreak",
        "\t\t\t}",
        "\t\t\tif x[n.Feature] <= n.Threshold {",
        "\t\t\t\tidx = n.Left",
        "\t\t\t} else {",
        "\t\t\t\tidx = n.Right",
        "\t\t\t}",
        "\t\t}",
        "\t}",
        "\treturn score",
        "}",
        "",
        "func gbdtFeatures(v [Dims]float32) [gbdtFeatureCount]float32 {",
        "\tlastNull := float32(0)",
        "\tif v[5] < 0 {",
        "\t\tlastNull = 1",
        "\t}",
        "\treturn [gbdtFeatureCount]float32{",
        "\t\tv[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7],",
        "\t\tv[8], v[9], v[10], v[11], v[12], v[13], lastNull,",
        "\t\tv[0] * v[7], v[0] * v[11], v[7] * v[11],",
        "\t\tv[2] * v[8], v[9] * (1 - v[10]), v[12] * v[11],",
        "\t\tv[0] * v[12], v[7] * v[12], v[8] * v[11],",
        "\t}",
        "}",
        "",
        "var gbdtTrees = [...][]gbdtNode{",
    ]
    for tree in trees:
        lines.append("\t{")
        for f, th, left, right, value in tree:
            lines.append(
                f"\t\t{{Feature: {int(f)}, Threshold: {float(th):.9g}, Left: {int(left)}, Right: {int(right)}, Value: {float(value):.9g}}},"
            )
        lines.append("\t},")
    lines.append("}")
    Path(path).write_text("\n".join(lines) + "\n", encoding="utf-8")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--trees", type=int, default=240)
    ap.add_argument("--depth", type=int, default=4)
    ap.add_argument("--lr", type=float, default=0.08)
    ap.add_argument("--min-leaf", type=int, default=35)
    ap.add_argument("--lambda", dest="lam", type=float, default=2.0)
    ap.add_argument("--bins", type=int, default=192)
    args = ap.parse_args()

    raw = json.loads(Path(args.data).read_text(encoding="utf-8"))
    vectors = np.array([vectorize(e["request"]) for e in raw["entries"]], dtype=np.float32)
    expected_approved = np.array([e["expected_approved"] for e in raw["entries"]], dtype=bool)
    y = (~expected_approved).astype(np.float32)
    x = features(vectors)

    pos = float(y.sum())
    neg = float(len(y) - pos)
    base = math.log(pos / neg)
    score = np.full(len(y), base, dtype=np.float32)
    trainer = Trainer(x, y, args.depth, args.min_leaf, args.lam, args.bins)
    trees = []
    for i in range(args.trees):
        p = sigmoid(score)
        grad = y - p
        hess = p * (1 - p)
        tree = trainer.fit_tree(grad, hess)
        for node in tree:
            node[4] *= args.lr
        trees.append(tree)
        score += eval_tree(x, tree)
        if (i + 1) % 20 == 0:
            e, th, fp, fn = best_threshold(score, expected_approved)
            print(f"trees={i+1} weighted={e} fp={fp} fn={fn} threshold={th:.6f}")
    e, threshold, fp, fn = best_threshold(score, expected_approved)
    print(f"best weighted={e} fp={fp} fn={fn} threshold={threshold:.9g}")
    write_go(args.out, base, threshold, trees)


if __name__ == "__main__":
    main()
