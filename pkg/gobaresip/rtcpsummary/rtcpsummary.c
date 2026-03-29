/**
 * @file rtcpsummary.c RTCP summary module
 * Output RTCP stats at the end of a call if there are any
 *
 *  Copyright (C) 2010 - 2018 Alfred E. Heggestad
 */
#include <re.h>
#include <baresip.h>


static void print_rtcp_summary_line(const struct call *call,
				    const struct stream *s)
{
	const struct rtcp_stats *rtcp;
	struct mbuf *mb;

	rtcp = stream_rtcp_stats(s);

	mb = mbuf_alloc(512);
	if (!mb)
		return;

	if (rtcp && (rtcp->tx.sent || rtcp->rx.sent)) {

		mbuf_printf(mb,
			"peeruri=%s;"	/* Peer URI */
			"rx=%s;"		/* Packets RX */
			"tx=%s;",		/* Packets TX */
			//"id=%s;"		/* Call ID */
			call_peeruri(call),
			rtcp->rx.sent > 10 ? "true" : "false",
			rtcp->tx.sent > 10 ? "true" : "false");
			//call_id(call),
			//1.0 * rtcp->rx.jit/1000,
			//1.0 * rtcp->tx.jit/1000,
			//1.0 * rtcp->rtt/1000);
		bevent_ua_emit(BEVENT_RTCP_SUMMARY, call_get_ua(call), "%b", mb->buf, mb->pos);
	}
	else {
		mbuf_printf(mb, "No RTCP stats collected for peer=%s", call_peeruri(call));
		bevent_ua_emit(BEVENT_RTCP_SUMMARY, call_get_ua(call), "%b", mb->buf, mb->pos);
	}

	mem_deref(mb);
}


static void event_handler(enum bevent_ev ev, struct bevent *event, void *arg)
{
	const struct stream *s;
	struct le *le;
	struct call *call = bevent_get_call(event);
	(void)arg;

	if (!call)
		return;

	switch (ev) {

	case BEVENT_CALL_CLOSED:
		for (le = call_streaml(call)->head;
		     le;
		     le = le->next) {
			s = le->data;
			print_rtcp_summary_line(call, s);
		}
		break;

	default:
		break;
	}
}


static int module_init(void)
{
	int err = bevent_register(event_handler, NULL);
	if (err) {
		info("Error loading rtcpsummary module: %d", err);
		return err;
	}
	return 0;
}


static int module_close(void)
{
    bevent_unregister(event_handler);
	debug("rtcpsummary: module closing..\n");
	bevent_unregister(event_handler);
	return 0;
}


const struct mod_export DECL_EXPORTS(rtcpsummary) = {
	"rtcpsummary",
	"application",
	module_init,
	module_close
};
