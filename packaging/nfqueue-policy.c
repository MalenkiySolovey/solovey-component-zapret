// SPDX-License-Identifier: MIT
#include <stdint.h>
#include <linux/netfilter/nfnetlink_queue.h>
#include <libnetfilter_queue/libnetfilter_queue.h>

int __real_nfq_set_queue_flags(struct nfq_q_handle *, uint32_t, uint32_t);

// The pinned engine requests FAIL_OPEN. In the Solovey build a full queue
// must drop packets, even if nftables did not request the bypass flag.
int __wrap_nfq_set_queue_flags(struct nfq_q_handle *queue, uint32_t mask, uint32_t flags)
{
	return __real_nfq_set_queue_flags(queue, mask | NFQA_CFG_F_FAIL_OPEN,
					flags & ~NFQA_CFG_F_FAIL_OPEN);
}
