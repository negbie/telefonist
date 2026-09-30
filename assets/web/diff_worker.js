import { computeLCSDiff } from "./diff.js";
self.onmessage = event => {
  const {a,b} = event.data;
  self.postMessage(computeLCSDiff(a,b));
};
