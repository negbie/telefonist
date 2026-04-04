// Example: SIP Header Inspection
// Verify that the SIP communications contain required headers and normalized formats

var invites = events.filter(e => e.type === "SIP" && e.param && e.param.includes("INVITE sip:"));
telefonist.assert(invites.length > 0, "No SIP INVITE messages found");

for (var i = 0; i < invites.length; i++) {
   var sipText = invites[i].param;
   
   // Verify strict User-Agent normalization
   telefonist.assert(sipText.includes("User-Agent:"), "User agent header completely missing!");
   
   // Check if Max-Forwards header exists
   var hasMaxForwards = sipText.includes("Max-Forwards:");
   telefonist.assert(hasMaxForwards, "Max-Forwards header missing from INVITE");
}
