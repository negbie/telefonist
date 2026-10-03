const MAX_PAIR_CANDIDATES = 2000000;

function itemIndex(item, fallback) {
  return Number.isFinite(item?.idx) ? item.idx : fallback;
}

function tokenize(value) {
  const matches = String(value || "").toLowerCase().match(/[a-z0-9_@.+:/-]+/g);
  return new Set(matches || []);
}

function similarity(a, b) {
  if (a === b) return 1;
  const left = tokenize(a);
  const right = tokenize(b);
  if (!left.size || !right.size) return 0;

  let common = 0;
  const smaller = left.size <= right.size ? left : right;
  const larger = smaller === left ? right : left;
  smaller.forEach(token => {
    if (larger.has(token)) common++;
  });
  return (2 * common) / (left.size + right.size);
}

function resultOrder(entry, aLength, bLength) {
  if (entry.itemA) return itemIndex(entry.itemA, aLength);
  const bIndex = itemIndex(entry.itemB, bLength);
  return bLength ? (bIndex * Math.max(aLength, 1)) / bLength : bIndex;
}

/**
 * Compares two captures as multisets instead of sequences.
 *
 * Exact values are matched first, which makes harmless reordering and duplicate
 * entries deterministic. Remaining values may only become a changed pair when
 * their mode-specific matchKey agrees and their content is sufficiently alike.
 */
export function computeRunDiff(aItems, bItems) {
  const a = Array.isArray(aItems) ? aItems : [];
  const b = Array.isArray(bItems) ? bItems : [];
  const bByValue = new Map();

  b.forEach((item, index) => {
    const key = String(item?.compare || "");
    const queue = bByValue.get(key) || [];
    queue.push(index);
    bByValue.set(key, queue);
  });

  const matchedA = new Uint8Array(a.length);
  const matchedB = new Uint8Array(b.length);
  const result = [];

  a.forEach((itemA, aIndex) => {
    const queue = bByValue.get(String(itemA?.compare || ""));
    while (queue?.length && matchedB[queue[0]]) queue.shift();
    if (!queue?.length) return;

    const bIndex = queue.shift();
    matchedA[aIndex] = 1;
    matchedB[bIndex] = 1;
    result.push({
      type: "common",
      itemA,
      itemB: b[bIndex],
      aIndex,
      bIndex,
      score: 1,
    });
  });

  const remainingA = [];
  const remainingB = [];
  a.forEach((item, index) => !matchedA[index] && remainingA.push({ item, index }));
  b.forEach((item, index) => !matchedB[index] && remainingB.push({ item, index }));

  const groupsA = new Map();
  const groupsB = new Map();
  remainingA.forEach(ref => {
    const key = String(ref.item?.matchKey || "");
    if (!key) return;
    const group = groupsA.get(key) || [];
    group.push(ref);
    groupsA.set(key, group);
  });
  remainingB.forEach(ref => {
    const key = String(ref.item?.matchKey || "");
    if (!key) return;
    const group = groupsB.get(key) || [];
    group.push(ref);
    groupsB.set(key, group);
  });

  let candidateCount = 0;
  groupsA.forEach((left, key) => {
    candidateCount += left.length * (groupsB.get(key)?.length || 0);
  });

  if (candidateCount <= MAX_PAIR_CANDIDATES) {
    const candidates = [];
    groupsA.forEach((left, key) => {
      const right = groupsB.get(key) || [];
      left.forEach(aRef => {
        right.forEach(bRef => {
          const score = similarity(aRef.item?.compare, bRef.item?.compare);
          const threshold = Math.max(
            Number(aRef.item?.matchThreshold) || 0,
            Number(bRef.item?.matchThreshold) || 0,
            0.3,
          );
          if (score >= threshold) {
            candidates.push({ aRef, bRef, score });
          }
        });
      });
    });

    // Global best-first matching avoids the first duplicate stealing a much
    // better counterpart later in the capture. Distance is only a tie-breaker.
    candidates.sort((x, y) =>
      y.score - x.score ||
      Math.abs(x.aRef.index - x.bRef.index) - Math.abs(y.aRef.index - y.bRef.index) ||
      x.aRef.index - y.aRef.index ||
      x.bRef.index - y.bRef.index,
    );

    candidates.forEach(({ aRef, bRef, score }) => {
      if (matchedA[aRef.index] || matchedB[bRef.index]) return;
      matchedA[aRef.index] = 1;
      matchedB[bRef.index] = 1;
      result.push({
        type: "changed",
        itemA: aRef.item,
        itemB: bRef.item,
        aIndex: aRef.index,
        bIndex: bRef.index,
        score,
      });
    });
  }

  a.forEach((itemA, aIndex) => {
    if (!matchedA[aIndex]) result.push({ type: "a", itemA, aIndex });
  });
  b.forEach((itemB, bIndex) => {
    if (!matchedB[bIndex]) result.push({ type: "b", itemB, bIndex });
  });

  const rank = { changed: 0, a: 1, b: 2, common: 3 };
  result.sort((x, y) =>
    resultOrder(x, a.length, b.length) - resultOrder(y, a.length, b.length) ||
    rank[x.type] - rank[y.type] ||
    (x.bIndex ?? -1) - (y.bIndex ?? -1),
  );
  return result;
}

// Ordered line diff used by Git history. Run comparison intentionally uses the
// separate multiset matcher above because capture order is not stable.
export function computeLCSDiff(aItems, bItems) {
  const a = Array.isArray(aItems) ? aItems : [];
  const b = Array.isArray(bItems) ? bItems : [];
  const m = a.length;
  const n = b.length;
  if (m * n > MAX_PAIR_CANDIDATES) {
    return a.map(itemA => ({ type: "a", text: itemA.display, itemA }))
      .concat(b.map(itemB => ({ type: "b", text: itemB.display, itemB })));
  }

  const dp = Array.from({ length: m + 1 }, () => new Uint32Array(n + 1));
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      dp[i][j] = a[i - 1].compare === b[j - 1].compare
        ? dp[i - 1][j - 1] + 1
        : Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }

  const result = [];
  let i = m;
  let j = n;
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && a[i - 1].compare === b[j - 1].compare) {
      result.push({
        type: "common",
        text: a[i - 1].display,
        textA: a[i - 1].display,
        textB: b[j - 1].display,
        itemA: a[i - 1],
        itemB: b[j - 1],
      });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      result.push({ type: "b", text: b[j - 1].display, itemB: b[j - 1] });
      j--;
    } else {
      result.push({ type: "a", text: a[i - 1].display, itemA: a[i - 1] });
      i--;
    }
  }
  return result.reverse();
}
