// Example: Multi-line Log Parsing
// Baresip occasionally prints complex multi-line text tables to the LOG stream.
// Example Log Format:
// audio           Transmit:     Receive:
// packets:            302          296
// avg. bitrate:      63.9         63.7  (kbit/s)
// errors:               0            0

// Filter all LOG events that contain the "audio" and "Transmit:" headers
var logEvents = events.filter(e => e.type === "LOG" && e.param && e.param.includes("packets:") && e.param.includes("Transmit:"));

telefonist.assert(logEvents.length > 0, "No audio statistic logs found during the test. Did the call run long enough to output a report?");

for (var i = 0; i < logEvents.length; i++) {
    var rawText = logEvents[i].param;

    // Use regular expressions to extract the Transmit and Receive values from the table
    // The "\s+" pattern matches multiple spaces between the values.
    // Example string: "packets:            302          296"
    var match = rawText.match(/packets:\s+(\d+)\s+(\d+)/);
    
    if (match && match.length === 3) {
        var txPackets = parseInt(match[1]);
        var rxPackets = parseInt(match[2]);
        
        // Assert that we received at least 200 packets!
        telefonist.assert(rxPackets > 200, "Audio reception failure! Only received " + rxPackets + " packets.");
        
        // You could also check errors!
        var errorMatch = rawText.match(/errors:\s+(\d+)\s+(\d+)/);
        if (errorMatch && errorMatch.length === 3) {
            var rxErrors = parseInt(errorMatch[2]);
            telefonist.assert(rxErrors === 0, "Call experienced " + rxErrors + " receive errors!");
        }
    }
}
