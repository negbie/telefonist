/**
 * @file presence.c Presence module
 *
 * Copyright (C) 2010 Alfred E. Heggestad
 */
#include <re.h>
#include <baresip.h>
#include "presence.h"

static void event_handler(enum bevent_ev ev, struct bevent *event, void *arg)
{
	(void)arg;

	if (ev == BEVENT_SHUTDOWN) {
		struct ua *ua = bevent_get_ua(event);
		debug("presence: ua=%p got event %d (%s)\n", ua, ev,
		      bevent_str(ev));

		subscriber_close_all();
	}
}


static int module_init(void)
{
	int err;

	err = subscriber_init();
	if (err)
		return err;

	err = bevent_register(event_handler, NULL);
	if (err)
		return err;

	return 0;
}


static int module_close(void)
{
	bevent_unregister(event_handler);
	subscriber_close();
	return 0;
}


const struct mod_export DECL_EXPORTS(presence) = {
	"presence",
	"application",
	module_init,
	module_close
};
