// Example: Strict Chronological order check
// Ensure events happened in exact predicted progression

var requiredSequence = ["CALL_INCOMING", "CALL_RINGING", "CALL_ESTABLISHED", "CALL_CLOSED"];
var currentIndex = 0;

for (var i = 0; i < events.length; i++) {
    if (events[i].type === requiredSequence[currentIndex]) {
        currentIndex++;
    }
    if (currentIndex >= requiredSequence.length) {
        break; // Match found completely!
    }
}

var lastMatchedState = currentIndex > 0 ? requiredSequence[currentIndex-1] : "None";
telefonist.assert(
    currentIndex === requiredSequence.length, 
    "Events fired out of order or failed to trigger fully! Stuck waiting for: " + requiredSequence[currentIndex] + " (Last matched: " + lastMatchedState + ")"
);
