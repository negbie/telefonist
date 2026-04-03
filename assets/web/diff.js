export function computeLCSDiff(aItems, bItems) {
  var m = aItems.length,
    n = bItems.length;
  var dp = Array.from({ length: m + 1 }, function () {
    return new Uint32Array(n + 1);
  });

  for (var i = 1; i <= m; i++) {
    for (var j = 1; j <= n; j++) {
      dp[i][j] =
        aItems[i - 1].compare === bItems[j - 1].compare
          ? dp[i - 1][j - 1] + 1
          : Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }

  var result = [];
  var ci = m,
    cj = n;
  while (ci > 0 || cj > 0) {
    if (ci > 0 && cj > 0 && aItems[ci - 1].compare === bItems[cj - 1].compare) {
      result.unshift({ type: "common", text: aItems[ci - 1].display });
      ci--;
      cj--;
    } else if (cj > 0 && (ci === 0 || dp[ci][cj - 1] >= dp[ci - 1][cj])) {
      result.unshift({ type: "b", text: bItems[cj - 1].display });
      cj--;
    } else {
      result.unshift({ type: "a", text: aItems[ci - 1].display });
      ci--;
    }
  }
  return result;
}
