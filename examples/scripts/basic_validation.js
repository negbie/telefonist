// Example: Basic Test Validation
// Verify that standard call steps executed successfully

var ringingEvents = events.filter(e => e.type === "CALL_RINGING");
telefonist.assert(ringingEvents.length > 0, "No ringing events found in the flow");

var establishedEvents = events.filter(e => e.type === "CALL_ESTABLISHED");
telefonist.assert(establishedEvents.length >= 1, "Call was never completely established");

var hangups = events.filter(e => e.type === "CALL_CLOSED");
telefonist.assert(hangups.length >= 2, "Call was not closed properly by both parties");
