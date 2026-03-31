/**
 * @file audioreport.c Audio report module
 * Outputs an audio report at the end of a call
 */
#include <re.h>
#include <baresip.h>


static void emit_audio_report_event(const struct call *call,
				    const struct stream *s)
{
	struct mbuf *mb;
	mb = mbuf_alloc(512);
	if (!mb)
		return;

	if (s) {
		uint32_t tx_packets = stream_metric_get_tx_n_packets(s);
		uint32_t rx_packets = stream_metric_get_rx_n_packets(s);
		const struct sdp_media *m = stream_sdpmedia(s);

		mbuf_printf(mb,
			"peeruri=%s;"
			"tx=%s;"
			"rx=%s;"
			"proto=%s;",
			call_peeruri(call),
			tx_packets > 10 ? "true" : "false",
			rx_packets > 10 ? "true" : "false",
			sdp_media_proto(m));

		if (m && 0 == str_casecmp(sdp_media_name(m), "audio")) {
			struct audio *au = call_audio(call);
			if (au) {
				const struct aucodec *ac = audio_codec(au, true);
				if (!ac)
					ac = audio_codec(au, false);

				if (ac && ac->name) {
					mbuf_printf(mb, "codec=%s;", ac->name);
				}
			}
		}

		bevent_ua_emit(BEVENT_AUDIO_REPORT, call_get_ua(call), "%b", mb->buf, mb->pos);
	}
	else {
		mbuf_printf(mb, "No audio report for peer=%s", call_peeruri(call));
		bevent_ua_emit(BEVENT_AUDIO_REPORT, call_get_ua(call), "%b", mb->buf, mb->pos);
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
			emit_audio_report_event(call, s);
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
		info("Error loading audioreport module: %d", err);
		return err;
	}
	return 0;
}


static int module_close(void)
{
	debug("audioreport: module closing..\n");
	bevent_unregister(event_handler);
	return 0;
}


const struct mod_export DECL_EXPORTS(audioreport) = {
	"audioreport",
	"application",
	module_init,
	module_close
};
