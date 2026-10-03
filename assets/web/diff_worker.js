import { computeRunDiff } from "./diff.js";

self.onmessage = event => {
  const { a, b } = event.data;
  self.postMessage(computeRunDiff(a, b));
};
