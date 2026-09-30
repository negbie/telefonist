#include <assert.h>
#include "../../pkg/gobaresip/g722/g722.c"

/* NULL codec contexts deliberately make any premature codec call fail. */
int main(void)
{
    struct auenc_state encoder = {0};
    struct audec_state decoder = {0};
    int16_t samples[4] = {0};
    uint8_t bytes[4] = {0};
    size_t capacity = 1;
    assert(encode(&encoder, NULL, bytes, &capacity, AUFMT_S16LE, samples, 4) == EOVERFLOW);
    assert(decode(&decoder, AUFMT_S16LE, samples, &capacity, false, bytes, 1) == EOVERFLOW);
    capacity = sizeof(bytes);
    assert(encode(&encoder, NULL, bytes, &capacity, AUFMT_S16LE, samples, 3) == EINVAL);
    assert(decode(&decoder, AUFMT_S16LE, samples, NULL, false, bytes, 1) == EINVAL);
    return 0;
}
