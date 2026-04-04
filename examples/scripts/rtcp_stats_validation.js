// Example: RTCP Audio Stats
// Ensure call quality doesn't degrade (e.g. excessive burst loss or Jitter)

var rtcpStats = events.filter(e => e.type === "CALL_RTCP");
telefonist.assert(rtcpStats.length > 0, "No RTCP stats reported. Was the call long enough to generate stats?");

for (var i = 0; i < rtcpStats.length; i++) {
    var rawStat = rtcpStats[i].param;
    if (!rawStat) continue;

    // A baresip RTCP param might look like: "rx Jitter=2.5ms ..."
    var match = rawStat.match(/Jitter=([\d\.]+)ms/i);
    if (match && match.length > 1) {
        var jitter = parseFloat(match[1]);
        telefonist.assert(jitter < 20.0, "Jitter too high! " + jitter + "ms exceeds the 20ms maximum threshold.");
    }
}
